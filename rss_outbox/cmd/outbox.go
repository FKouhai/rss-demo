package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/FKouhai/rss-demo/libs/instrumentation"
	"github.com/FKouhai/rss-demo/libs/pubsub"
	"github.com/FKouhai/rss-outbox/store"
	"github.com/nats-io/nats.go/jetstream"
)

// tracer is the service-scoped helper for outbox spans.
var tracer = instrumentation.ForService("outbox")

type Outbox struct {
	nc          *pubsub.Client
	store       store.Store
	consumeCtxs []jetstream.ConsumeContext
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
	// One root span for the whole replay so a cold start reads as a single
	// trace instead of N orphan publishes.
	repCtx, repSpan := tracer.Start(ctx, "outbox.Replay")
	for _, e := range pending {
		if err := o.dispatch(repCtx, e); err != nil {
			repSpan.End()
			return err
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
	return nil
}

// dispatch publishes one stored entry. msgID is the row ID, so a repeated
// dispatch of the same row dedupes at the broker.
func (o *Outbox) dispatch(ctx context.Context, e store.Entry) error {
	dm := pubsub.DeliveryMessage{
		TenantID:   "default", // TODO(config service): resolve per routing rules
		OutboxID:   e.ID,
		WebhookURL: e.WebhookURL,
		FeedURL:    e.FeedURL,
		Title:      e.Title,
		Link:       e.Link,
	}
	payload, err := json.Marshal(dm)
	if err != nil {
		return fmt.Errorf("marshal delivery %s: %w", e.ID, err)
	}
	if _, err := o.nc.Publish(ctx, pubsub.SubjectNotificationRequest, payload, e.ID); err != nil {
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
		return pubsub.AckDecisionTerm, err
	}
	entry := store.Entry{
		ID:         item.MsgID(),
		FeedURL:    item.FeedURL,
		Title:      item.Title,
		Link:       item.Link,
		WebhookURL: item.WebhookURL,
		Status:     "pending",
		CreatedAt:  time.Now().UTC(),
	}
	if _, err := o.store.Save(ctx, entry); err != nil {
		if store.IsDuplicateKey(err) {
			return pubsub.AckDecisionAck, nil
		}
		return pubsub.AckDecisionNack, err
	}
	if err := o.dispatch(ctx, entry); err != nil {
		return pubsub.AckDecisionNack, err
	}
	return pubsub.AckDecisionAck, nil
}

// handleAck applies a delivery confirmation to the store.
// Unknown IDs Term (nothing to retry); store failures Nak for redelivery.
func (o *Outbox) handleAck(ctx context.Context, msg pubsub.Message) (pubsub.AckDecision, error) {
	var ack pubsub.AckReply
	if err := json.Unmarshal(msg.Data, &ack); err != nil {
		return pubsub.AckDecisionTerm, err
	}
	if ack.OutboxID == "" {
		return pubsub.AckDecisionTerm, errors.New("outbox: ack missing outbox_id")
	}
	var err error
	if ack.Status == pubsub.StatusDelivered {
		err = o.store.MarkDelivered(ctx, ack.OutboxID)
	} else {
		err = o.store.MarkFailed(ctx, ack.OutboxID, ack.Error)
	}
	if err != nil {
		return pubsub.AckDecisionNack, err
	}
	return pubsub.AckDecisionAck, nil
}

// Stop gracefully drains both subscriptions. In-flight work completes
// against the still-open store (see Shutdown order in cmd.Run); anything
// interrupted stays unacked and redelivers on restart.
func (o *Outbox) Stop() {
	for _, cc := range o.consumeCtxs {
		if cc != nil {
			cc.Drain()
		}
	}
	o.consumeCtxs = nil
}
