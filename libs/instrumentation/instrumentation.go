// Package instrument provides the init for the tracing
package instrumentation

import (
	"context"
	"os"

	"sync"

	log "github.com/FKouhai/rss-demo/libs/logger"
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

// nolint
// var tracer trace.Tracer
var once sync.Once

// InitTracer starts the otel tracer
func InitTracer(tracerName string) (*sdktrace.TracerProvider, error) {
	headers := map[string]string{
		"content-type": "application/json",
	}
	ep := os.Getenv("OTEL_EP")
	if ep == "" {
		log.Info("OTEL_EP not set, using default localhost:4317")
		ep = "localhost:4317"
	}
	log.Info("using OTEL_EP=" + ep)
	exporter, err := otlptrace.New(
		context.Background(),
		otlptracegrpc.NewClient(
			otlptracegrpc.WithEndpoint(ep),
			otlptracegrpc.WithHeaders(headers),
			otlptracegrpc.WithInsecure(),
		),
	)
	if err != nil {
		log.ErrorFmt("Failed to create OTLP trace exporter: %v", err)
		return nil, err
	}
	resources, err := resource.New(
		context.Background(),
		resource.WithAttributes(
			attribute.String("service.name", tracerName),
			attribute.String("library.language", "go"),
		),
	)
	if err != nil {
		log.ErrorFmt("Failed to create resource: %v", err)
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

		// tracer = tp.Tracer(tracerName)
	})

	return tp, nil
}

// GetTracer used to get the otel tracer being used
func GetTracer(tracerName string) trace.Tracer {
	// Return a tracer from the tracer provider with the proper resource attributes
	// If the tracer provider hasn't been initialized yet, fall back to creating a new one
	if tp != nil {
		return tp.Tracer(tracerName)
	}
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
