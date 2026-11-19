package pubsub

import (
	"encoding/json"
	"reflect"
	"regexp"
	"testing"
)

var hex64 = regexp.MustCompile(`^[0-9a-f]{64}$`)

func TestRSSItem_MsgIDStable(t *testing.T) {
	// Fixed vector: any change to the hash inputs or encoding breaks this
	// test on purpose (dedup keys must be stable across deploys).
	r := RSSItem{FeedURL: "https://example.com/feed", Link: "https://example.com/item-1"}
	want := "359ccf0b8f97cc84cc32a9d33f34864dc537f48055cf4ef15d49206ef369e712"
	if got := r.MsgID(); got != want {
		t.Errorf("MsgID = %q, want %q", got, want)
	}
}

func TestRSSItem_MsgIDHexEncoded(t *testing.T) {
	// Guards the raw-bytes regression: the ID travels in NATS headers and
	// JSON, so it must be printable hex, never arbitrary bytes.
	r := RSSItem{FeedURL: "https://example.com/feed", Link: "https://example.com/üñîçødé-îtéms?x=1&y=2"}
	if got := r.MsgID(); !hex64.MatchString(got) {
		t.Errorf("MsgID = %q, want 64 lowercase hex chars", got)
	}
}

func TestRSSItem_MsgIDDistinct(t *testing.T) {
	base := RSSItem{FeedURL: "https://example.com/feed", Link: "https://example.com/a"}
	cases := map[string]RSSItem{
		"different link": {FeedURL: "https://example.com/feed", Link: "https://example.com/b"},
		"different feed": {FeedURL: "https://example.com/other", Link: "https://example.com/a"},
		// Raw concatenation equals base ("...feed" + ".../a"); only the
		// separator byte keeps these distinct.
		"separator check": {FeedURL: "https://example.com/fe", Link: "edhttps://example.com/a"},
	}
	for name, other := range cases {
		if base.MsgID() == other.MsgID() {
			t.Errorf("%s: MsgID collision with base item", name)
		}
	}
	// Same content hashes the same (value receiver, no hidden state).
	dup := RSSItem{FeedURL: "https://example.com/feed", Link: "https://example.com/a"}
	if base.MsgID() != dup.MsgID() {
		t.Error("identical items produced different MsgIDs")
	}
}

func TestDeliveryMessage_RoundTrip(t *testing.T) {
	want := DeliveryMessage{
		TenantID:   "tenant-1",
		OutboxID:   "550e8400-e29b-41d4-a716-446655440000",
		WebhookURL: "https://discord.com/api/webhooks/x",
		FeedURL:    "https://example.com/feed",
		Title:      "Hello",
		Link:       "https://example.com/item-1",
	}
	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got DeliveryMessage
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round trip = %+v, want %+v", got, want)
	}
}

func TestAckReply_RoundTrip(t *testing.T) {
	for _, tc := range []AckReply{
		{TenantID: "default", OutboxID: "x1", Status: StatusDelivered},
		{TenantID: "t2", OutboxID: "x2", Status: StatusFailed, Error: "webhook 500"},
	} {
		raw, err := json.Marshal(tc)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		var got AckReply
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if !reflect.DeepEqual(got, tc) {
			t.Errorf("round trip = %+v, want %+v", got, tc)
		}
	}
}

func TestAckReply_OmitsEmptyError(t *testing.T) {
	raw, err := json.Marshal(AckReply{OutboxID: "x", Status: StatusDelivered})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if _, ok := decoded["error"]; ok {
		t.Errorf("expected no error key on success, got %s", raw)
	}
}
