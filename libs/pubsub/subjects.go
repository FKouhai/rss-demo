package pubsub

const (
	// Feeds prefix
	SubjectPrefixFeeds = "feeds."
	// Notifications prefix
	SubjectPrefixNotifications = "notifications."
	// ACKs prefix
	SubjectPrefixAcks = "acks."
	// SubjectFeedsNew default primary subject for poller and outbox
	SubjectFeedsNew = SubjectPrefixFeeds + "new"
	// SubjectNotificationRequest default subject for outbox and notify
	SubjectNotificationRequest = SubjectPrefixNotifications + "request"
	// SubjectAcksDelivered default subject for notify and outbox
	SubjectAcksDelivered = SubjectPrefixAcks + "delivered"
	// SubjectFeedsEnriched default subject for an enriched subject
	SubjectFeedsEnriched = SubjectPrefixFeeds + "enriched"
	// SubjectFeedsBackfill default subject for backfill feeds
	SubjectFeedsBackfill = SubjectPrefixFeeds + "backfill"
)
