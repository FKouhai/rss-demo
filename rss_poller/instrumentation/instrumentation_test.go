package instrumentation

import (
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
	os.Setenv("OTEL_EP", "testingotel12:443")
	_, err := InitTracer()
	if err != nil {
		t.Errorf("Expected no error but got %v", err)
	}
}
