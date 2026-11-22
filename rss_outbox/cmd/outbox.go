package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	db "github.com/FKouhai/rss-demo/libs/db"
	"github.com/FKouhai/rss-demo/libs/instrumentation"
	log "github.com/FKouhai/rss-demo/libs/logger"
	"github.com/FKouhai/rss-demo/libs/pubsub"
	"github.com/FKouhai/rss-outbox/store"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// tracer is the service-scoped helper for outbox spans.
var tracer = instrumentation.ForService("outbox")

type Outbox struct {
	nc            *pubsub.Client
	store         store.Store
	consumeCtxs   []jetstream.ConsumeContext
	dispatchTotal *prometheus.CounterVec
	ackTotal      *prometheus.CounterVec
	poisonTotal   *prometheus.CounterVec
	pendingItems  *prometheus.GaugeVec
	dispatchSecs  *prometheus.HistogramVec
	sweepStop     chan struct{}
	stopOnce      sync.Once
}

// Metrics bundles the outbox counters. All fields optional; nil entries
// are skipped so unit tests can attach a subset or nothing at all.
type Metrics struct {
	DispatchTotal *prometheus.CounterVec
	AckTotal      *prometheus.CounterVec
	PoisonTotal   *prometheus.CounterVec
	PendingItems  *prometheus.GaugeVec
	DispatchSecs  *prometheus.HistogramVec
}

// WithMetrics attaches counters; nil-safe so tests can skip it.
func (o *Outbox) WithMetrics(m *Metrics) *Outbox {
	if m == nil {
		return o
	}
	o.dispatchTotal, o.ackTotal = m.DispatchTotal, m.AckTotal
	o.poisonTotal, o.pendingItems = m.PoisonTotal, m.PendingItems
	o.dispatchSecs = m.DispatchSecs
	return o
}

func NewOutbox(nc *pubsub.Client, s store.Store) *Outbox {
	return &Outbox{nc: nc, store: s}
}

func (o *Outbox) Start(ctx context.Context) error {

	if o.nc == nil {
		return errors.New("outbox: NATS client is nil")
	}

	if _, err := o.nc.EnsureStream(ctx, pubsub.FeedsStreamConfig()); err != nil {
		return fmt.Errorf("ensure feeds stream: %w", err)
	}

	if _, err := o.nc.EnsureConsumer(ctx, string(pubsub.StreamFeeds),
		pubsub.ConsumerFor("outbox-feeds", pubsub.SubjectFeedsNew)); err != nil {
		return fmt.Errorf("ensure outbox-feeds consumer: %w", err)
	}

	if _, err := o.nc.EnsureStream(ctx, pubsub.NotificationsStreamConfig()); err != nil {
		return fmt.Errorf("ensure notifications stream: %w", err)
	}

	if _, err := o.nc.EnsureStream(ctx, pubsub.ACKStreamConfig()); err != nil {
		return fmt.Errorf("ensure acks stream: %w", err)
	}

	if _, err := o.nc.EnsureConsumer(ctx, string(pubsub.StreamAcks),
		pubsub.ConsumerFor("outbox-acks", pubsub.SubjectAcksDelivered)); err != nil {
		return fmt.Errorf("ensure outbox-acks consumer: %w", err)
	}

	pending, err := o.store.GetPending(ctx)
	if err != nil {
		return fmt.Errorf("outbox replay get pending: %w", err)
	}
	// Re-baseline the pending gauge: decrements can be lost when an Ack
	// call fails after the store write, so the gauge heals on every boot.
	if o.pendingItems != nil {
		o.pendingItems.WithLabelValues().Set(float64(len(pending)))
	}
	// One root span for the whole replay so a cold start reads as a single
	// trace instead of N orphan publishes. Rows still inside their first
	// backoff window wait for the sweeper instead of re-flooding on boot.
	repCtx, repSpan := tracer.Start(ctx, "outbox.Replay")
	repSpan.SetAttributes(attribute.Int("pending.rows", len(pending)))
	for _, e := range pending {
		if e.NextRetryAt != nil && e.NextRetryAt.After(time.Now().UTC()) {
			continue
		}
		if err := o.driveOne(repCtx, e); err != nil {
			// Log and continue: the sweeper re-drives on backoff, so a
			// single bad row must not fail boot for all the others.
			log.ErrorCtx(repCtx, "outbox replay dispatch failed",
				zap.String("outbox.id", e.ID), zap.Error(err))
		}
	}
	repSpan.End()

	feeds, err := o.nc.Consume(ctx, string(pubsub.StreamFeeds), "outbox-feeds", o.handleFeed)
	if err != nil {
		return fmt.Errorf("consume feeds.new: %w", err)
	}
	o.consumeCtxs = append(o.consumeCtxs, feeds)

	acks, err := o.nc.Consume(ctx, string(pubsub.StreamAcks), "outbox-acks", o.handleAck)
	if err != nil {
		o.Stop() // don't leak the feeds consumer on partial startup
		return fmt.Errorf("consume acks.delivered: %w", err)
	}
	o.consumeCtxs = append(o.consumeCtxs, acks)

	// The sweeper owns delivery assurance: any row still pending past its
	// backoff (lost ack, crashed notify, transient publish failure) gets
	// re-driven here. Terminal states only arrive via explicit acks.
	o.sweepStop = make(chan struct{})
	go o.sweepLoop(o.sweepStop)
	return nil
}

