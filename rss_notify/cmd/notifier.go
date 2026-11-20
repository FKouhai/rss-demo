package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/FKouhai/rss-demo/libs/instrumentation"
	log "github.com/FKouhai/rss-demo/libs/logger"
	"github.com/FKouhai/rss-demo/libs/pubsub"
	webhookpush "github.com/FKouhai/rss-notify/webhookPush"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const (
	// notifyBatchMax caps one Discord post. The broker holds the backlog;
	// the batch is only a grouping window, not storage.
	notifyBatchMax = 10
	// notifyFetchWait bounds idle polling so shutdown stays responsive
	// (Fetch itself takes no ctx).
	notifyFetchWait = time.Second
	// notifyErrorBackoff spaces retries when the broker itself errors.
	notifyErrorBackoff = 5 * time.Second
	// notifyRetryDelay spaces redelivery after a retryable post failure
	// (rate limit / transport error): the message definitely wasn't
	// delivered, so redeliver rather than mark failed.
	notifyRetryDelay = 30 * time.Second
	// discordMaxLen is Discord's message character limit.
	discordMaxLen = 2000
)

var tracer = instrumentation.ForService("notify")

type Notify struct {
	nc        *pubsub.Client
	runCtx    context.Context
	runCancel context.CancelFunc
}

// NewNotify constructor for Notify struct
func NewNotify(nc *pubsub.Client) *Notify {
	return &Notify{nc: nc}
}

// Start ensures streams + consumer, then runs the fetch loop.
// Ready fires at HTTP listen (after Start returns): the subscription exists
// and the loop is running, so delivery begins immediately.
func (n *Notify) Start(ctx context.Context) error {
	if n.nc == nil {
		return errors.New("notify: NATS client is nil")
	}

	// Streams are shared infrastructure: ensuring what you touch is
	// idempotent, so startup order vs outbox doesn't matter.
	if _, err := n.nc.EnsureStream(ctx, pubsub.NotificationsStreamConfig()); err != nil {
		return fmt.Errorf("ensure notifications stream: %w", err)
	}
	if _, err := n.nc.EnsureStream(ctx, pubsub.ACKStreamConfig()); err != nil {
		return fmt.Errorf("ensure acks stream: %w", err)
	}

	// This service's OWN durable on the notifications stream.
	if _, err := n.nc.EnsureConsumer(ctx, string(pubsub.StreamNotifications),
		pubsub.ConsumerFor("notify-deliveries", pubsub.SubjectNotificationRequest)); err != nil {
		return fmt.Errorf("ensure notify-deliveries consumer: %w", err)
	}

	runCtx, cancel := context.WithCancel(ctx)
	n.runCtx = runCtx
	n.runCancel = cancel
	go n.loop(runCtx)
	return nil
}

// Stop cancels the fetch loop. In-flight batches finish or abort on their
// own; anything unacked redelivers on restart, so nothing is lost.
func (n *Notify) Stop() {
	if n.runCancel != nil {
		n.runCancel()
		n.runCancel = nil
	}
}

// loop pulls deliveries in batches until ctx ends.
func (n *Notify) loop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		msgs, err := n.nc.FetchBatch(ctx, string(pubsub.StreamNotifications), "notify-deliveries", notifyBatchMax, notifyFetchWait)
		if err != nil {
			if errors.Is(err, nats.ErrTimeout) {
				continue // idle: nothing arrived in time
			}
			log.ErrorFmt("notify fetch: %v", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(notifyErrorBackoff):
			}
			continue
		}
		if len(msgs) == 0 {
			continue
		}
		n.dispatchBatch(ctx, msgs)
	}
}

// item pairs a parsed delivery with its NATS message and consumer span.
type item struct {
	dm   pubsub.DeliveryMessage
	msg  jetstream.Msg
	span trace.Span
}

// parseDelivery decodes a delivery; ok=false means poison (undecodable,
// missing ID, or nothing deliverable) — Term, never redeliver.
func parseDelivery(m jetstream.Msg) (pubsub.DeliveryMessage, bool) {
	var dm pubsub.DeliveryMessage
	if err := json.Unmarshal(m.Data(), &dm); err != nil {
		return dm, false
	}
	if dm.OutboxID == "" || (dm.Title == "" && dm.Link == "") {
		return dm, false
	}
	return dm, true
}

// consumerSpan extracts the producer trace context from the message headers
// and starts a consumer span so each delivery stays joined to its
// poller/outbox trace.
func consumerSpan(m jetstream.Msg) trace.Span {
	parentCtx := otel.GetTextMapPropagator().Extract(
		context.Background(),
		propagation.HeaderCarrier(http.Header(m.Headers())),
	)
	_, span := tracer.StartConsumer(parentCtx, "notify.consume")
	span.SetAttributes(attribute.String("messaging.destination", m.Subject()))
	return span
}

