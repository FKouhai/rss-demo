// Package service collapses the bootstrap prologue every Go service repeats
// (tracer init, otelhttp HTTP serve) into two calls. It is the shared core
// behind each service's cmd.Run; service-specific wiring (NATS, store,
// handlers) stays in the service's own cmd package.
//
// Before (repeated ~40 lines per service): InitTracer + deferred Shutdown,
// net.Listen + otelhttp + Serve goroutine. After:
//
//	handle, _ := service.Bootstrap(ctx, service.Config{ServiceName: "outbox"})
//	srv, _ := service.ServeHTTP(":3000", mux, "rss_outbox")
//
// Both helpers stay non-fatal the way the hand-rolled code was: a failed
// tracer init is logged, not returned as an error, so services still boot
// in dev/test environments.
package service

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"

	"github.com/FKouhai/rss-demo/libs/instrumentation"
	log "github.com/FKouhai/rss-demo/libs/logger"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Config describes a service instance. Zero values resolve to production
// defaults (FQDN "<name>:3000", HTTPAddr ":3000").
type Config struct {
	ServiceName string
	ServiceFQDN string
	HTTPAddr    string
	// SkipTracer bypasses the OTEL exporter init (which dials
	// localhost:4317). Set by tests so they stay offline/hermetic.
	SkipTracer bool
}

// Handle owns the bootstrap-owned resources for a service: the tracer
// provider. Close it with Shutdown.
type Handle struct {
	ServiceName string
	FQDN        string
	TP          *sdktrace.TracerProvider
}

// EnvOr returns the named environment variable, or def when unset/empty.
func EnvOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// ResolveFQDN returns SERVICE_FQDN, defaulting to "<serviceName>:3000"
// so each service keeps its historical fallback in one place.
func ResolveFQDN(serviceName string) string {
	return EnvOr("SERVICE_FQDN", serviceName+":3000")
}

// Bootstrap inits the tracer. A tracer failure is logged, not returned,
// matching the historical per-service behavior.
func Bootstrap(ctx context.Context, cfg Config) (*Handle, error) {
	if cfg.ServiceName == "" {
		return nil, errors.New("service: ServiceName is required")
	}
	if cfg.ServiceFQDN == "" {
		cfg.ServiceFQDN = ResolveFQDN(cfg.ServiceName)
	}

	h := &Handle{ServiceName: cfg.ServiceName, FQDN: cfg.ServiceFQDN}

	if !cfg.SkipTracer {
		tp, err := instrumentation.InitTracer(cfg.ServiceName)
		if err != nil {
			log.Error(err.Error())
		} else {
			h.TP = tp
		}
	}

	return h, nil
}

// Shutdown flushes/shuts down the tracer, returning any shutdown error.
func (h *Handle) Shutdown(ctx context.Context) error {
	if h.TP != nil {
		_ = h.TP.ForceFlush(ctx)
		return h.TP.Shutdown(ctx)
	}
	return nil
}

// Server is a bound HTTP server. Ready fires once the OS is accepting
// connections (before Serve loops, so it is deterministic and race-free).
type Server struct {
	srv   *http.Server
	ln    net.Listener
	ready chan struct{}
}

// ServeHTTP binds addr, wraps handler with otelhttp, and serves in the
// background. It returns once the listener is bound.
// optionally as a fourth arg it mounts /metrics (unwrapped)
func ServeHTTP(addr string, handler http.Handler, serviceName string, metrics ...http.Handler) (*Server, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}

	base := otelhttp.NewHandler(handler, serviceName)
	if len(metrics) > 0 && metrics[0] != nil {
		mux := http.NewServeMux()
		mux.Handle("/metrics", metrics[0])
		mux.Handle("/", base)
		base = mux
	}

	s := &Server{
		srv:   &http.Server{Handler: base},
		ln:    ln,
		ready: make(chan struct{}),
	}
	close(s.ready)

	go func() {
		if err := s.srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.ErrorFmt("http server: %v", err)
		}
	}()

	log.InfoFmt("starting server on %s", ln.Addr())
	return s, nil
}

// Ready fires once the HTTP listener is bound.
func (s *Server) Ready() <-chan struct{} { return s.ready }

// Addr is the bound HTTP address (useful when addr was ":0").
func (s *Server) Addr() net.Addr { return s.ln.Addr() }

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error { return s.srv.Shutdown(ctx) }

// BaseConfig carries the bootstrap settings shared by every Go service.
// Service-specific cmd packages reuse it directly (via alias when they add
// nothing, e.g. `type Config = service.BaseConfig`) or embed it and add
// their own fields (NATS URL, store DSN, ...).
type BaseConfig struct {
	ServiceFQDN string
	HTTPAddr    string // defaults to ":3000"

	// SkipTracer skips OTEL exporter init (which dials localhost:4317).
	// Set by tests so they stay offline/hermetic.
	SkipTracer bool
}

// WithDefaults fills zero values: HTTPAddr ":3000" and ServiceFQDN
// SERVICE_FQDN or "<serviceName>:3000".
func (c *BaseConfig) WithDefaults(serviceName string) {
	if c.HTTPAddr == "" {
		c.HTTPAddr = ":3000"
	}
	if c.ServiceFQDN == "" {
		c.ServiceFQDN = ResolveFQDN(serviceName)
	}
}

// Bootstrap inits the tracer for serviceName.
func (c BaseConfig) Bootstrap(ctx context.Context, serviceName string) (*Handle, error) {
	return Bootstrap(ctx, Config{
		ServiceName: serviceName,
		ServiceFQDN: c.ServiceFQDN,
		SkipTracer:  c.SkipTracer,
	})
}

// BaseApp owns the bootstrap-owned lifecycle shared by every service: the
// HTTP server and the tracer handle. Service cmd packages embed
// `service.BaseApp` and add their own handles (NATS, store, ...);
// Ready/Addr/Shutdown promote, and Shutdown stays nil-tolerant so failure
// paths mid-Run can delegate to it unconditionally.
type BaseApp struct {
	HTTP *Server
	Boot *Handle
}

// Ready fires once the HTTP listener is bound.
func (a *BaseApp) Ready() <-chan struct{} { return a.HTTP.Ready() }

// Addr is the bound HTTP address (useful when HTTPAddr was ":0").
func (a *BaseApp) Addr() net.Addr { return a.HTTP.Addr() }

// Shutdown stops the server and the tracer, joining errors.
func (a *BaseApp) Shutdown(ctx context.Context) error {
	var errs []error
	if a.HTTP != nil {
		errs = append(errs, a.HTTP.Shutdown(ctx))
	}
	if a.Boot != nil {
		errs = append(errs, a.Boot.Shutdown(ctx))
	}
	return errors.Join(errs...)
}
