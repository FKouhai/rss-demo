package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/FKouhai/rss-aggregator/store"
	db "github.com/FKouhai/rss-demo/libs/db"
	log "github.com/FKouhai/rss-demo/libs/logger"
	"github.com/FKouhai/rss-demo/libs/pubsub"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus"
)

// Aggregator persists feed items consumed from the FEEDS stream. It owns
// the durable consumer position: JetStream replays anything missed while
// down, so — unlike the outbox — there is no startup replay to run. A
// store that starts empty (fresh container, ephemeral sqlite) resets the
// durable first, so the read model rebuilds from retained history instead
// of resuming past it.
type Aggregator struct {
	nc          *pubsub.Client
	store       store.Store
	consumeCtxs []jetstream.ConsumeContext
	saveTotal   *prometheus.CounterVec
	poisonTotal *prometheus.CounterVec
}

// Metrics bundles the aggregator counters. All fields optional; nil
// entries are skipped so tests can attach a subset or nothing at all.
type Metrics struct {
	SaveTotal   *prometheus.CounterVec
	PoisonTotal *prometheus.CounterVec
}

// WithMetrics attaches counters; nil-safe so tests can skip it.
func (a *Aggregator) WithMetrics(m *Metrics) *Aggregator {
	if m == nil {
		return a
	}
	a.saveTotal, a.poisonTotal = m.SaveTotal, m.PoisonTotal
	return a
}

func (a *Aggregator) count(cv *prometheus.CounterVec, v string) {
	if cv != nil {
		cv.WithLabelValues(v).Inc()
	}
}

// NewAggregator builds an Aggregator over an established NATS client and
// an initialized store.
func NewAggregator(nc *pubsub.Client, s store.Store) *Aggregator {
	return &Aggregator{nc: nc, store: s}
}

// Start ensures the FEEDS stream and the aggregator's durable consumer,
// then begins consuming feeds.new.
func (a *Aggregator) Start(ctx context.Context) error {
	if a.nc == nil {
		return errors.New("aggregator: NATS client is nil")
	}

	if _, err := a.nc.EnsureStream(ctx, pubsub.FeedsStreamConfig()); err != nil {
		return fmt.Errorf("ensure feeds stream: %w", err)
	}

	// Empty store with an existing durable position would permanently skip
	// history the previous instance already acked (ephemeral sqlite starts
	// over while JetStream remembers). Reset so the read model rebuilds
	// from the stream, bounded by its retention. Non-empty stores resume
	// normally. A lookup failure fails safe to resume.
	if empty, err := a.store.IsEmpty(ctx); err == nil && empty {
		if derr := a.nc.JsContext.DeleteConsumer(ctx, string(pubsub.StreamFeeds), "aggregator-items"); derr != nil {
			log.InfoFmt("aggregator-items consumer already absent (or not deletable): %v", derr)
		}
	}

	if _, err := a.nc.EnsureConsumer(ctx, string(pubsub.StreamFeeds),
		pubsub.ConsumerFor("aggregator-items", pubsub.SubjectFeedsNew)); err != nil {
		return fmt.Errorf("ensure aggregator-items consumer: %w", err)
	}

	items, err := a.nc.Consume(ctx, string(pubsub.StreamFeeds), "aggregator-items", a.handleFeed)
	if err != nil {
		return fmt.Errorf("consume feeds.new: %w", err)
	}
	a.consumeCtxs = append(a.consumeCtxs, items)
	return nil
}

// handleFeed persists a consumed RSS item. The row ID is the content hash
// (feed URL + link), so redeliveries and cross-poll duplicates collapse
// onto one row: Term on poison (never redeliver), Ack on duplicates,
// Nak on transient store failures.
func (a *Aggregator) handleFeed(ctx context.Context, msg pubsub.Message) (pubsub.AckDecision, error) {
	var item pubsub.RSSItem
	if err := json.Unmarshal(msg.Data, &item); err != nil {
		a.count(a.poisonTotal, "feed")
		return pubsub.AckDecisionTerm, err
	}
	entry := store.Item{
		ID:          item.MsgID(),
		FeedURL:     item.FeedURL,
		Title:       item.Title,
		Link:        item.Link,
		PublishedAt: item.PublishedAt,
	}
	if _, err := a.store.Save(ctx, entry); err != nil {
		if db.IsDuplicateKey(err) {
			a.count(a.saveTotal, "duplicate")
			return pubsub.AckDecisionAck, nil
		}
		return pubsub.AckDecisionNack, err
	}
	a.count(a.saveTotal, "saved")
	return pubsub.AckDecisionAck, nil
}

// Stop gracefully drains the subscription. In-flight work completes
// against the still-open store (see Shutdown order in cmd.Run); anything
// interrupted stays unacked and redelivers on restart.
func (a *Aggregator) Stop() {
	for _, cc := range a.consumeCtxs {
		if cc != nil {
			cc.Drain()
		}
	}
	a.consumeCtxs = nil
}
