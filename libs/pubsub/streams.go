package pubsub

import (
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// StreamFeeds name for stream config
const StreamFeeds = "FEEDS"

// FeedsStreamConfig canonical baseline for items in one stream
func FeedsStreamConfig() jetstream.StreamConfig {
	return jetstream.StreamConfig{
		Name:        StreamFeeds,
		Description: "Raw + enriched feed items",
		Subjects:    []string{SubjectFeedsNew, SubjectFeedsEnriched},
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