// dispatchBatch groups one fetch by destination webhook and posts a single
// combined Discord message per group, then settles each delivery. A panic
// Naks everything fetched so the broker redelivers (MaxDeliver bounds it).
func (n *Notify) dispatchBatch(ctx context.Context, msgs []jetstream.Msg) {
	defer func() {
		if r := recover(); r != nil {
			for _, m := range msgs {
				_ = m.Nak()
			}
		}
	}()

	_, batchSpan := tracer.Start(ctx, "notify.dispatch")
	defer batchSpan.End()
	batchSpan.SetAttributes(attribute.Int("messages.received", len(msgs)))

	groups := make(map[string][]item)
	var order []string
	for _, m := range msgs {
		span := consumerSpan(m)
		dm, ok := parseDelivery(m)
		if !ok {
			span.End()
			_ = m.Term()
			continue
		}
		span.SetAttributes(attribute.String("outbox.id", dm.OutboxID))
		if _, seen := groups[dm.WebhookURL]; !seen {
			order = append(order, dm.WebhookURL)
		}
		groups[dm.WebhookURL] = append(groups[dm.WebhookURL], item{dm: dm, msg: m, span: span})
	}
	for _, url := range order {
		n.flushGroup(ctx, url, groups[url])
	}
}

// ackDelivery publishes the outbox ack and settles the NATS message.
// Publish failure Naks (redeliver; outbox UPDATEs are idempotent).
func (n *Notify) ackDelivery(ctx context.Context, it item, status pubsub.Status, detail string) {
	defer it.span.End()
	ack := pubsub.AckReply{TenantID: it.dm.TenantID, OutboxID: it.dm.OutboxID, Status: status, Error: detail}
	payload, err := json.Marshal(ack)
	if err != nil {
		it.span.RecordError(err)
		_ = it.msg.Nak()
		return
	}
	if _, err := n.nc.Publish(ctx, pubsub.SubjectAcksDelivered, payload, it.dm.OutboxID); err != nil {
		it.span.RecordError(err)
		_ = it.msg.Nak()
		return
	}
	_ = it.msg.Ack()
}

// formatBatch combines deliveries into one Discord message, capped at the
// character limit with a "+N more" tail for the overflow.
func formatBatch(dms []pubsub.DeliveryMessage) string {
	entries := make([]string, 0, len(dms))
	for _, dm := range dms {
		var e string
		switch {
		case dm.Title != "" && dm.Link != "":
			e = dm.Title + "\n" + dm.Link
		case dm.Title != "":
			e = dm.Title
		default:
			e = dm.Link
		}
		if e != "" {
			entries = append(entries, e)
		}
	}
	var b strings.Builder
	shown := 0
	for _, e := range entries {
		sep := ""
		if b.Len() > 0 {
			sep = "\n\n"
		}
		if b.Len()+len(sep)+len(e) > discordMaxLen {
			break
		}
		b.WriteString(sep + e)
		shown++
	}
	if shown < len(entries) {
		tail := fmt.Sprintf("\n…+%d more", len(entries)-shown)
		out := b.String()
		if len(out)+len(tail) > discordMaxLen {
			out = out[:discordMaxLen-len(tail)]
		}
		return out + tail
	}
	return b.String()
}

// classifyWebhook maps a webhook POST outcome to an outbox ack. Only outcomes
// that prove nothing was delivered (transport errors, 429) ask for redelivery;
// everything else settles the delivery so a duplicate post can't happen.
func classifyWebhook(code int, err error) (status pubsub.Status, detail string, retry bool) {
	if err != nil {
		return pubsub.StatusFailed, err.Error(), true
	}
	switch {
	case code == http.StatusTooManyRequests:
		return pubsub.StatusFailed, "discord rate limited (429)", true
	case code >= 200 && code < 300:
		return pubsub.StatusDelivered, "", false
	default:
		return pubsub.StatusFailed, fmt.Sprintf("discord responded %d", code), false
	}
}

// flushGroup posts one combined message for a single destination webhook,
// then settles every delivery in the group.
func (n *Notify) flushGroup(ctx context.Context, webhookURL string, items []item) {
	dms := make([]pubsub.DeliveryMessage, 0, len(items))
	for _, it := range items {
		dms = append(dms, it.dm)
	}

	var status pubsub.Status
	var detail string
	var retry bool
	if webhookURL == "" {
		// No destination: nothing to post; mark failed so outbox shows it.
		status, detail, retry = pubsub.StatusFailed, "no webhook configured", false
	} else {
		var code int
		var err error
		d := webhookpush.DiscordNotification{WebHookURL: webhookURL}
		code, err = d.SendNotification(ctx, []string{formatBatch(dms)})
		status, detail, retry = classifyWebhook(code, err)
	}

	for _, it := range items {
		if retry {
			it.span.End()
			_ = it.msg.NakWithDelay(notifyRetryDelay)
			continue
		}
		n.ackDelivery(ctx, it, status, detail)
	}
}
