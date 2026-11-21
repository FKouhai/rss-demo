package oidc

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/FKouhai/rss-demo/libs/auth"
	"golang.org/x/oauth2"
)

func testKey() []byte {
	k := make([]byte, 32)
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
		"SessionKey":   func(c *Config) { c.SessionKey = nil },
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

func TestSessionMiddleware_ValidCookie(t *testing.T) {
	c := &Client{cfg: Config{SessionKey: testKey()}}
	sealed, err := c.Seal(Session{
		Sub: "user-123", Email: "you@example.com", TenantID: "acme",
		Groups: []string{"acme"}, Expiry: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	var gotTenant string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTenant = auth.TenantID(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "__Host-session", Value: sealed})
	rr := httptest.NewRecorder()
	c.SessionMiddleware(next).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || gotTenant != "acme" {
		t.Errorf("got code=%d tenant=%q, want 200 acme", rr.Code, gotTenant)
	}
}

func TestSessionMiddleware_Unauthenticated(t *testing.T) {
	c := &Client{cfg: Config{SessionKey: testKey()}}
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	rr := httptest.NewRecorder()
	c.SessionMiddleware(next).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	if rr.Code != http.StatusFound {
		t.Errorf("got %d, want 302", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/auth/login" {
		t.Errorf("redirect = %q, want /auth/login", loc)
	}
}

func TestSessionMiddleware_HtmxUnauthenticated(t *testing.T) {
	c := &Client{cfg: Config{SessionKey: testKey()}}
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/partials/items", nil)
	req.Header.Set("HX-Request", "true")
	rr := httptest.NewRecorder()
	c.SessionMiddleware(next).ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized || called {
		t.Errorf("got code=%d called=%v, want 401 next-not-called", rr.Code, called)
	}
	if got := rr.Header().Get("HX-Redirect"); got != "/auth/login" {
		t.Errorf("HX-Redirect = %q, want /auth/login", got)
	}
}

func TestRoundTrip(t *testing.T) {
	c := &Client{cfg: Config{SessionKey: testKey()}}
	s := Session{
		Sub:      "user-123",
		Email:    "you@example.com",
		TenantID: "acme",
		Groups:   []string{"acme"},
		Expiry:   time.Now().Add(time.Hour),
	}

	enc, err := c.Seal(s)
	if err != nil {
		t.Fatalf("Seal error: %v", err)
	}

	raw, err := c.Open(enc)
	if err != nil {
		t.Fatalf("Open seal error: %v", err)
	}

	if s.Sub != raw.Sub || s.Email != raw.Email || s.TenantID != raw.TenantID {
		t.Fatalf("sessions do not match")
	}
}

func TestExpiredSession(t *testing.T) {
	c := &Client{cfg: Config{SessionKey: testKey()}}
	s := Session{
		Sub:      "user-123",
		Email:    "you@example.com",
		TenantID: "acme",
		Groups:   []string{"acme"},
		Expiry:   time.Now().AddDate(0, -1, 0),
	}

	enc, err := c.Seal(s)
	if err != nil {
		t.Fatalf("Seal error: %v", err)
	}

	_, err = c.Open(enc)
	if err == nil {
		t.Fatal("expected expired session error")
	}

}

func TestEmptyTenant(t *testing.T) {
	c := &Client{cfg: Config{SessionKey: testKey()}}
	s := Session{
		Sub:      "user-123",
		Email:    "you@example.com",
		TenantID: "",
		Groups:   []string{"acme"},
		Expiry:   time.Now().Add(time.Hour),
	}

	enc, err := c.Seal(s)
	if err != nil {
		t.Fatalf("Seal error: %v", err)
	}

	_, err = c.Open(enc)
	if err == nil {
		t.Fatal("expected empty tenant error")
	}

}

func TestTampered(t *testing.T) {
	c := &Client{cfg: Config{SessionKey: testKey()}}
	s := Session{
		Sub:      "user-123",
		Email:    "you@example.com",
		TenantID: "acme",
		Groups:   []string{"acme"},
		Expiry:   time.Now().Add(time.Hour),
	}
	enc, err := c.Seal(s)
	if err != nil {
		t.Fatalf("Seal error: %v", err)
	}

	first := enc[0]
	replacement := byte('A')
	if first == 'A' {
		replacement = 'B'
	}
	tampered := string(replacement) + enc[1:]
	if _, err := c.Open(tampered); err == nil {
		t.Fatal("expected tampered session error")
	}
}

func TestAuthURL(t *testing.T) {
	c := &Client{cfg: Config{ClientID: "rss-frontend",
		RedirectURL: "https://rss.example.com/auth/callback",
		SessionKey:  testKey()},
	}

	c.oauth = oauth2.Config{
		ClientID:    "rss-frontend",
		RedirectURL: "https://rss.example.com/auth/callback",
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://id.example.com/auth",
			TokenURL: "https://id.example.com/token",
		},
		Scopes: []string{"openid", "email", "profile", "groups"},
	}

	req, err := c.AuthURL()
	if err != nil {
		t.Fatalf("create auth url: %v", err)
	}

	u, _ := url.Parse(req.URL)
	q := u.Query()
	if got := q.Get("client_id"); got != c.cfg.ClientID {
		t.Fatalf("expected %q got %q", c.cfg.ClientID, got)
	}

	if got := q.Get("redirect_uri"); got != c.cfg.RedirectURL {
		t.Fatalf("expected %q got %q", c.cfg.RedirectURL, got)
	}

	if got := q.Get("response_type"); got != "code" {
		t.Fatalf("expected %q got %q", "code", got)
	}

	if got := q.Get("code_challenge_method"); got != "S256" {
		t.Fatalf("expected %q got %q", "S256", got)
	}

	if got := q.Get("code_challenge"); got != oauth2.S256ChallengeFromVerifier(req.Verifier) {
		t.Fatalf("expected %q got %q", oauth2.S256ChallengeFromVerifier(req.Verifier), got)
	}

	if got := q.Get("state"); got != req.State {
		t.Fatalf("expected %q got %q", req.State, got)
	}

	if got := q.Get("nonce"); got != req.Nonce {
		t.Fatalf("expected %q got %q", req.Nonce, got)
	}
}
func TestRoundTripSession(t *testing.T) {
	c := &Client{cfg: Config{SessionKey: testKey()}}
	a := AuthRequest{State: "test-state", Nonce: "test-nonce", Verifier: "test-verifier"}

	enc, err := c.SealState(a)
	if err != nil {
		t.Fatalf("Seal error: %v", err)
	}

	raw, err := c.OpenState(enc)
	if err != nil {
		t.Fatalf("Open seal error: %v", err)
	}

	if a.Nonce != raw.Nonce || a.State != raw.State || a.Verifier != raw.Verifier {
		t.Fatalf("state do not match")
	}
}

func TestExpiredSessionState(t *testing.T) {
	c := &Client{cfg: Config{SessionKey: testKey()}}
	a := AuthRequest{State: "test-state", Nonce: "test-nonce", Verifier: "test-verifier"}
	enc, err := c.sealState(a, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("Seal error: %v", err)
	}

	_, err = c.OpenState(enc)
	if err == nil {
		t.Fatal("expected expired state error")
	}
}

func TestTamperedSession(t *testing.T) {
	c := &Client{cfg: Config{SessionKey: testKey()}}
	a := AuthRequest{State: "test-state", Nonce: "test-nonce", Verifier: "test-verifier"}
	enc, err := c.SealState(a)
	if err != nil {
		t.Fatalf("Seal error: %v", err)
	}

	first := enc[0]
	replacement := byte('A')
	if first == 'A' {
		replacement = 'B'
	}
	tampered := string(replacement) + enc[1:]
	if _, err := c.OpenState(tampered); err == nil {
		t.Fatal("expected tampered state error")
	}
}

// mockIdP is a minimal OIDC issuer for Exchange tests: discovery + JWKS +
// a token endpoint that mints RS256 ID tokens. The minted token varies by
// the posted code so failure paths stay hermetic.
type mockIdP struct {
	srv *httptest.Server
	url string
	key *rsa.PrivateKey
}

const (
	mockNonce   = "test-nonce-123"
	mockSubject = "user-123"
)

func newMockIdP(t *testing.T) *mockIdP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	m := &mockIdP{key: key}
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := fmt.Fprintf(w, `{"issuer":%q,"authorization_endpoint":%q,"token_endpoint":%q,"jwks_uri":%q}`,
			srv.URL, srv.URL+"/auth", srv.URL+"/token", srv.URL+"/keys"); err != nil {
			http.Error(w, "write failed", http.StatusInternalServerError)
		}
	})
	mux.HandleFunc("/token", m.tokenHandler)
	mux.HandleFunc("/keys", m.jwksHandler)
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	m.srv = srv
	m.url = srv.URL
	return m
}