// driveOne publishes a single stored entry and records the attempt,
// scheduling the next re-drive with exponential backoff. msgID is the
// row ID, so a repeated drive dedupes at the broker.
func (o *Outbox) driveOne(ctx context.Context, e store.Entry) error {
	// Per-row span: re-drives are the interesting event on this path, and
	// the span parents the Publish producer span so a redelivery joins
	// the sweep tick instead of floating as an orphan root.
	driveCtx, driveSpan := tracer.Start(ctx, "outbox.drive")
	defer driveSpan.End()
	driveSpan.SetAttributes(attribute.String("outbox.id", e.ID))
	err := o.dispatch(driveCtx, e)
	if err != nil {
		driveSpan.RecordError(err)
	}
	next := time.Now().UTC().Add(store.BackoffForAttempt(e.Attempts))
	if nerr := o.store.NoteAttempt(driveCtx, e.ID, next); nerr != nil {
		driveSpan.RecordError(nerr)
		log.ErrorCtx(driveCtx, "outbox note attempt failed",
			zap.String("outbox.id", e.ID), zap.Error(nerr))
	}
	return err
}

// sweepLoop re-drives past-due pending rows until Stop. Rows past the
// attempt budget fail terminally instead of re-publishing: something is
// wrong with the delivery itself (bad webhook, poison downstream), and
// the row's error names it. A late ack still flips a failed row to
// delivered — success wins over the budget.
func (o *Outbox) sweepLoop(stop <-chan struct{}) {
	t := time.NewTicker(store.SweepInterval)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			o.sweepOnce(context.Background())
		}
	}
}

func (o *Outbox) sweepOnce(ctx context.Context) {
	sweepCtx, sweepSpan := tracer.Start(ctx, "outbox.sweep")
	defer sweepSpan.End()
	rows, err := o.store.GetDueForRetry(sweepCtx, time.Now().UTC(), store.SweepBatchLimit)
	if err != nil {
		sweepSpan.RecordError(err)
		log.ErrorCtx(sweepCtx, "outbox sweep fetch due failed", zap.Error(err))
		return
	}
	sweepSpan.SetAttributes(attribute.Int("rows.due", len(rows)))
	for _, e := range rows {
		if e.Attempts >= store.MaxAttempts {
			msg := fmt.Sprintf("retry budget exhausted (%d attempts)", e.Attempts)
			if merr := o.store.MarkFailed(sweepCtx, e.ID, msg); merr != nil {
				sweepSpan.RecordError(merr)
				log.ErrorCtx(sweepCtx, "outbox sweep mark failed",
					zap.String("outbox.id", e.ID), zap.Error(merr))
				continue
			}
			o.count(o.ackTotal, "failed")
			o.gaugeAdd(o.pendingItems, -1)
			sweepSpan.AddEvent("outbox.row.exhausted", trace.WithAttributes(attribute.String("outbox.id", e.ID)))
			log.ErrorCtx(sweepCtx, "outbox row failed terminally",
				zap.String("outbox.id", e.ID), zap.String("reason", msg))
			continue
		}
		if derr := o.driveOne(sweepCtx, e); derr != nil {
			log.ErrorCtx(sweepCtx, "outbox sweep dispatch failed",
				zap.String("outbox.id", e.ID), zap.Error(derr))
		}
	}
}

func (o *Outbox) count(cv *prometheus.CounterVec, v string) {
	if cv != nil {
		cv.WithLabelValues(v).Inc()
	}
}

func (o *Outbox) gaugeAdd(gv *prometheus.GaugeVec, delta float64) {
	if gv != nil {
		gv.WithLabelValues().Add(delta)
	}
}

func (o *Outbox) observe(hv *prometheus.HistogramVec, label string, secs float64) {
	if hv != nil {
		hv.WithLabelValues(label).Observe(secs)
	}
}

