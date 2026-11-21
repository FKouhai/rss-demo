package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/FKouhai/rss-frontend/views"
)

// ItemStore serves feed items to the pages. The production implementation
// is the aggregator HTTP client below; tests use a fake. Handlers never
// touch storage directly, so the aggregator service is a drop-in.
type ItemStore interface {
	// List returns newest-first items plus the total match count.
	List(ctx context.Context, q, feed string, limit, offset int) ([]views.Item, int, error)
	// Feeds returns distinct feed URLs, newest activity first.
	Feeds(ctx context.Context) ([]string, error)
	// Get returns one item by id, or ErrNotFound.
	Get(ctx context.Context, id string) (views.Item, error)
}

// ErrNotFound marks a missing item (rendered as 404; anything else from
// the aggregator is a 503 with the unavailable notice).
var ErrNotFound = errors.New("not found")

// wireItem mirrors the aggregator's item JSON.
type wireItem struct {
	ID          string `json:"id"`
	FeedURL     string `json:"feed_url"`
	Title       string `json:"title"`
	Link        string `json:"link"`
	PublishedAt string `json:"published_at"`
}

func toItem(w wireItem) views.Item {
	return views.Item{ID: w.ID, FeedURL: w.FeedURL, Title: w.Title, Link: w.Link, PublishedAt: w.PublishedAt}
}

// aggClient implements ItemStore over the aggregator HTTP API:
//
//	GET {base}/api/items?q=&feed=&limit=&offset= -> {"items":[...],"total":N}
//	GET {base}/api/feeds                         -> {"feeds":[...]}
//	GET {base}/api/items/{id}                    -> item (404 when missing)
//
// AggClient builds the production ItemStore: the aggregator HTTP API.
func AggClient(baseURL string, httpClient *http.Client) ItemStore {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return aggClient{base: baseURL, http: httpClient}
}

type aggClient struct {
	base string
	http *http.Client
}

func (a aggClient) get(ctx context.Context, path string, query url.Values, out any) (int, error) {
	u := a.base + path
	if query != nil {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := a.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, fmt.Errorf("aggregator %s: %s", path, resp.Status)
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return resp.StatusCode, err
		}
	}
	return resp.StatusCode, nil
}

func (a aggClient) List(ctx context.Context, q, feed string, limit, offset int) ([]views.Item, int, error) {
	qs := url.Values{}
	qs.Set("q", q)
	qs.Set("feed", feed)
	qs.Set("limit", strconv.Itoa(limit))
	qs.Set("offset", strconv.Itoa(offset))
	var body struct {
		Items []wireItem `json:"items"`
		Total int        `json:"total"`
	}
	if _, err := a.get(ctx, "/api/items", qs, &body); err != nil {
		return nil, 0, err
	}
	items := make([]views.Item, 0, len(body.Items))
	for _, w := range body.Items {
		items = append(items, toItem(w))
	}
	return items, body.Total, nil
}

func (a aggClient) Feeds(ctx context.Context) ([]string, error) {
	var body struct {
		Feeds []string `json:"feeds"`
	}
	if _, err := a.get(ctx, "/api/feeds", nil, &body); err != nil {
		return nil, err
	}
	return body.Feeds, nil
}

func (a aggClient) Get(ctx context.Context, id string) (views.Item, error) {
	var w wireItem
	code, err := a.get(ctx, "/api/items/"+url.PathEscape(id), nil, &w)
	if err != nil {
		if code == http.StatusNotFound {
			return views.Item{}, ErrNotFound
		}
		return views.Item{}, err
	}
	return toItem(w), nil
}
