package opml

import (
	"strings"
	"testing"
)

const nestedOPML = `<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0">
  <head><title>Subscriptions</title></head>
  <body>
    <outline text="Tech">
      <outline text="Example News" title="Example" type="rss" xmlUrl="https://example.com/feed.rss" htmlUrl="https://example.com/"/>
      <outline text="Text Only Feed" type="rss" xmlUrl="https://example.org/rss.xml"/>
    </outline>
    <outline text="Flat Feed" title="Flat" type="rss" xmlUrl="https://flat.example/feed"/>
    <outline text="Empty Folder">
      <outline text="Nested" title="Deep" type="rss" xmlUrl="https://deep.example/feed.xml"/>
    </outline>
    <outline text="No URL Here"/>
  </body>
</opml>`

func TestParse_Nested(t *testing.T) {
	feeds, err := Parse(strings.NewReader(nestedOPML), 0)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(feeds) != 4 {
		t.Fatalf("got %d feeds, want 4: %+v", len(feeds), feeds)
	}
	// Folder outlines (no xmlUrl) contribute nothing.
	for _, f := range feeds {
		if f.URL == "" {
			t.Errorf("empty URL in %+v", f)
		}
	}
}

func TestParse_TitleFallback(t *testing.T) {
	feeds, err := Parse(strings.NewReader(nestedOPML), 0)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byURL := map[string]string{}
	for _, f := range feeds {
		byURL[f.URL] = f.Title
	}
	if byURL["https://example.com/feed.rss"] != "Example" {
		t.Errorf("title = %q, want Example (title attr wins)", byURL["https://example.com/feed.rss"])
	}
	if byURL["https://example.org/rss.xml"] != "Text Only Feed" {
		t.Errorf("title = %q, want text fallback", byURL["https://example.org/rss.xml"])
	}
	if byURL["https://deep.example/feed.xml"] != "Deep" {
		t.Errorf("nested title = %q, want Deep", byURL["https://deep.example/feed.xml"])
	}
}

func TestParse_CapRejects(t *testing.T) {
	if _, err := Parse(strings.NewReader(nestedOPML), 2); err == nil {
		t.Error("expected limit error for 4 feeds with max 2, got nil")
	}
}

func TestParse_Garbage(t *testing.T) {
	for _, raw := range []string{"", "not xml at all <", "<opml><body>"} {
		if _, err := Parse(strings.NewReader(raw), 0); err == nil {
			t.Errorf("input %q: expected decode error, got nil", raw)
		}
	}
}

func TestParse_Empty(t *testing.T) {
	feeds, err := Parse(strings.NewReader(`<?xml version="1.0"?><opml version="2.0"><head/><body/></opml>`), 0)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(feeds) != 0 {
		t.Errorf("got %d feeds, want 0", len(feeds))
	}
}
