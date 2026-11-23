package pubsub

import (
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// StreamType enum for stream types
type StreamType string

// StreamFeeds name for stream config
const (
	StreamFeeds         StreamType = "FEEDS"
	StreamNotifications StreamType = "NOTIFICATIONS"
	StreamAcks          StreamType = "ACKS"
)

// FeedsStreamConfig canonical baseline for a stream producer
func FeedsStreamConfig() jetstream.StreamConfig {
	return jetstream.StreamConfig{
		Name:        string(StreamFeeds),
		Description: "Raw + enriched feed items",
		Subjects:    []string{SubjectFeedsNew, SubjectFeedsEnriched, SubjectFeedsBackfill},
		Retention:   jetstream.LimitsPolicy,
		Discard:     jetstream.DiscardOld,
		MaxAge:      24 * time.Hour,
		MaxBytes:    1 << 30, // 1GB
		Storage:     jetstream.FileStorage,
	}
}

// ConsumerFor returns the canonical durable consumer for a service.
// The durable name is the caller's choice (per-service position on the
// stream); the lib stays ignorant of which services exist.
// Note: AckWait must exceed the slowest handler plus any ConsumeOptions
// NakDelay, or redeliveries race the ack deadline.
func ConsumerFor(durable, filterSubject string) jetstream.ConsumerConfig {
	return jetstream.ConsumerConfig{
		Durable:       durable,
		FilterSubject: filterSubject,
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       30 * time.Second,
		MaxDeliver:    5,
	}
}

// NotificationsStreamConfig canonical baseline for a notifications producer
func NotificationsStreamConfig() jetstream.StreamConfig {
	return jetstream.StreamConfig{
		Name:        string(StreamNotifications),
		Description: "Notifications streams",
		Subjects:    []string{SubjectNotificationRequest},
		Retention:   jetstream.LimitsPolicy,
		Discard:     jetstream.DiscardOld,
		MaxAge:      1 * time.Hour,
		MaxBytes:    256 << 20, // 256 MB
		Storage:     jetstream.FileStorage,
	}
}

// ACKStreamConfig canonical baseline for an ACK producer
func ACKStreamConfig() jetstream.StreamConfig {
	return jetstream.StreamConfig{
		Name:        string(StreamAcks),
		Description: "ACKs stream",
		Subjects:    []string{SubjectAcksDelivered},
		Retention:   jetstream.LimitsPolicy,
		Discard:     jetstream.DiscardOld,
		MaxAge:      1 * time.Hour,
		MaxBytes:    64 << 20, // 64 MB
		Storage:     jetstream.FileStorage,
	}
}
