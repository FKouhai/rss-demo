package pubsub

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"go.opentelemetry.io/otel/trace"
)

// mockMsg is a minimal jetstream.Msg implementation that records which ack
// method was called. It satisfies the full interface; methods not relevant to
// the ack-mapping tests return zero values.
type mockMsg struct {
	data     []byte
	subject  string
	header   nats.Header
	acked    bool
	nacked   bool
	nakDelay time.Duration
	termed   bool
}

func newMockMsg() *mockMsg {
	return &mockMsg{
		data:    []byte("payload"),
		subject: "feeds.new",
		header:  nats.Header{},
	}
}

func (m *mockMsg) Metadata() (*jetstream.MsgMetadata, error) { return nil, errors.New("no metadata") }
func (m *mockMsg) Data() []byte                              { return m.data }
func (m *mockMsg) Headers() nats.Header                      { return m.header }
func (m *mockMsg) Subject() string                           { return m.subject }
func (m *mockMsg) Reply() string                             { return "" }
func (m *mockMsg) Ack() error                                { m.acked = true; return nil }
func (m *mockMsg) DoubleAck(context.Context) error           { m.acked = true; return nil }
func (m *mockMsg) Nak() error                                { m.nacked = true; return nil }
func (m *mockMsg) NakWithDelay(d time.Duration) error {
	m.nacked = true
	m.nakDelay = d
	return nil
}
func (m *mockMsg) InProgress() error           { return nil }
func (m *mockMsg) Term() error                 { m.termed = true; return nil }
func (m *mockMsg) TermWithReason(string) error { m.termed = true; return nil }

// noopSpan is a non-recording span so RecordError/SetAttributes are no-ops.
func noopSpan() trace.Span { return trace.SpanFromContext(context.Background()) }

func TestApplyAckDecision(t *testing.T) {
	tests := []struct {
		name     string
		decision AckDecision
		hErr     error
		wantAck  bool
		wantNak  bool
		wantTerm bool
	}{
		{
			name:     "ack on success",
			decision: AckDecisionAck,
			hErr:     nil,
			wantAck:  true,
		},
		{
			name:     "nak on explicit nack",
			decision: AckDecisionNack,
			hErr:     errors.New("transient"),
			wantNak:  true,
		},
		{
			name:     "term on explicit term",
			decision: AckDecisionTerm,
			hErr:     errors.New("permanent"),
			wantTerm: true,
		},
		{
			name:     "nak when handler errored despite ack decision",
			decision: AckDecisionAck,
			hErr:     errors.New("caller mistake"),
			wantNak:  true,
		},
		{
			name:     "nak on unknown decision (safe default redeliver)",
			decision: AckDecision(99),
			hErr:     nil,
			wantNak:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := newMockMsg()
			applyAckDecision(noopSpan(), tc.decision, tc.hErr, msg, ConsumeOptions{})

			if msg.acked != tc.wantAck {
				t.Errorf("acked = %v, want %v", msg.acked, tc.wantAck)
			}
			if msg.nacked != tc.wantNak {
				t.Errorf("nacked = %v, want %v", msg.nacked, tc.wantNak)
			}
			if msg.termed != tc.wantTerm {
				t.Errorf("termed = %v, want %v", msg.termed, tc.wantTerm)
			}
		})
	}
}

func TestApplyAckDecisionExactlyOneAckMethod(t *testing.T) {
	// No branch should call more than one of Ack/Nak/Term.
	cases := []AckDecision{AckDecisionAck, AckDecisionNack, AckDecisionTerm, AckDecision(99)}
	for _, d := range cases {
		msg := newMockMsg()
		applyAckDecision(noopSpan(), d, nil, msg, ConsumeOptions{})
		called := 0
		if msg.acked {
			called++
		}
		if msg.nacked {
			called++
		}
		if msg.termed {
			called++
		}
		if called != 1 {
			t.Errorf("decision %d: expected exactly one ack method called, got %d", d, called)
		}
	}
}

func TestInvokeHandler_Success(t *testing.T) {
	wantData := []byte("hello")
	handler := func(ctx context.Context, msg Message) (AckDecision, error) {
		if string(msg.Data) != string(wantData) {
			t.Errorf("handler received data %q, want %q", msg.Data, wantData)
		}
		if msg.Subject != "feeds.new" {
			t.Errorf("handler received subject %q, want %q", msg.Subject, "feeds.new")
		}
		return AckDecisionAck, nil
	}

	msg := &mockMsg{data: wantData, subject: "feeds.new", header: nats.Header{}}
	decision, err := invokeHandler(context.Background(), noopSpan(), handler, msg)
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if decision != AckDecisionAck {
		t.Errorf("expected AckDecisionAck, got %d", decision)
	}
}

func TestInvokeHandler_PanicRecovered(t *testing.T) {
	handler := func(ctx context.Context, msg Message) (AckDecision, error) {
		panic("boom")
	}

	msg := newMockMsg()
	decision, err := invokeHandler(context.Background(), noopSpan(), handler, msg)

	if err == nil {
		t.Fatal("expected error from recovered panic, got nil")
	}
	if decision != AckDecisionNack {
		t.Errorf("expected AckDecisionNack after panic, got %d", decision)
	}
}

func TestInvokeHandler_PanicDoesNotPropagate(t *testing.T) {
	handler := func(ctx context.Context, msg Message) (AckDecision, error) {
		panic("should not escape invokeHandler")
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic propagated out of invokeHandler: %v", r)
		}
	}()

	_, _ = invokeHandler(context.Background(), noopSpan(), handler, newMockMsg())
}

func TestRecordAckErr_OnlyRecordsOnError(t *testing.T) {
	// No error -> no panic, no side effect. We can't assert span.RecordError
	// was not called (noop span), but we can ensure the nil path returns cleanly.
	assertNotPanics := func(t *testing.T, fn func()) {
		t.Helper()
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("panicked: %v", r)
			}
		}()
		fn()
	}
	assertNotPanics(t, func() { recordAckErr(noopSpan(), nil, "ack") })
	assertNotPanics(t, func() {
		recordAckErr(noopSpan(), errors.New("ack failed"), "ack")
	})
}

func TestRecordHandlerErr(t *testing.T) {
	// nil error -> no-op; non-nil error -> must not panic on noop span.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	recordHandlerErr(noopSpan(), nil)
	recordHandlerErr(noopSpan(), errors.New("handler failed"))
}
