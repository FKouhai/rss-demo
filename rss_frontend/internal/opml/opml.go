package opml

import (
	"encoding/xml"
	"fmt"
	"io"
)

// DefaultMaxFeeds prevents from overwhelming the poller
const DefaultMaxFeeds = 500

// Feed importable subscriptions
type Feed struct {
	URL   string `json:"url"`
	Title string `json:"title,omitempty"`
}

// OPML follows the standard for feed xml import files
type OPML struct {
	XMLName xml.Name `xml:"opml"`
	Version string   `xml:"version,attr"`
	Head    head
	Body    body
}

type head struct {
	XMLName xml.Name `xml:"head"`
	Title   string   `xml:"title"`
}

type body struct {
	XMLName  xml.Name  `xml:"body"`
	Outlines []outline `xml:"outline"`
}

type outline struct {
	Text     string    `xml:"text,attr"`
	Title    string    `xml:"title,attr"`
	Type     string    `xml:"type,attr"`
	XMLURL   string    `xml:"xmlUrl,attr"`
	HTMLURL  string    `xml:"htmlUrl,attr"`
	Outlines []outline `xml:"outline,omitempty"`
}

// Parse converts the OPML streamed file contents to Feed
func Parse(r io.Reader, maxFeeds int) ([]Feed, error) {
	var doc OPML
	if err := xml.NewDecoder(r).Decode(&doc); err != nil {
		return nil, fmt.Errorf("xml decode: %w", err)
	}

	if maxFeeds <= 0 {
		maxFeeds = DefaultMaxFeeds
	}

	// preallocate max feeds
	feeds := make([]Feed, 0, maxFeeds)

	err := walk(doc.Body.Outlines, &feeds, maxFeeds)
	if err != nil {
		return nil, err
	}
	return feeds, nil
}

func walk(outlines []outline, feeds *[]Feed, max int) error {
	for _, f := range outlines {
		if f.XMLURL != "" {
			if len(*feeds) >= max {
				return fmt.Errorf("opml: feed limit %d exceeded", max)
			}

			title := f.Title

			if title == "" {
				title = f.Text
			}

			*feeds = append(*feeds, Feed{Title: title, URL: f.XMLURL})
		}

		if err := walk(f.Outlines, feeds, max); err != nil {
			return err
		}
	}
	return nil
}
