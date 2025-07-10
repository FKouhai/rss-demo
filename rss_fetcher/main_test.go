package main

import (
	"testing"
)

// Testing ParseRSS expecting a transaction that should succeed
func TestRssGetterOk(t *testing.T) {
	rss := "https://www.reddit.com/r/news+wtf.rss"
	feed, err := ParseRSS(rss)

	if err != nil {
		t.FailNow()
	}

	items := len(feed.Items)
	if items < 0 {
		t.FailNow()
	}

}

// Testing the function behavior on failure
func TestRssGetterNotOk(t *testing.T) {
	rss := "https://www.nonexitantrss.com/r/news+wtf.rss"
	feed, err := ParseRSS(rss)

	if err == nil {
		t.FailNow()
	}

	if feed != nil {
		t.FailNow()
	}

}