func (m *mockIdP) basePayload(nonce string) map[string]any {
	return map[string]any{
		"iss":                m.url,
		"aud":                "rss-frontend",
		"sub":                mockSubject,
		"exp":                time.Now().Add(time.Hour).Unix(),
		"iat":                time.Now().Add(-time.Minute).Unix(),
		"nonce":              nonce,
		"email":              "you@example.com",
		"email_verified":     true,
		"preferred_username": "you",
		"groups":             []string{"acme"},
	}
}

func (m *mockIdP) signJWT(payload map[string]any) (string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	input := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"test-key","typ":"JWT"}`)) +
		"." + base64.RawURLEncoding.EncodeToString(body)
	sum := sha256.Sum256([]byte(input))
	sig, err := rsa.SignPKCS1v15(rand.Reader, m.key, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return input + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func (m *mockIdP) tokenHandler(w http.ResponseWriter, r *http.Request) {
	payload := m.basePayload(mockNonce)
	code := r.FormValue("code")
	switch code {
	case "bad-nonce":
		payload["nonce"] = "wrong"
	case "bad-aud":
		payload["aud"] = "someone-else"
	case "expired":
		payload["exp"] = time.Now().Add(-time.Hour).Unix()
	}
	w.Header().Set("Content-Type", "application/json")
	resp := map[string]any{
		"access_token":  "mock-access",
		"token_type":    "Bearer",
		"expires_in":    3600,
		"refresh_token": "mock-refresh",
	}
	if code != "no-id-token" {
		jwt, err := m.signJWT(payload)
		if err != nil {
			http.Error(w, "sign failed", http.StatusInternalServerError)
			return
		}
		resp["id_token"] = jwt
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		http.Error(w, "write failed", http.StatusInternalServerError)
	}
}

func (m *mockIdP) jwksHandler(w http.ResponseWriter, _ *http.Request) {
	n := base64.RawURLEncoding.EncodeToString(m.key.N.Bytes())
	e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(m.key.E)).Bytes())
	w.Header().Set("Content-Type", "application/json")
	if _, err := fmt.Fprintf(w, `{"keys":[{"kty":"RSA","kid":"test-key","use":"sig","alg":"RS256","n":%q,"e":%q}]}`, n, e); err != nil {
		http.Error(w, "write failed", http.StatusInternalServerError)
	}
}

func exchangeClient(t *testing.T, idp *mockIdP) *Client {
	t.Helper()
	c, err := NewClient(context.Background(), validConfig(idp.url))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

func TestExchange_Success(t *testing.T) {
	idp := newMockIdP(t)
	c := exchangeClient(t, idp)
	s, err := c.Exchange(context.Background(), "test-code", "any-verifier", mockNonce)
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if s.Sub != mockSubject || s.Email != "you@example.com" || s.TenantID != "acme" {
		t.Errorf("session = %+v, want sub/email/acme", s)
	}
	if len(s.Groups) != 1 || s.Groups[0] != "acme" {
		t.Errorf("groups = %v, want [acme]", s.Groups)
	}
	if time.Until(s.Expiry) > time.Hour || time.Until(s.Expiry) <= 0 {
		t.Errorf("expiry = %v, want ~1h future", s.Expiry)
	}
	if s.RefreshToken != "mock-refresh" {
		t.Errorf("refresh = %q, want mock-refresh", s.RefreshToken)
	}
}

func TestExchange_NonceMismatch(t *testing.T) {
	idp := newMockIdP(t)
	c := exchangeClient(t, idp)
	if _, err := c.Exchange(context.Background(), "test-code", "any-verifier", "other-nonce"); err == nil {
		t.Fatal("expected nonce mismatch error")
	}
}

func TestExchange_TokenWrongNonce(t *testing.T) {
	idp := newMockIdP(t)
	c := exchangeClient(t, idp)
	if _, err := c.Exchange(context.Background(), "bad-nonce", "any-verifier", mockNonce); err == nil {
		t.Fatal("expected nonce mismatch error")
	}
}

func TestExchange_BadAudience(t *testing.T) {
	idp := newMockIdP(t)
	c := exchangeClient(t, idp)
	if _, err := c.Exchange(context.Background(), "bad-aud", "any-verifier", mockNonce); err == nil {
		t.Fatal("expected audience error")
	}
}

func TestExchange_Expired(t *testing.T) {
	idp := newMockIdP(t)
	c := exchangeClient(t, idp)
	if _, err := c.Exchange(context.Background(), "expired", "any-verifier", mockNonce); err == nil {
		t.Fatal("expected expired error")
	}
}

func TestExchange_MissingIDToken(t *testing.T) {
	idp := newMockIdP(t)
	c := exchangeClient(t, idp)
	if _, err := c.Exchange(context.Background(), "no-id-token", "any-verifier", mockNonce); err == nil {
		t.Fatal("expected missing id_token error")
	}
}