// dispatch publishes one stored entry. msgID is the row ID, so a repeated
// dispatch of the same row dedupes at the broker.
func (o *Outbox) dispatch(ctx context.Context, e store.Entry) (err error) {
	start := time.Now()
	outcome := "published"
	defer func() {
		o.observe(o.dispatchSecs, outcome, time.Since(start).Seconds())
		o.count(o.dispatchTotal, outcome)
	}()
	dm := pubsub.DeliveryMessage{
		TenantID:    "default", // TODO(config service): resolve per routing rules
		OutboxID:    e.ID,
		WebhookURL:  e.WebhookURL,
		FeedURL:     e.FeedURL,
		Title:       e.Title,
		Link:        e.Link,
		PublishedAt: e.PublishedAt,
		// First-seen time drives the recency gate: new-to-us posts even
		// when the feed date is old (e.g. newly surfaced HN stories).
		IngestedAt: e.CreatedAt.UTC().Format(time.RFC3339),
	}
	payload, err := json.Marshal(dm)
	if err != nil {
		outcome = "marshal_error"
		return fmt.Errorf("marshal delivery %s: %w", e.ID, err)
	}
	if _, err := o.nc.Publish(ctx, pubsub.SubjectNotificationRequest, payload, e.ID); err != nil {
		outcome = "publish_error"
		return fmt.Errorf("publish delivery %s: %w", e.ID, err)
	}

	return nil
}

// handleFeed persists a consumed RSS item and queues its delivery.
// Poison payloads Term (never redeliver); transient store/publish failures
// Nak (redelivery lands on the duplicate-Save branch and Acks).
func (o *Outbox) handleFeed(ctx context.Context, msg pubsub.Message) (pubsub.AckDecision, error) {
	var item pubsub.RSSItem
	if err := json.Unmarshal(msg.Data, &item); err != nil {
		o.count(o.poisonTotal, "feed")
		return pubsub.AckDecisionTerm, err
	}
	entry := store.Entry{
		ID:          item.MsgID(),
		FeedURL:     item.FeedURL,
		Title:       item.Title,
		Link:        item.Link,
		PublishedAt: item.PublishedAt,
		WebhookURL:  item.WebhookURL,
		Status:      "pending",
		CreatedAt:   time.Now().UTC(),
	}
	if _, err := o.store.Save(ctx, entry); err != nil {
		if db.IsDuplicateKey(err) {
			return pubsub.AckDecisionAck, nil
		}
		return pubsub.AckDecisionNack, err
	}
	o.gaugeAdd(o.pendingItems, 1)
	if err := o.dispatch(ctx, entry); err != nil {
		// Attempt consumed even on failure: the redelivery lands on the
		// duplicate-Save branch and Acks without re-dispatching, so the
		// row's next drive comes from the sweeper, not the broker.
		o.noteAttempt(ctx, entry.ID)
		return pubsub.AckDecisionNack, err
	}
	o.noteAttempt(ctx, entry.ID)
	return pubsub.AckDecisionAck, nil
}

// noteAttempt records one delivery drive with the initial backoff: normal
// flows ack in seconds, so only lost acks wait out the window.
func (o *Outbox) noteAttempt(ctx context.Context, id string) {
	if err := o.store.NoteAttempt(ctx, id, time.Now().UTC().Add(store.InitialRetryDelay)); err != nil {
		log.ErrorFmt("outbox note attempt %s: %v", id, err)
	}
}

// handleAck applies a delivery confirmation to the store.
// Unknown IDs Term (nothing to retry); store failures Nak for redelivery.
func (o *Outbox) handleAck(ctx context.Context, msg pubsub.Message) (pubsub.AckDecision, error) {
	var ack pubsub.AckReply
	if err := json.Unmarshal(msg.Data, &ack); err != nil {
		o.count(o.poisonTotal, "ack")
		return pubsub.AckDecisionTerm, err
	}
	if ack.OutboxID == "" {
		o.count(o.poisonTotal, "ack")
		return pubsub.AckDecisionTerm, errors.New("outbox: ack missing outbox_id")
	}
	var err error
	status := string(ack.Status)

	switch ack.Status {
	case pubsub.StatusDelivered:
		err = o.store.MarkDelivered(ctx, ack.OutboxID)
	case pubsub.StatusSkipped:
		err = o.store.MarkSkipped(ctx, ack.OutboxID, ack.Error)
	default:
		status = string(pubsub.StatusFailed)
		err = o.store.MarkFailed(ctx, ack.OutboxID, ack.Error)
	}
	if err != nil {
		return pubsub.AckDecisionNack, err
	}
	o.count(o.ackTotal, status)
	// Terminal ack: the row leaves pending. The gauge re-baselines from
	// GetPending on every restart, so a lost decrement heals on boot.
	o.gaugeAdd(o.pendingItems, -1)
	return pubsub.AckDecisionAck, nil
}

// Stop gracefully drains both subscriptions and halts the sweeper.
// In-flight work completes against the still-open store (see Shutdown
// order in cmd.Run); anything interrupted stays unacked and redelivers
// on restart.
func (o *Outbox) Stop() {
	// Once-guarded: Shutdown paths can each call Stop, and a second
	// close would panic.
	o.stopOnce.Do(func() {
		if o.sweepStop != nil {
			close(o.sweepStop)
			o.sweepStop = nil
		}
	})
	for _, cc := range o.consumeCtxs {
		if cc != nil {
			cc.Drain()
		}
	}
	o.consumeCtxs = nil
}
