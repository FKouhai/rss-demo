package pubsub

import (
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

func TestFeedsStreamConfig_Baseline(t *testing.T) {
	cfg := FeedsStreamConfig()
	if cfg.Name != StreamFeeds {
		t.Errorf("Name = %q, want %q", cfg.Name, StreamFeeds)
	}
	wantSubjects := map[string]bool{SubjectFeedsNew: true, SubjectFeedsEnriched: true}
	if len(cfg.Subjects) != len(wantSubjects) {
		t.Fatalf("Subjects = %v, want %v", cfg.Subjects, wantSubjects)
	}
	for _, s := range cfg.Subjects {
		if !wantSubjects[s] {
			t.Errorf("unexpected subject %q", s)
		}
	}
	if cfg.Retention != jetstream.LimitsPolicy {
		t.Errorf("Retention = %v, want LimitsPolicy", cfg.Retention)
	}
	if cfg.Discard != jetstream.DiscardOld {
		t.Errorf("Discard = %v, want DiscardOld", cfg.Discard)
	}
	if cfg.MaxAge != 24*time.Hour {
		t.Errorf("MaxAge = %v, want 24h", cfg.MaxAge)
	}
	if cfg.MaxBytes != 1<<30 {
		t.Errorf("MaxBytes = %v, want 1GiB", cfg.MaxBytes)
	}
	if cfg.Storage != jetstream.FileStorage {
		t.Errorf("Storage = %v, want FileStorage", cfg.Storage)
	}
}

func TestConsumerFor_PassthroughAndDefaults(t *testing.T) {
	cfg := ConsumerFor("outbox-feeds", SubjectFeedsNew)
	if cfg.Durable != "outbox-feeds" {
		t.Errorf("Durable = %q, want outbox-feeds", cfg.Durable)
	}
	if cfg.FilterSubject != SubjectFeedsNew {
		t.Errorf("FilterSubject = %q, want %q", cfg.FilterSubject, SubjectFeedsNew)
	}
	if cfg.AckPolicy != jetstream.AckExplicitPolicy {
		t.Errorf("AckPolicy = %v, want AckExplicitPolicy", cfg.AckPolicy)
	}
	if cfg.MaxDeliver != 5 {
		t.Errorf("MaxDeliver = %d, want 5", cfg.MaxDeliver)
	}
	if cfg.AckWait != 30*time.Second {
		t.Errorf("AckWait = %v, want 30s", cfg.AckWait)
	}
}
