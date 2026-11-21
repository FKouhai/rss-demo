package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func nextOK() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestMiddleware_PublicPathsBypass(t *testing.T) {
	cfg := Config{GatewaySecret: "s"}
	for _, p := range []string{"/healthz", "/ready"} {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		rr := httptest.NewRecorder()
		Middleware(cfg, nextOK()).ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("%s: got %d, want 200", p, rr.Code)
		}
	}
}

func TestMiddleware_SkipAuth(t *testing.T) {
	cfg := Config{SkipAuth: true}
	req := httptest.NewRequest(http.MethodGet, "/api/items", nil)
	rr := httptest.NewRecorder()
	var got string
	h := Middleware(cfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = TenantID(r.Context())
		w.WriteHeader(http.StatusOK)
	}))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || got != "dev" {
		t.Errorf("got code=%d tenant=%q, want 200 dev", rr.Code, got)
	}
}

func TestMiddleware_WrongSecret(t *testing.T) {
	cfg := Config{GatewaySecret: "correct"}
	req := httptest.NewRequest(http.MethodGet, "/api/items", nil)
	req.Header.Set("X-Gateway-Secret", "wrong")
	rr := httptest.NewRecorder()
	Middleware(cfg, nextOK()).ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("got %d, want 401", rr.Code)
	}
}

func TestMiddleware_ValidTenantHeaderNoGroups(t *testing.T) {
	cfg := Config{GatewaySecret: "s"}
	req := httptest.NewRequest(http.MethodGet, "/api/items", nil)
	req.Header.Set("X-Gateway-Secret", "s")
	req.Header.Set("X-Tenant-ID", "acme")
	rr := httptest.NewRecorder()
	var got string
	h := Middleware(cfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = TenantID(r.Context())
		w.WriteHeader(http.StatusOK)
	}))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || got != "acme" {
		t.Errorf("got code=%d tenant=%q, want 200 acme", rr.Code, got)
	}
}

func TestMiddleware_MissingTenantDeniedNotHung(t *testing.T) {
	cfg := Config{GatewaySecret: "s"}
	req := httptest.NewRequest(http.MethodGet, "/api/items", nil)
	req.Header.Set("X-Gateway-Secret", "s")
	rr := httptest.NewRecorder()
	Middleware(cfg, nextOK()).ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("got %d, want 401 (must not hang)", rr.Code)
	}
}

func TestMiddleware_EmptySecretFailsClosed(t *testing.T) {
	cfg := Config{}
	req := httptest.NewRequest(http.MethodGet, "/api/items", nil)
	rr := httptest.NewRecorder()
	Middleware(cfg, nextOK()).ServeHTTP(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("got %d, want 500", rr.Code)
	}
}

func TestTenantID_Anonymous(t *testing.T) {
	if got := TenantID(context.Background()); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}
