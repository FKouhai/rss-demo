package auth

import (
	"context"
	"crypto/subtle"
	"net/http"
	"slices"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Config for gateway header-trust auth (services never touch OIDC).
type Config struct {
	// Shared secret the gateway injects (AUTH_GATEWAY_SECRET).
	// Requests without it are rejected unless SkipAuth.
	// Empty secret without SkipAuth is a misconfiguration and fails closed (500).
	GatewaySecret string
	// SkipAuth for `go test -race` hermetic runs (same pattern as SkipTracer).
	SkipAuth bool
	// Header mapping — defaults cover oauth2-proxy + traefik-forward-auth + TinyAuth.
	// Override only if your gateway sends something custom.
	UserHeader   string // default "X-Forwarded-User"
	EmailHeader  string // default "X-Forwarded-Email"
	GroupsHeader string // default "X-Forwarded-Groups"
	TenantHeader string // default "X-Tenant-ID" (gateway maps OIDC groups claim -> tenant)
	// Bypass paths — healthz/ready never require auth.
	PublicPaths []string // default ["/healthz", "/ready"]
}

// Identity what user
type Identity struct {
	TenantID string
	User     string // sub or username
	Email    string
	Groups   []string
}

// WithDefaults default config for auth
func (c *Config) WithDefaults() {
	if c.UserHeader == "" {
		c.UserHeader = "X-Forwarded-User"
	}
	if c.EmailHeader == "" {
		c.EmailHeader = "X-Forwarded-Email"
	}
	if c.GroupsHeader == "" {
		c.GroupsHeader = "X-Forwarded-Groups"
	}
	if c.TenantHeader == "" {
		c.TenantHeader = "X-Tenant-ID"
	}
	if c.PublicPaths == nil {
		c.PublicPaths = []string{"/healthz", "/ready"}
	}
}

type ctxKey struct{}

// Middleware shared http handler for gateway header-trust auth.
func Middleware(cfg Config, next http.Handler) http.Handler {
	cfg.WithDefaults()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if slices.Contains(cfg.PublicPaths, r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		if cfg.SkipAuth {
			r = r.WithContext(context.WithValue(r.Context(), ctxKey{}, Identity{TenantID: "dev", User: "dev"}))
			next.ServeHTTP(w, r)
			return
		}

		if cfg.GatewaySecret == "" {
			http.Error(w, `{"error":"auth misconfigured"}`, http.StatusInternalServerError)
			return
		}

		got := r.Header.Get("X-Gateway-Secret")
		if subtle.ConstantTimeCompare([]byte(got), []byte(cfg.GatewaySecret)) != 1 {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		id := Identity{
			TenantID: r.Header.Get(cfg.TenantHeader),
			User:     r.Header.Get(cfg.UserHeader),
			Email:    r.Header.Get(cfg.EmailHeader),
		}
		if g := r.Header.Get(cfg.GroupsHeader); g != "" {
			for s := range strings.SplitSeq(g, ",") {
				if s = strings.TrimSpace(s); s != "" {
					id.Groups = append(id.Groups, s)
				}
			}
		}
		if id.TenantID == "" && len(id.Groups) > 0 {
			id.TenantID = id.Groups[0]
		}
		if id.TenantID == "" {
			http.Error(w, `{"error":"missing tenant"}`, http.StatusUnauthorized)
			return
		}
		trace.SpanFromContext(r.Context()).SetAttributes(attribute.String("tenant.id", id.TenantID))
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, id)))
	})
}

func FromContext(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(ctxKey{}).(Identity)
	return id, ok
}

// WithIdentity returns a context carrying id for FromContext/TenantID.
func WithIdentity(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// TenantID returns the tenant for ctx, or "" when anonymous/missing.
func TenantID(ctx context.Context) string {
	id, ok := FromContext(ctx)
	if !ok {
		return ""
	}
	return id.TenantID
}
