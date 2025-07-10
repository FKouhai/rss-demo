package main

import (
	"fmt"
	log "github.com/FKouhai/rss-microservices/logger"

	"github.com/mmcdole/gofeed"
)

func main() {
	rss := "https://www.reddit.com/r/news/.rss"
	feeds, err := ParseRSS(rss)
	if err != nil {
		log.Debug(err.Error())
	}
	for _, v := range feeds.Items {
		fmt.Println(v.Title, v.Image, v.Description, v.Content)
	}
}

// ParseRSS returns the rss feed with all its items
func ParseRSS(feedURL string) (*gofeed.Feed, error) {
	feedParser := gofeed.NewParser()
	feed, err := feedParser.ParseURL(feedURL)
	if err != nil {
		log.Debug(err.Error())
		return nil, err
	}
	log.Info("got feed")
	return feed, nil
}
