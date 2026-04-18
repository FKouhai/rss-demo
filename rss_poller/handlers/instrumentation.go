package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/FKouhai/rss-demo/libs/instrumentation"
	log "github.com/FKouhai/rss-demo/libs/logger"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// startSpan opens a new span with the given name and kind.
// The returned context must be used for child operations to maintain the trace hierarchy.
// The caller must defer span.End().
func startSpan(ctx context.Context, name string, kind trace.SpanKind) (context.Context, trace.Span) {
	return instrumentation.GetTracer("poller").Start(ctx, name, trace.WithSpanKind(kind))
}

// spanErrorf records err on the span, logs at error level, and marks the span as failed.
func spanErrorf(span trace.Span, err error, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	log.Error(msg)
	span.RecordError(err, trace.WithStackTrace(true))
	span.SetStatus(codes.Error, msg)
}

// recordHTTPSpan annotates span with HTTP method and status code attributes.
func recordHTTPSpan(span trace.Span, method string, status int) {
	span.SetAttributes(
		attribute.String("http.method", method),
		attribute.Int("http.status", status),
	)
}

// getConfigSnapshot returns a copy of the current config under the read lock.
func getConfigSnapshot() ConfigStruct {
	cfgMu.RLock()
	defer cfgMu.RUnlock()
	return cfg
}

// writeJSON writes status and JSON-encodes body to w, logging any encoding error.
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.ErrorFmt("failed to encode response: %v", err)
	}
}
