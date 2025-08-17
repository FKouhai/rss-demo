// Package webhookpush contains all the needed logic for sending post request to the webhook destinations
package webhookpush

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	log "github.com/FKouhai/rss-notify/logger"
)

// PushMessage interface contains the function contracts that are required to be compliant with while sending a notification
type PushMessage interface {
	// GetContent returns a string that is bound to the specific destination(s) and error if the content parsing fails if it doesnt return nil
	GetContent(content []byte) ([]string, error)
	// SendNotification returns the http.StatusCode and error or nil
	SendNotification(message []string) (int, error)
}

// DiscordNotification basic data structure that provides the Content and the webhook url for disrcord
type DiscordNotification struct {
	Content    []string `json:"feed_url"`
	WebHookURL string   `json:"webhook_url"`
}

// DiscordMessage is the final message that will get sent to the destination
type DiscordMessage struct {
	Content string `json:"content"`
}

// GetContent is the helper function that receives a feed as an input and returns the Data structure
func (d *DiscordNotification) GetContent(content []byte) ([]string, error) {
	err := json.Unmarshal(content, &d)
	if err != nil {
		log.ErrorFmt("GetContent(): error got %w", err)
		return nil, err
	}
	return d.Content, nil
}

// SendNotification method compliant with the PushMessage interface that sends a post request to the discord webhook endpoint
func (d *DiscordNotification) SendNotification(message []string) (int, error) {
	m, err := d.toDiscordMessage(message)
	if err != nil {
		return 0, err
	}
	r := bytes.NewBuffer(m)
	req, err := http.NewRequest("POST", d.WebHookURL, r)
	if err != nil {
		log.ErrorFmt("SendNotification(): Failed to make request: %w", err)
		return req.Response.StatusCode, err
	}
	req.Header.Add("Content-Type", "application/json")
	client := &http.Client{}
	res, err := client.Do(req)
	if err != nil {
		log.ErrorFmt("SendNotification(): Unsuccessful request %w", err)
		return http.StatusInternalServerError, err
	}
	defer req.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		log.ErrorFmt("SendNotification(): Error parsing request body %w", err)
		return http.StatusInternalServerError, err
	}
	defer res.Body.Close()
	log.InfoFmt("SendNotification(): Response %s", string(body))
	return res.StatusCode, nil
}

func (d *DiscordNotification) toDiscordMessage(message []string) ([]byte, error) {
	dm := DiscordMessage{
		Content: message[0],
	}

	log.InfoFmt("DiscordMessage: payload %s", dm.Content)
	b, err := json.Marshal(&dm)
	if err != nil {
		log.ErrorFmt("toDiscordMessage(): Error parsing payload %w", err)
		return nil, err
	}
	return b, nil

}
