package instrumentation

import (
	"context"
	"os"
	"testing"
)

func TestGetTracer(t *testing.T) {
	tracer := GetTracer("test-tracer")
	if tracer == nil {
		t.Error("Expected a valid Tracer object, got nil.")
	}
}

func TestInitNoError(t *testing.T) {
	if err := os.Setenv("OTEL_EP", "testingotel12:443"); err != nil {
		t.Errorf("Expected OTEL_EP environment variable set")
	}
	_, err := InitTracer(context.Background())
	if err != nil {
		t.Errorf("Expected no error but got %v", err)
	}
}
