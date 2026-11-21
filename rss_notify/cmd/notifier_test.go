package cmd

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/FKouhai/rss-demo/libs/pubsub"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

var errTest = errors.New("test error")

// fakeMsg is a minimal jetstream.Msg for parse tests (no broker needed).
type fakeMsg struct{ data []byte }

func (m fakeMsg) Metadata() (*jetstream.MsgMetadata, error) { return nil, nil }
func (m fakeMsg) Data() []byte                              { return m.data }
func (m fakeMsg) Headers() nats.Header                      { return nil }
func (m fakeMsg) Subject() string                           { return "notifications.request" }
func (m fakeMsg) Reply() string                             { return "" }
func (m fakeMsg) Ack() error                                { return nil }
func (m fakeMsg) DoubleAck(context.Context) error           { return nil }
func (m fakeMsg) Nak() error                                { return nil }
func (m fakeMsg) NakWithDelay(time.Duration) error          { return nil }
func (m fakeMsg) InProgress() error                         { return nil }
func (m fakeMsg) Term() error                               { return nil }
func (m fakeMsg) TermWithReason(string) error               { return nil }

func TestFormatBatch_Single(t *testing.T) {
	got := formatBatch([]pubsub.DeliveryMessage{{OutboxID: "x", Title: "Hello", Link: "https://example.com/1"}})
	if want := "Hello\nhttps://example.com/1"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatBatch_MultipleJoined(t *testing.T) {
	got := formatBatch([]pubsub.DeliveryMessage{
		{OutboxID: "a", Title: "One", Link: "https://example.com/1"},
		{OutboxID: "b", Title: "Two", Link: "https://example.com/2"},
	})
	want := "One\nhttps://example.com/1\n\nTwo\nhttps://example.com/2"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatBatch_TruncatesWithMoreCount(t *testing.T) {
	var dms []pubsub.DeliveryMessage
	for i := 0; i < 20; i++ {
		dms = append(dms, pubsub.DeliveryMessage{
			OutboxID: "x",
			Title:    strings.Repeat("T", 100),
			Link:     "https://example.com/" + strings.Repeat("l", 100),
		})
	}
	got := formatBatch(dms)
	if len(got) > 2000 {
		t.Errorf("got %d chars, want <= 2000 (discord limit)", len(got))
	}
	// 20 items x ~221 chars: 8 fit fully, the rest fold into the tail.
	if n := strings.Count(got, "https://example.com/"); n != 8 {
		t.Errorf("got %d full links, want 8 with +12 more tail", n)
	}
	if !strings.Contains(got, "+12 more") {
		t.Errorf("expected +12 more tail, got %q", got)
	}
}

func TestFormatBatch_SkipsEmpty(t *testing.T) {
	got := formatBatch([]pubsub.DeliveryMessage{
		{OutboxID: "a"},
		{OutboxID: "b", Title: "Real", Link: "https://example.com/r"},
	})
	if got != "Real\nhttps://example.com/r" {
		t.Errorf("got %q, want only the non-empty entry", got)
	}
}

func TestClassifyWebhook(t *testing.T) {
	tests := []struct {
		name       string
		code       int
		err        error
		wantStatus pubsub.Status
		wantRetry  bool
	}{
		{"delivered 200", 200, nil, pubsub.StatusDelivered, false},
		{"delivered 204", 204, nil, pubsub.StatusDelivered, false},
		{"rate limited retries", 429, errTest, pubsub.StatusFailed, true},
		{"network error retries", 0, errTest, pubsub.StatusFailed, true},
		{"client error fails", 400, nil, pubsub.StatusFailed, false},
		{"server error fails without retry", 500, nil, pubsub.StatusFailed, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			status, detail, retry := classifyWebhook(tc.code, tc.err)
			if status != tc.wantStatus {
				t.Errorf("status = %v, want %v", status, tc.wantStatus)
			}
			if retry != tc.wantRetry {
				t.Errorf("retry = %v, want %v", retry, tc.wantRetry)
			}
			if tc.wantStatus == pubsub.StatusFailed && detail == "" {
				t.Error("failed classification must carry a detail message")
			}
			if tc.wantStatus == pubsub.StatusDelivered && detail != "" {
				t.Errorf("delivered must have empty detail, got %q", detail)
			}
		})
	}
}

func TestParseDelivery(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		msg := fakeMsg{data: []byte(`{"tenant_id":"t","outbox_id":"x1","title":"T","link":"https://example.com"}`)}
		dm, ok := parseDelivery(msg)
		if !ok {
			t.Fatal("expected ok for valid payload")
		}
		if dm.OutboxID != "x1" || dm.Title != "T" {
			t.Errorf("decoded %+v, want outbox x1 / title T", dm)
		}
	})
	t.Run("bad json is poison", func(t *testing.T) {
		if _, ok := parseDelivery(fakeMsg{data: []byte("{nope")}); ok {
			t.Error("expected not-ok for malformed JSON")
		}
	})
	t.Run("missing id is poison", func(t *testing.T) {
		if _, ok := parseDelivery(fakeMsg{data: []byte(`{"title":"T","link":"https://example.com"}`)}); ok {
			t.Error("expected not-ok for missing outbox_id")
		}
	})
	t.Run("contentless is poison", func(t *testing.T) {
		if _, ok := parseDelivery(fakeMsg{data: []byte(`{"outbox_id":"x"}`)}); ok {
			t.Error("expected not-ok for titleless linkless payload")
		}
	})
}

func TestNotifyStart_NilNATS(t *testing.T) {
	n := NewNotify(nil, 0)
	if err := n.Start(context.Background()); err == nil {
		t.Error("expected error for nil NATS client, got nil")
	}
}

func TestStaleItem(t *testing.T) {
	now := time.Date(2026, 11, 21, 20, 0, 0, 0, time.UTC)
	old := now.Add(-3 * time.Hour).Format(time.RFC3339)
	fresh := now.Add(-time.Hour).Format(time.RFC3339)
	if !staleItem(old, 2*time.Hour, now) {
		t.Error("3h-old item with 2h max should be stale")
	}
	if staleItem(fresh, 2*time.Hour, now) {
		t.Error("1h-old item with 2h max should be fresh")
	}
	if staleItem("", 2*time.Hour, now) {
		t.Error("empty PublishedAt must fail open as fresh")
	}
	if staleItem("not a date", 2*time.Hour, now) {
		t.Error("unparseable PublishedAt must fail open as fresh")
	}
	if staleItem(old, 0, now) {
		t.Error("zero maxAge disables the gate")
	}
}

func TestNotifyStop_Empty(t *testing.T) {
	n := &Notify{}
	n.Stop() // must not panic with no loop running
}
