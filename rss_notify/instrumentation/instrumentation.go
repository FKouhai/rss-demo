// Package instrument provides the init for the tracing
package instrumentation

import (
	"context"
	"fmt"
	"os"

	"sync"

	log "github.com/FKouhai/rss-notify/logger"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

var tp *sdktrace.TracerProvider

var tracer trace.Tracer
var once sync.Once

// InitTracer starts the otel tracer
func InitTracer(ctx context.Context) (*sdktrace.TracerProvider, error) {
	headers := map[string]string{
		"Content-Type": "application/json",
	}
	ep := os.Getenv("OTEL_EP")
	if ep == "" {
		return nil, fmt.Errorf("OTEL_EP environment variable not set")
	}
	log.InfoFmt("InitTracer() using OTEL_EP=%s", ep)
	exporter, err := otlptrace.New(
		ctx,
		otlptracegrpc.NewClient(
			otlptracegrpc.WithEndpoint(ep),
			otlptracegrpc.WithHeaders(headers),
			otlptracegrpc.WithInsecure(),
		),
	)
	if err != nil {
		return nil, err
	}
	resources, err := resource.New(
		ctx,
		resource.WithAttributes(
			attribute.String("service.name", "notifier"),
			attribute.String("library.language", "go"),
		),
	)
	if err != nil {
		return nil, err
	}

	once.Do(func() {
		tp = sdktrace.NewTracerProvider(
			sdktrace.WithSampler(sdktrace.AlwaysSample()),
			sdktrace.WithBatcher(exporter),
			sdktrace.WithResource(resources),
		)
		otel.SetTracerProvider(tp)
		otel.SetTextMapPropagator(
			propagation.NewCompositeTextMapPropagator(propagation.TraceContext{},
				propagation.Baggage{}),
		)

		tracer = tp.Tracer("notify")
	})

	return tp, nil
}

// GetTracer used to get the otel tracer being used
func GetTracer(tracerName string) trace.Tracer {
	return otel.Tracer(tracerName)
}

type spanAttrs struct {
	method   attribute.KeyValue
	httpCode attribute.KeyValue
}

// SetSpanAttributes function that returns a span with the minimum attributes
func SetSpanAttributes(span trace.Span, attributes spanAttrs) trace.Span {
	span.SetAttributes(attributes.httpCode, attributes.method)
	return span
}
