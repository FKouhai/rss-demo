package pubsub

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/FKouhai/rss-demo/libs/instrumentation"
	log "github.com/FKouhai/rss-demo/libs/logger"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// AckDecision ack that the handler wants the wrapper to send
type AckDecision int

const (
	// AckDecisionAck processed successfully, remove from stream
	AckDecisionAck AckDecision = iota
	// AckDecisionNack transient failure, redeliver
	AckDecisionNack
	// AckDecisionTerm permanent failure, don't redeliver
	AckDecisionTerm
)

// Message is the decoded payload handed to a handler.
// It deliberately does not expose Ack/Nak/Term — the wrapper owns the ACK
// lifecycle so callers can't forget or double-ack.
type Message struct {
	Subject string
	Data    []byte
	Header  nats.Header
}

// MessageHandler is the callback a consumer service implements. It decodes the
// payload, does its work, and returns the ack decision (and any error).
// The wrapper maps the decision to msg.Ack/Nak/Term and records the error
// on the consumer span.
type MessageHandler func(ctx context.Context, msg Message) (AckDecision, error)

// ConsumeOptions tunes a Consume loop. Zero value preserves the historical
// behavior (immediate Nak on failure).
type ConsumeOptions struct {
	// NakDelay, when positive, redelivers via NakWithDelay instead of an
	// immediate Nak, spacing out retries for transient failures.
	NakDelay time.Duration
}

// Consume starts a background consume loop on an existing durable consumer
// (created ahead of time via EnsureConsumer). It returns a ConsumeContext the
// caller uses to Stop/Drain at shutdown. The handler is invoked for each
// message; the wrapper extracts trace context, starts a consumer span, and
// owns the Ack/Nak/Term lifecycle based on the handler's returned AckDecision.
func (c *Client) Consume(
	ctx context.Context,
	streamName, consumerName string,
	handler MessageHandler,
	opts ...ConsumeOptions,
) (jetstream.ConsumeContext, error) {
	if c.JsContext == nil {
		return nil, errors.New("no jetstream context is available")
	}
	if handler == nil {
		return nil, errors.New("handler cannot be nil")
	}
	var opt ConsumeOptions
	if len(opts) > 0 {
		opt = opts[0]
	}

	regCtx, regSpan := tracer.Start(ctx, "pubsub.Consume")
	defer regSpan.End()
	setConsumeRegistrationAttrs(regSpan, streamName, consumerName)

	cons, err := c.JsContext.Consumer(regCtx, streamName, consumerName)
	if err != nil {
		regSpan.RecordError(err)
		return nil, err
	}

	// Register the callback. Returns immediately; the adapter runs in a library
	// goroutine for every delivered message.
	cc, err := cons.Consume(func(msg jetstream.Msg) {
		processMessage(tracer, handler, msg, opt)
	})
	if err != nil {
		regSpan.RecordError(err)
		return nil, err
	}
	return cc, nil
}

// FetchBatch pulls up to max messages from an existing durable consumer,
// waiting up to wait for arrival. Unlike Consume it owns no ack lifecycle:
// the caller acks each message individually, which makes it the right
// primitive for batch dispatch where one action covers many messages.
// An empty batch surfaces as nats.ErrTimeout; anything else is a real error.
// No span is started here (fetch loops call this every second); span the
// batch work at the call site instead.
func (c *Client) FetchBatch(ctx context.Context, streamName, consumerName string, max int, wait time.Duration) ([]jetstream.Msg, error) {
	if c.JsContext == nil {
		return nil, errors.New("no jetstream context is available")
	}
	if max <= 0 {
		return nil, errors.New("max must be positive")
	}

	cons, err := c.JsContext.Consumer(ctx, streamName, consumerName)
	if err != nil {
		return nil, err
	}
	batch, err := cons.Fetch(max, jetstream.FetchMaxWait(wait))
	if err != nil {
		return nil, err
	}
	var msgs []jetstream.Msg
	for m := range batch.Messages() {
		msgs = append(msgs, m)
	}
	if err := batch.Error(); err != nil {
		return msgs, err
	}
	return msgs, nil
}

// processMessage is the per-message adapter body: extract trace, start a
// consumer span, invoke the handler, and map the returned decision to an ack.
func processMessage(tracer instrumentation.Tracer, handler MessageHandler, msg jetstream.Msg, opts ConsumeOptions) {
	spanCtx, span := startConsumerSpan(tracer, msg)
	defer span.End()

	decision, hErr := invokeHandler(spanCtx, span, handler, msg)
	applyAckDecision(span, decision, hErr, msg, opts)
}

