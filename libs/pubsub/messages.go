// Package pubsub messages
// contains the shape for:
// RSS Items -> common contract of what a producer would publish and how a consumer would ingest it
// Delivery messages ->
// ACK replies
package pubsub

import (
	"crypto/sha256"
	"encoding/hex"
)

// Status enum for ACK reply statuses
type Status string

const (
	// StatusDelivered message has been delivered
	StatusDelivered Status = "delivered"

	// StatusFailed message failed to be delivered
	StatusFailed Status = "failed"
)

// RSSItem represents the shape of an rss entry
type RSSItem struct {
	FeedURL     string `json:"feed_url"`
	Title       string `json:"title"`
	Link        string `json:"link"`
	PublishedAt string `json:"published_at"`
	WebhookURL  string `json:"webhook_url"`
}

// DeliveryMessage represents the message
// that is published and read from NATS
type DeliveryMessage struct {
	TenantID   string `json:"tenant_id"`
	OutboxID   string `json:"outbox_id"`
	WebhookURL string `json:"webhook_url"`
	FeedURL    string `json:"feed_url"`
	Title      string `json:"title"`
	Link       string `json:"link"`
}

// AckReply fields for ACK replies on services
type AckReply struct {
	TenantID string `json:"tenant_id"`
	OutboxID string `json:"outbox_id"`
	Status   Status `json:"status"` // "delivered" | "failed"
	Error    string `json:"error,omitempty"`
}

// MsgID produces a message ID for NATS dedup key
// uses the Feed URL and the link for hashing
func (r *RSSItem) MsgID() string {
	sum := sha256.Sum256([]byte(r.FeedURL + "\x00" + r.Link))
	return hex.EncodeToString(sum[:])
}
