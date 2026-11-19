// Package instrumentation helpers: service-scoped tracers that remove the
// per-call GetTracer + SpanKind boilerplate, plus shared span helpers.
//
// Typical use in a service package:
//
//	var tracer = instrumentation.ForService("poller")
//
//	ctx, span := tracer.StartServer(r.Context(), "handlers.RSSHandler")
//	defer span.End()
package instrumentation

import (
	"context"
	"fmt"

	log "github.com/FKouhai/rss-demo/libs/logger"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Tracer is a service-scoped wrapper around an OTEL tracer. It fixes the
// service name once so call sites only name the operation and pick a kind.
// The zero value is invalid; build one with ForService (production) or
// ForTracer (tests / custom providers).
type Tracer struct {
	inner trace.Tracer
}

// ForService returns a Tracer bound to the service's provider. The name
// must match the one passed to InitTracer so spans land in the right
// resource (service.name).
func ForService(name string) Tracer {
	return Tracer{inner: GetTracer(name)}
}

// ForTracer wraps an arbitrary tracer, e.g. a noop provider in tests.
func ForTracer(t trace.Tracer) Tracer {
	return Tracer{inner: t}
}

// Start begins an internal span for in-process work.
func (t Tracer) Start(ctx context.Context, op string) (context.Context, trace.Span) {
	return t.inner.Start(ctx, op, trace.WithSpanKind(trace.SpanKindInternal))
}

// StartServer begins a server span for an inbound request.
func (t Tracer) StartServer(ctx context.Context, op string) (context.Context, trace.Span) {
	return t.inner.Start(ctx, op, trace.WithSpanKind(trace.SpanKindServer))
}

// StartClient begins a client span for an outbound call (feed fetch,
// webhook POST, locator query).
func (t Tracer) StartClient(ctx context.Context, op string) (context.Context, trace.Span) {
	return t.inner.Start(ctx, op, trace.WithSpanKind(trace.SpanKindClient))
}

// StartConsumer begins a consumer span for a received message.
func (t Tracer) StartConsumer(ctx context.Context, op string) (context.Context, trace.Span) {
	return t.inner.Start(ctx, op, trace.WithSpanKind(trace.SpanKindConsumer))
}

// StartProducer begins a producer span for a published message.
func (t Tracer) StartProducer(ctx context.Context, op string) (context.Context, trace.Span) {
	return t.inner.Start(ctx, op, trace.WithSpanKind(trace.SpanKindProducer))
}

// Fail records err on the span (with stack trace), marks it failed, and
// logs msg with the span's trace fields so the line joins to the trace
// in Loki. The message is formatted; pass a "%s" wrapper if msg itself
// may contain '%' verbs.
func Fail(span trace.Span, err error, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	log.Error(msg, log.SpanFields(span)...)
	span.RecordError(err, trace.WithStackTrace(true))
	span.SetStatus(codes.Error, msg)
}

// HTTP annotates span with HTTP method and status attributes.
func HTTP(span trace.Span, method string, status int) {
	span.SetAttributes(
		attribute.String("http.method", method),
		attribute.Int("http.status", status),
	)
}
