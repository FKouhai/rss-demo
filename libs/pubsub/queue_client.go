package pubsub

import (
	"context"
	"errors"
	"time"

	"github.com/FKouhai/rss-demo/libs/instrumentation"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Client defines the shape of a re-usable nats client
type Client struct {
	Connection *nats.Conn
	JsContext  jetstream.JetStream
}

// RssOptions opts passed down to the NATS client
// Using a struct allows for a common signature across services
// It allows for the extra flexibility of updating the struct when need be
type RssOptions struct {
	ServiceName          string
	ReconnectWait        time.Duration
	MaxReconnect         int
	DisconnectErrHandler nats.ConnErrHandler
	ErrHandler           nats.ErrHandler
}

// resolveServerURL returns the given server URL, defaulting to the local
// broker so callers don't each repeat the fallback.
func resolveServerURL(server string) string {
	if server == "" {
		return nats.DefaultURL
	}
	return server
}

// NewClient returns an initialised nats client
func NewClient(server string, opts RssOptions) (*Client, error) {
	if opts.MaxReconnect == 0 {
		opts.MaxReconnect = -1
	}
	if opts.ReconnectWait == 0 {
		opts.ReconnectWait = 2 * time.Second
	}

	conn, err := nats.Connect(resolveServerURL(server),
		nats.Name(opts.ServiceName),
		nats.ReconnectWait(opts.ReconnectWait),
		nats.MaxReconnects(opts.MaxReconnect),
		nats.DisconnectErrHandler(opts.DisconnectErrHandler),
		nats.ErrorHandler(opts.ErrHandler),
	)
	if err != nil {
		return nil, err
	}
	js, err := jetstream.New(conn)
	if err != nil {
		conn.Close()
		return nil, err
	}
	return &Client{
		Connection: conn,
		JsContext:  js,
	}, nil
}

// Closes the connection with the NATS server
func (c *Client) Close() {
	if c.Connection != nil {
		c.Connection.Close()
	}
}

// Drains the queue, returning the drain error if any.
func (c *Client) Drain() error {
	if c.Connection != nil {
		return c.Connection.Drain()
	}
	return nil
}

// EnsureStream bootstrap function to create streams in nats
func (c *Client) EnsureStream(ctx context.Context, cfg jetstream.StreamConfig) (jetstream.Stream, error) {
	if c.JsContext == nil {
		return nil, errors.New("jetstream context not available")
	}

	tracer := instrumentation.GetTracer("pubsub")
	spanCtx, span := tracer.Start(ctx, "pubsub.EnsureStream", trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()

	span.SetAttributes(
		attribute.String("stream.name", cfg.Name),
		attribute.StringSlice("stream.subjects", cfg.Subjects),
	)
	stream, err := c.JsContext.CreateOrUpdateStream(spanCtx, cfg)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	return stream, nil
}

// EnsureConsumer bootstrap function to create consumers in nats
func (c *Client) EnsureConsumer(ctx context.Context, streamName string, cfg jetstream.ConsumerConfig) (jetstream.Consumer, error) {
	if c.JsContext == nil {
		return nil, errors.New("jetstream context not available")
	}

	tracer := instrumentation.GetTracer("pubsub")
	spanCtx, span := tracer.Start(ctx, "pubsub.EnsureConsumer", trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()

	span.SetAttributes(
		attribute.String("consumer.name", cfg.Durable),
		attribute.String("consumer.stream", streamName),
		attribute.StringSlice("consumer.filterSubjects", cfg.FilterSubjects),
	)

	consumer, err := c.JsContext.CreateOrUpdateConsumer(spanCtx, streamName, cfg)

	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	return consumer, nil
}
