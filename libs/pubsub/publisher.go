package pubsub

import (
	"context"
	"errors"
	"net/http"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
)

// Publish wrapper for producers
func (c *Client) Publish(ctx context.Context, subject string, payload []byte, msgID string) (*jetstream.PubAck, error) {
	// Using publishMsg for the headers
	if c.JsContext == nil {
		return nil, errors.New("no jetstream context is available")
	}
	if msgID == "" {
		return nil, errors.New("message id is empty, expected a value")
	}

	// Starting tracer
	spanCtx, span := tracer.StartProducer(ctx, "pubsub.Publish")
	defer span.End()

	// common attributes for the producers
	span.SetAttributes(
		attribute.String("messaging.system", "nats"),
		attribute.String("messaging.destination", subject),
		attribute.String("messaging.message_id", msgID),
	)

	msg := &nats.Msg{
		Data:    payload,
		Subject: subject,
		Header: nats.Header{
			"Nats-Msg-Id": []string{msgID},
		},
	}

	// propagate the span context for correlation across services
	otel.GetTextMapPropagator().Inject(
		spanCtx,
		propagation.HeaderCarrier(http.Header(msg.Header)),
	)

	ack, err := c.JsContext.PublishMsg(spanCtx, msg)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	span.SetAttributes(
		attribute.String("messaging.nats.stream", ack.Stream),
		attribute.Int64("messaging.nats.sequence", int64(ack.Sequence)),
		attribute.Bool("messaging.nats.duplicate", ack.Duplicate),
	)
	return ack, nil
}
