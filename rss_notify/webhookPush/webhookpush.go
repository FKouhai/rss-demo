// Package webhookpush contains all the needed logic for sending post request to the webhook destinations
package webhookpush

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/FKouhai/rss-demo/libs/instrumentation"
	log "github.com/FKouhai/rss-demo/libs/logger"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// PushMessage interface contains the function contracts that are required to be compliant with while sending a notification
type PushMessage interface {
	// GetContent returns a string that is bound to the specific destination(s) and error if the content parsing fails if it doesnt return nil
	GetContent(ctx context.Context, content []byte) ([]string, error)
	// SendNotification returns the http.StatusCode and error or nil
	SendNotification(ctx context.Context, message []string) (int, error)
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
func (d *DiscordNotification) GetContent(ctx context.Context, content []byte) ([]string, error) {
	_, span := instrumentation.GetTracer("notify").Start(ctx, "webhookPush.GetContent", trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()
	span.AddEvent("UNMARSHALING_JSON")
	span.SetAttributes(attribute.Int("payload.size", len(content)))
	err := json.Unmarshal(content, &d)
	if err != nil {
		span.RecordError(err)
		log.Info("got error unmarshaling JSON", zap.String("trace_id", span.SpanContext().TraceID().String()))
		log.ErrorFmt("JSON unmarshal error: %v", err.Error())
		log.ErrorFmt("Received payload: %s", string(content))
		return nil, err
	}
	span.SetAttributes(attribute.Int("messages.count", len(d.Content)))
	return d.Content, nil
}

// SendNotification method compliant with the PushMessage interface that sends a post request to the discord webhook endpoint
func (d *DiscordNotification) SendNotification(ctx context.Context, message []string) (int, error) {
	_, span := instrumentation.GetTracer("notify").Start(ctx, "webhookPush.SendNotification", trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()
	span.AddEvent("SENDING_WEBHOOK")
	span.SetAttributes(attribute.Int("messages.count", len(message)))
	m, err := d.toDiscordMessage(ctx, message)
	if err != nil {
		span.RecordError(err)
		return 0, err
	}
	r := bytes.NewBuffer(m)
	req, err := http.NewRequest("POST", d.WebHookURL, r)
	if err != nil {
		span.RecordError(err)
		log.Info("Failed to make request")
		return 0, err
	}
	req.Header.Add("Content-Type", "application/json")
	client := &http.Client{}
	res, err := client.Do(req)
	if err != nil {
		span.RecordError(err)
		log.Info("Request was unsuccesful")
		return http.StatusInternalServerError, err
	}
	// nolint
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		span.RecordError(err)
		return http.StatusInternalServerError, err
	}
	span.SetAttributes(attribute.Int("webhook.status", res.StatusCode), attribute.Int("response.size", len(body)))
	log.InfoFmt("webhook response: %v", string(body)) // TODO: add trace_id
	return res.StatusCode, nil
}

func (d *DiscordNotification) toDiscordMessage(ctx context.Context, message []string) ([]byte, error) {
	_, span := instrumentation.GetTracer("notify").Start(ctx, "webhookPush.toDiscordMessage", trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()
	span.AddEvent("MARSHALING_MESSAGE")
	if len(message) == 0 {
		err := fmt.Errorf("no messages to send")
		span.RecordError(err)
		return nil, err
	}
	dm := DiscordMessage{
		Content: message[0],
	}

	log.InfoFmt("payload: %v", dm.Content) // TODO: add trace_id
	b, err := json.Marshal(&dm)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	span.SetAttributes(attribute.Int("message.size", len(b)))
	return b, nil

}
