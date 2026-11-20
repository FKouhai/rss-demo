// Package webhookpush contains all the needed logic for sending post request to the webhook destinations
package webhookpush

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/FKouhai/rss-demo/libs/instrumentation"
	log "github.com/FKouhai/rss-demo/libs/logger"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"
)

// DiscordNotification basic data structure that provides the Content and the webhook url for disrcord
type DiscordNotification struct {
	Content    []string `json:"feed_url"`
	WebHookURL string   `json:"webhook_url"`
}

// tracer is the service-scoped helper for notify spans.
var tracer = instrumentation.ForService("notify")

// DiscordMessage is the final message that will get sent to the destination
type DiscordMessage struct {
	Content string `json:"content"`
}

// SendNotification sends a post request to the discord webhook endpoint,
// returning the HTTP status code and error (or nil on success).
func (d *DiscordNotification) SendNotification(ctx context.Context, message []string) (int, error) {
	_, span := tracer.Start(ctx, "webhookPush.SendNotification")
	defer span.End()
	span.AddEvent("SENDING_WEBHOOK")
	span.SetAttributes(attribute.Int("messages.count", len(message)))

	log.DebugCtx(ctx, "[TRACE] SendNotification: incoming trace context",
		zap.String("webhook_url", d.WebHookURL))

	m, err := d.toDiscordMessage(ctx, message)
	if err != nil {
		span.RecordError(err)
		return 0, err
	}
	r := bytes.NewBuffer(m)
	req, err := http.NewRequestWithContext(ctx, "POST", d.WebHookURL, r)
	if err != nil {
		span.RecordError(err)
		log.InfoCtx(ctx, "Failed to make request")
		return 0, err
	}
	req.Header.Add("Content-Type", "application/json")

	log.DebugCtx(ctx, "[TRACE] SendNotification: request traceparent header",
		zap.String("traceparent", req.Header.Get("traceparent")))

	client := &http.Client{
		Timeout:   10 * time.Second,
		Transport: otelhttp.NewTransport(http.DefaultTransport),
	}
	res, err := client.Do(req)
	if err != nil {
		span.RecordError(err)
		log.InfoCtx(ctx, "Request was unsuccesful")
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

	log.DebugCtx(ctx, "[TRACE] SendNotification: webhook response",
		zap.Int("status_code", res.StatusCode))
	log.InfoFmtCtx(ctx, "webhook response: %v", string(body))
	return res.StatusCode, nil
}

func (d *DiscordNotification) toDiscordMessage(ctx context.Context, message []string) ([]byte, error) {
	_, span := tracer.Start(ctx, "webhookPush.toDiscordMessage")
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
