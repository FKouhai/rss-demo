// Package config is the shared client for rss_config: one client every
// service uses to pull its config, so no service reimplements fetch,
// auth headers, or snapshot decoding.
//
// Services pull; the config service never pushes. A service fetches
// GET /config on its own interval, keeps the last-good snapshot, and
// falls back to its local file when config is unreachable.
package config

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// FeedSource is one feed subscription with an optional per-feed webhook
// override. When empty, the global WebhookURL applies.
type FeedSource struct {
	URL        string `json:"url"`
	WebhookURL string `json:"webhook_url,omitempty"`
}

// Snapshot is the poller-facing view served by rss_config:
// legacy URL list + global default + per-feed overrides.
type Snapshot struct {
	RSSFeeds   []string     `json:"rss_feeds"`
	WebhookURL string       `json:"webhook_url"`
	Feeds      []FeedSource `json:"feeds"`
}

// Client pulls config from rss_config over HTTP.
type Client struct {
	baseURL       string
	gatewaySecret string
	tenantID      string
	http          *http.Client
}

// Option customizes a Client.
type Option func(*Client)

// WithTenantID sets the X-Tenant-ID header (default "default").
// The header-trust middleware requires a tenant on every request.
func WithTenantID(id string) Option {
	return func(c *Client) { c.tenantID = id }
}

// WithHTTPClient swaps the HTTP client (tests inject httptest clients).
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.http = h }
}

// NewClient builds a Client for the rss_config at baseURL
// (e.g. "http://rss_config:3000"). An empty gatewaySecret talks to an
// open-mode (local dev) config; otherwise it is sent as X-Gateway-Secret.
func NewClient(baseURL, gatewaySecret string, opts ...Option) *Client {
	c := &Client{
		baseURL:       baseURL,
		gatewaySecret: gatewaySecret,
		tenantID:      "default",
		http:          &http.Client{Timeout: 10 * time.Second},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Snapshot fetches GET /config. Callers keep their last-good snapshot
// on error and fall back to local files when config is unreachable.
func (c *Client) Snapshot(ctx context.Context) (Snapshot, error) {
	var snap Snapshot
	if c.baseURL == "" {
		return snap, fmt.Errorf("config: empty base URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/config", nil)
	if err != nil {
		return snap, err
	}
	if c.gatewaySecret != "" {
		req.Header.Set("X-Gateway-Secret", c.gatewaySecret)
	}
	req.Header.Set("X-Tenant-ID", c.tenantID)
	resp, err := c.http.Do(req)
	if err != nil {
		return snap, fmt.Errorf("config: GET /config: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return snap, fmt.Errorf("config: GET /config: status %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&snap); err != nil {
		return snap, fmt.Errorf("config: decode snapshot: %w", err)
	}
	return snap, nil
}