// startConsumerSpan extracts the propagated trace context from the message
// headers (the mirror of Publish's Inject) and starts a SpanKindConsumer span
// linked to that parent, decorated with NATS messaging attributes.
func startConsumerSpan(tracer instrumentation.Tracer, msg jetstream.Msg) (context.Context, trace.Span) {
	parentCtx := otel.GetTextMapPropagator().Extract(
		context.Background(),
		propagation.HeaderCarrier(http.Header(msg.Headers())),
	)
	spanCtx, span := tracer.StartConsumer(parentCtx, "pubsub.Consume.message")
	span.SetAttributes(
		attribute.String("messaging.system", "nats"),
		attribute.String("messaging.destination", msg.Subject()),
	)
	setConsumeMetadata(span, msg)
	return spanCtx, span
}

// setConsumeMetadata decorates the span with stream/consumer sequence and
// delivery count. Best-effort: missing metadata is silently skipped.
func setConsumeMetadata(span trace.Span, msg jetstream.Msg) {
	meta, err := msg.Metadata()
	if err != nil || meta == nil {
		return
	}
	span.SetAttributes(
		attribute.Int64("messaging.nats.stream_sequence", int64(meta.Sequence.Stream)),
		attribute.Int64("messaging.nats.consumer_sequence", int64(meta.Sequence.Consumer)),
		attribute.Int64("messaging.nats.num_delivered", int64(meta.NumDelivered)),
	)
}

// invokeHandler decodes the message, calls the handler, and recovers from
// panics so a poison message cannot kill the consume loop. On panic it
// records the error on the span and returns a Nack decision so the broker
// redelivers (MaxDeliver on the consumer is the eventual-Term backstop).
func invokeHandler(spanCtx context.Context, span trace.Span, handler MessageHandler, msg jetstream.Msg) (decision AckDecision, hErr error) {
	defer func() {
		if r := recover(); r != nil {
			hErr = errors.New("handler panic")
			decision = AckDecisionNack
			span.RecordError(hErr)
			log.ErrorFmt("consume handler panic recovered: %v", r)
		}
	}()
	return handler(spanCtx, Message{
		Subject: msg.Subject(),
		Data:    msg.Data(),
		Header:  msg.Headers(),
	})
}

// applyAckDecision maps the handler's (decision, error) to the broker ACK
// call, using safer-to-redeliver defaults on ambiguous inputs.
func applyAckDecision(span trace.Span, decision AckDecision, hErr error, msg jetstream.Msg, opts ConsumeOptions) {
	nak := func() error {
		if opts.NakDelay > 0 {
			return msg.NakWithDelay(opts.NakDelay)
		}
		return msg.Nak()
	}
	switch {
	case decision == AckDecisionAck && hErr == nil:
		recordAckErr(span, msg.Ack(), "ack")
	case decision == AckDecisionTerm:
		recordAckErr(span, msg.Term(), "term")
		recordHandlerErr(span, hErr)
	case decision == AckDecisionNack, decision == AckDecisionAck && hErr != nil:
		// Nack covers both explicit Nack and the caller-mistake case
		// (success decision returned alongside an error).
		recordAckErr(span, nak(), "nak")
		recordHandlerErr(span, hErr)
	default:
		recordAckErr(span, nak(), "nak (unknown decision)")
	}
}

// recordAckErr records a failed ACK call on the span and logs it.
func recordAckErr(span trace.Span, err error, kind string) {
	if err == nil {
		return
	}
	span.RecordError(err)
	log.ErrorFmt("failed to %s message: %v", kind, err)
}

// recordHandlerErr records a handler-returned error on the span and logs
// it. Logs matter here: a Nack without a log line is invisible everywhere
// except Jaeger, which once hid a sqlite-locking outage behind clean logs.
func recordHandlerErr(span trace.Span, err error) {
	if err != nil {
		span.RecordError(err)
		log.ErrorFmt("consume handler error: %v", err)
	}
}

// setConsumeRegistrationAttrs decorates the Consume registration span with the
// stream and consumer names.
func setConsumeRegistrationAttrs(span trace.Span, streamName, consumerName string) {
	span.SetAttributes(
		attribute.String("messaging.system", "nats"),
		attribute.String("messaging.nats.stream", streamName),
		attribute.String("messaging.nats.consumer", consumerName),
	)
}
