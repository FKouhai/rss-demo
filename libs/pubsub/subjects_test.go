package pubsub

import "testing"

func TestSubjectPrefixes(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"feeds prefix", SubjectPrefixFeeds, "feeds."},
		{"notifications prefix", SubjectPrefixNotifications, "notifications."},
		{"acks prefix", SubjectPrefixAcks, "acks."},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("got %q, want %q", tc.got, tc.want)
			}
		})
	}
}

func TestSubjectComposition(t *testing.T) {
	// Guard against the "acks.acks" typo we hit once already.
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"feeds.new", SubjectFeedsNew, "feeds.new"},
		{"notifications.request", SubjectNotificationRequest, "notifications.request"},
		{"acks.delivered", SubjectAcksDelivered, "acks.delivered"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("got %q, want %q", tc.got, tc.want)
			}
		})
	}
}

func TestSubjectsDeriveFromPrefixes(t *testing.T) {
	// The composed subjects must actually start with their prefix —
	// this catches the case where someone edits a prefix but forgets to
	// update the composed constant.
	if SubjectFeedsNew[:len(SubjectPrefixFeeds)] != SubjectPrefixFeeds {
		t.Errorf("SubjectFeedsNew does not start with SubjectPrefixFeeds")
	}
	if SubjectNotificationRequest[:len(SubjectPrefixNotifications)] != SubjectPrefixNotifications {
		t.Errorf("SubjectNotificationRequest does not start with SubjectPrefixNotifications")
	}
	if SubjectAcksDelivered[:len(SubjectPrefixAcks)] != SubjectPrefixAcks {
		t.Errorf("SubjectAcksDelivered does not start with SubjectPrefixAcks")
	}
}
