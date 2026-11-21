package oidc

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func testKey() [32]byte {
	var k [32]byte
	for i := range k {
		k[i] = byte(i + 1)
	}
	return k
}

func validConfig(issuer string) Config {
	return Config{
		IssuerURL:    issuer,
		ClientID:     "rss-frontend",
		ClientSecret: "secret",
		RedirectURL:  "https://rss.example.com/auth/callback",
		SessionKey:   testKey(),
	}
}

// discoveryServer serves the OIDC discovery doc go-oidc expects.
func discoveryServer(t *testing.T) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := fmt.Fprintf(w, `{"issuer":%q,"authorization_endpoint":%q,"token_endpoint":%q,"jwks_uri":%q}`,
			srv.URL, srv.URL+"/auth", srv.URL+"/token", srv.URL+"/keys"); err != nil {
			http.Error(w, "write failed", http.StatusInternalServerError)
			return
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestNewClient_NilContext(t *testing.T) {
	//nolint:staticcheck // intentionally passing nil to verify the fail-hard guard
	if _, err := NewClient(nil, validConfig("https://id.example.com")); err == nil {
		t.Error("expected error for nil ctx")
	}
}

func TestNewClient_Validation(t *testing.T) {
	full := validConfig("https://id.example.com")
	cases := map[string]func(*Config){
		"IssuerURL":    func(c *Config) { c.IssuerURL = "" },
		"ClientID":     func(c *Config) { c.ClientID = "" },
		"ClientSecret": func(c *Config) { c.ClientSecret = "" },
		"RedirectURL":  func(c *Config) { c.RedirectURL = "" },
		"SessionKey":   func(c *Config) { c.SessionKey = [32]byte{} },
	}
	for name, mutate := range cases {
		cfg := full
		mutate(&cfg)
		if _, err := NewClient(context.Background(), cfg); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestNewClient_DiscoveryFailure(t *testing.T) {
	cfg := validConfig("http://127.0.0.1:1") // unroutable, hermetic
	if _, err := NewClient(context.Background(), cfg); err == nil {
		t.Error("expected error for unreachable issuer")
	}
}

func TestNewClient_SuccessDefaultsScopes(t *testing.T) {
	srv := discoveryServer(t)
	c, err := NewClient(context.Background(), validConfig(srv.URL))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if c.provider == nil || c.verifier == nil {
		t.Fatal("provider/verifier must be set")
	}
	want := []string{"openid", "email", "profile", "groups"}
	if len(c.cfg.Scopes) != len(want) {
		t.Fatalf("scopes = %v, want %v", c.cfg.Scopes, want)
	}
	for i := range want {
		if c.cfg.Scopes[i] != want[i] {
			t.Fatalf("scopes = %v, want %v", c.cfg.Scopes, want)
		}
	}
	if c.oauth.Endpoint.AuthURL != srv.URL+"/auth" {
		t.Errorf("auth URL = %q", c.oauth.Endpoint.AuthURL)
	}
	if c.oauth.RedirectURL != "https://rss.example.com/auth/callback" {
		t.Errorf("redirect = %q", c.oauth.RedirectURL)
	}
}

func TestNewClient_CustomScopesKept(t *testing.T) {
	srv := discoveryServer(t)
	cfg := validConfig(srv.URL)
	cfg.Scopes = []string{"openid", "email"}
	c, err := NewClient(context.Background(), cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if len(c.cfg.Scopes) != 2 || c.cfg.Scopes[1] != "email" {
		t.Errorf("scopes = %v, want custom kept", c.cfg.Scopes)
	}
}

func TestStubs_NotImplemented(t *testing.T) {
	srv := discoveryServer(t)
	c, err := NewClient(context.Background(), validConfig(srv.URL))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	for name, h := range map[string]http.Handler{
		"login":    c.LoginHandler(),
		"callback": c.CallbackHandler(),
		"logout":   c.LogoutHandler(),
	} {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
		if rr.Code != http.StatusNotImplemented {
			t.Errorf("%s: got %d, want 501", name, rr.Code)
		}
	}
}

func TestSessionMiddleware_Passthrough(t *testing.T) {
	srv := discoveryServer(t)
	c, err := NewClient(context.Background(), validConfig(srv.URL))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	rr := httptest.NewRecorder()
	c.SessionMiddleware(next).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	if rr.Code != http.StatusOK || !called {
		t.Errorf("passthrough failed: code=%d called=%v", rr.Code, called)
	}
}
