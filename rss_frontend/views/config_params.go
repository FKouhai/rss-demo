package views

// ConfigFeed is one managed feed subscription for the Configure page.
type ConfigFeed struct {
	URL        string
	WebhookURL string
}

// ConfigData is the decoded rss_config snapshot the handlers pass in.
type ConfigData struct {
	Feeds          []ConfigFeed
	DefaultWebhook string
}

// ConfigParams carries the configure page.
type ConfigParams struct {
	Title       string
	User        string
	AuthEnabled bool
	Feeds       []string
	AggOK       bool
	// ConfigOK means the list below comes from rss_config (managed);
	// otherwise Feeds is the aggregator fallback (read-only).
	ConfigOK       bool
	IsAdmin        bool
	ConfigFeeds    []ConfigFeed
	DefaultWebhook string
}
