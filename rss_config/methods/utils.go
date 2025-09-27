package methods

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"time"

	log "github.com/FKouhai/rss-demo/libs/logger"
	"github.com/valkey-io/valkey-go"
)

// ConfigStruct contains the accepted config fields that this microservice will use
type ConfigStruct struct {
	RSSFeeds []string `json:"rss_feeds"`
}

func valkeyClient() valkey.Client {
	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{os.Getenv("VALKEY_HOST")},
	})
	if err != nil {
		return nil
	}
	return client
}

// GetFeeds Connects to valkey and retrieves the configured rss_feeds if they exist
func (c *ConfigStruct) GetFeeds(client valkey.Client) ([]byte, error) {
	get := client.B().JsonGet().Key("rss_feeds").Path("$").Build()
	result, err := client.Do(context.Background(), get).AsBytes()
	if err != nil {
		log.ErrorFmt("%v", err)
		return nil, err
	}
	return result, nil
}

// SetFeeds Connects to valkey and writes a json document for the feeds
func (c *ConfigStruct) SetFeeds(client valkey.Client) error {
	configJSON, err := json.Marshal(&c)
	if err != nil {
		log.ErrorFmt("%v", err)
		return err
	}
	set := client.B().JsonSet().Key("rss_feeds").Path("$").Value(string(configJSON)).Build()
	err = client.Do(context.Background(), set).Error()
	if err != nil {
		log.ErrorFmt("%v", err)
		return err
	}
	return nil
}

// PushConfig sends the needed payload to the poller config
func (c *ConfigStruct) PushConfig(data []byte) error {
	pollerURL := os.Getenv("POLLER_URL")
	req, err := http.NewRequest("POST", pollerURL, bytes.NewBuffer(data))
	if err != nil {
		log.ErrorFmt("%v", err)
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: time.Second * 10}
	resp, err := client.Do(req)
	if err != nil {
		log.ErrorFmt("%v", err)
		return err
	}
	defer resp.Body.Close()
	return nil
}
