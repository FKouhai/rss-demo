package service

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"
)

func TestEnvOr(t *testing.T) {
	t.Setenv("SERVICE_TEST_KEY", "value")
	if got := EnvOr("SERVICE_TEST_KEY", "def"); got != "value" {
		t.Errorf("EnvOr set = %q, want value", got)
	}
	t.Setenv("SERVICE_TEST_KEY", "")
	if got := EnvOr("SERVICE_TEST_KEY", "def"); got != "def" {
		t.Errorf("EnvOr empty = %q, want def", got)
	}
}

func TestResolveFQDN(t *testing.T) {
	t.Setenv("SERVICE_FQDN", "")
	if got := ResolveFQDN("poller"); got != "poller:3000" {
		t.Errorf("default = %q, want poller:3000", got)
	}
	t.Setenv("SERVICE_FQDN", "poller.svc:3000")
	if got := ResolveFQDN("poller"); got != "poller.svc:3000" {
		t.Errorf("env = %q, want poller.svc:3000", got)
	}
}

func TestBootstrap_RequiresServiceName(t *testing.T) {
	if _, err := Bootstrap(context.Background(), Config{}); err == nil {
		t.Error("expected error for empty ServiceName")
	}
}

func TestBootstrap_SkipsTracer(t *testing.T) {
	t.Setenv("SERVICE_FQDN", "")
	h, err := Bootstrap(context.Background(), Config{
		ServiceName: "test-svc",
		SkipTracer:  true,
	})
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if h.FQDN != "test-svc:3000" {
		t.Errorf("FQDN = %q, want test-svc:3000", h.FQDN)
	}
	if h.TP != nil {
		t.Error("TP should be nil when tracer is skipped")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := h.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown: %v", err)
	}
}

func TestServeHTTP_ReadyAndShutdown(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	s, err := ServeHTTP("127.0.0.1:0", mux, "test-svc")
	if err != nil {
		t.Fatalf("ServeHTTP: %v", err)
	}
	select {
	case <-s.Ready():
	case <-time.After(2 * time.Second):
		t.Fatal("Ready did not fire")
	}

	resp, err := http.Get("http://" + s.Addr().String() + "/healthz")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	defer func() { _ = resp.Body.Close() }()
	if string(body) != "ok" {
		t.Errorf("body = %q, want ok", body)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown: %v", err)
	}
}

func TestServeHTTP_MetricsMounted(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	stub := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("stub-metrics"))
	})
	s, err := ServeHTTP("127.0.0.1:0", mux, "test-svc", stub)
	if err != nil {
		t.Fatalf("ServeHTTP: %v", err)
	}
	select {
	case <-s.Ready():
	case <-time.After(2 * time.Second):
		t.Fatal("Ready did not fire")
	}

	// /metrics serves the injected handler, untouched by otel wrapping.
	resp, err := http.Get("http://" + s.Addr().String() + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if string(body) != "stub-metrics" {
		t.Errorf("/metrics body = %q, want stub-metrics", body)
	}

	// Service routes still work alongside it.
	resp, err = http.Get("http://" + s.Addr().String() + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if string(body) != "ok" {
		t.Errorf("/healthz body = %q, want ok", body)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown: %v", err)
	}
}

func TestServeHTTP_NoMetrics(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	// No fourth arg and explicit nil both mean: no /metrics route.
	for _, extra := range [][]http.Handler{nil, {nil}} {
		s, err := ServeHTTP("127.0.0.1:0", mux, "test-svc", extra...)
		if err != nil {
			t.Fatalf("ServeHTTP: %v", err)
		}
		select {
		case <-s.Ready():
		case <-time.After(2 * time.Second):
			t.Fatal("Ready did not fire")
		}
		resp, err := http.Get("http://" + s.Addr().String() + "/metrics")
		if err != nil {
			t.Fatalf("GET /metrics: %v", err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("/metrics status = %d, want 404 when unmounted", resp.StatusCode)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		if err := s.Shutdown(ctx); err != nil {
			t.Errorf("Shutdown: %v", err)
		}
		cancel()
	}
}

func TestBaseConfig_WithDefaults(t *testing.T) {
	t.Setenv("SERVICE_FQDN", "")
	c := BaseConfig{}
	c.WithDefaults("poller")
	if c.HTTPAddr != ":3000" {
		t.Errorf("HTTPAddr = %q, want :3000", c.HTTPAddr)
	}
	if c.ServiceFQDN != "poller:3000" {
		t.Errorf("ServiceFQDN = %q, want poller:3000", c.ServiceFQDN)
	}

	// Set values are preserved; env FQDN is honored.
	t.Setenv("SERVICE_FQDN", "custom:4000")
	c = BaseConfig{HTTPAddr: ":8080", ServiceFQDN: "keep:3000"}
	c.WithDefaults("notify")
	if c.HTTPAddr != ":8080" || c.ServiceFQDN != "keep:3000" {
		t.Errorf("WithDefaults overwrote set values: %+v", c)
	}
	c = BaseConfig{}
	c.WithDefaults("notify")
	if c.ServiceFQDN != "custom:4000" {
		t.Errorf("ServiceFQDN = %q, want custom:4000", c.ServiceFQDN)
	}
}

func TestBaseConfig_BootstrapSkips(t *testing.T) {
	t.Setenv("SERVICE_FQDN", "")
	h, err := BaseConfig{SkipTracer: true}.Bootstrap(context.Background(), "test-svc")
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if h.FQDN != "test-svc:3000" {
		t.Errorf("FQDN = %q, want test-svc:3000", h.FQDN)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := h.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown: %v", err)
	}
}

func TestBaseApp_ShutdownNil(t *testing.T) {
	var a BaseApp
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := a.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown on zero BaseApp = %v, want nil", err)
	}
}

func TestBaseApp_ReadyAddrShutdown(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	srv, err := ServeHTTP("127.0.0.1:0", mux, "test-svc")
	if err != nil {
		t.Fatalf("ServeHTTP: %v", err)
	}
	a := BaseApp{HTTP: srv}
	select {
	case <-a.Ready():
	case <-time.After(2 * time.Second):
		t.Fatal("Ready did not fire")
	}
	if a.Addr() == nil {
		t.Fatal("Addr is nil")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := a.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown: %v", err)
	}
}
