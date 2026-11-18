package service

import (
	"context"
	"io"
	"net/http"
	"sync/atomic"
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

func TestBootstrap_SkipsOffline(t *testing.T) {
	t.Setenv("SERVICE_FQDN", "")
	h, err := Bootstrap(context.Background(), Config{
		ServiceName: "test-svc",
		SkipLocator: true,
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

func TestBootstrap_RegistersWithLocator(t *testing.T) {
	var registers atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/register", func(w http.ResponseWriter, _ *http.Request) {
		registers.Add(1)
		_, _ = w.Write([]byte(`{"message":"ok"}`))
	})
	srv, err := ServeHTTP("127.0.0.1:0", mux, "fake-locator")
	if err != nil {
		t.Fatalf("ServeHTTP: %v", err)
	}
	<-srv.Ready()
	defer srv.Shutdown(context.Background()) // nolint

	t.Setenv("LOCATOR_URL", "http://"+srv.Addr().String())

	h, err := Bootstrap(context.Background(), Config{
		ServiceName:       "test-svc",
		SkipTracer:        true,
		HeartbeatInterval: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	defer h.Shutdown(context.Background()) // nolint

	deadline := time.Now().Add(2 * time.Second)
	for registers.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := registers.Load(); got < 2 {
		t.Errorf("register calls = %d, want >= 2 (initial + heartbeat)", got)
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
	h, err := BaseConfig{SkipLocator: true, SkipTracer: true}.Bootstrap(context.Background(), "test-svc")
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
