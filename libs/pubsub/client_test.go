package pubsub

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// noopTracer is a non-recording tracer for adapter tests.
func noopTracer() trace.Tracer {
	return noop.NewTracerProvider().Tracer("test")
}

func TestResolveServerURL(t *testing.T) {
	if got := resolveServerURL(""); got != nats.DefaultURL {
		t.Errorf("empty = %q, want default %q", got, nats.DefaultURL)
	}
	if got := resolveServerURL("nats://custom:4222"); got != "nats://custom:4222" {
		t.Errorf("explicit = %q, want nats://custom:4222", got)
	}
}

func TestNewClient_InvalidURL(t *testing.T) {
	// Port 1 is never bound: refused immediately, no broker or timeout needed.
	if _, err := NewClient("nats://127.0.0.1:1", RssOptions{ServiceName: "test"}); err == nil {
		t.Error("expected error for unreachable server, got nil")
	}
}

func TestClient_ZeroValueCloseDrain(t *testing.T) {
	var c Client
	c.Close() // must not panic on nil connection
	if err := c.Drain(); err != nil {
		t.Errorf("Drain on zero Client = %v, want nil", err)
	}
}

func TestPublish_NilJetStream(t *testing.T) {
	c := &Client{}
	if _, err := c.Publish(context.Background(), SubjectFeedsNew, []byte("{}"), "id-1"); err == nil {
		t.Error("expected error with nil JsContext, got nil")
	}
}

func TestEnsure_NilJetStream(t *testing.T) {
	c := &Client{}
	ctx := context.Background()
	if _, err := c.EnsureStream(ctx, jetstream.StreamConfig{Name: "TEST"}); err == nil {
		t.Error("EnsureStream: expected error with nil JsContext, got nil")
	}
	if _, err := c.EnsureConsumer(ctx, "TEST", jetstream.ConsumerConfig{Durable: "test"}); err == nil {
		t.Error("EnsureConsumer: expected error with nil JsContext, got nil")
	}
}

func TestConsume_NilJetStream(t *testing.T) {
	c := &Client{}
	handler := func(ctx context.Context, msg Message) (AckDecision, error) {
		return AckDecisionAck, nil
	}
	if _, err := c.Consume(context.Background(), "TEST", "test", handler); err == nil {
		t.Error("expected error with nil JsContext, got nil")
	}
}

func TestProcessMessage_AckPath(t *testing.T) {
	msg := newMockMsg()
	handler := func(ctx context.Context, m Message) (AckDecision, error) {
		if string(m.Data) != "payload" {
			t.Errorf("handler received data %q, want %q", m.Data, "payload")
		}
		return AckDecisionAck, nil
	}
	processMessage(noopTracer(), handler, msg, ConsumeOptions{})
	if !msg.acked {
		t.Error("expected Ack after successful handler, got none")
	}
}

func TestProcessMessage_NakDelayOption(t *testing.T) {
	msg := newMockMsg()
	handler := func(ctx context.Context, m Message) (AckDecision, error) {
		return AckDecisionNack, nil
	}
	processMessage(noopTracer(), handler, msg, ConsumeOptions{NakDelay: 50 * time.Millisecond})
	if !msg.nacked {
		t.Fatal("expected Nak after Nack decision, got none")
	}
	if msg.nakDelay != 50*time.Millisecond {
		t.Errorf("nak delay = %v, want 50ms (NakWithDelay not used)", msg.nakDelay)
	}
}

func TestProcessMessage_NoDelayUsesPlainNak(t *testing.T) {
	msg := newMockMsg()
	handler := func(ctx context.Context, m Message) (AckDecision, error) {
		return AckDecisionNack, nil
	}
	processMessage(noopTracer(), handler, msg, ConsumeOptions{})
	if !msg.nacked {
		t.Fatal("expected Nak after Nack decision, got none")
	}
	if msg.nakDelay != 0 {
		t.Errorf("nak delay = %v, want 0 (plain Nak expected)", msg.nakDelay)
	}
}
