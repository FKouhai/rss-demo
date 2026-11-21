package oidc

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/FKouhai/rss-demo/libs/auth"
	"github.com/coreos/go-oidc/v3/oidc"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/oauth2"
)

// Config odic config struct
type Config struct {
	IssuerURL     string   // OIDC_ISSUER_URL, e.g. https://id.example.com
	ClientID      string   // OIDC_CLIENT_ID      (from Pocket-ID client setup)
	ClientSecret  string   // OIDC_CLIENT_SECRET  (confidential client)
	RedirectURL   string   // OIDC_REDIRECT_URL, e.g. https://rss.example.com/auth/callback
	Scopes        []string // default ["openid","email","profile","groups"]
	SessionKey    []byte   // AUTH_SESSION_KEY (base64 32B) — AES-GCM for the cookie
	SessionCookie string   // default "__Host-session"
	StateCookie   string   // default "__Host-oauth-state" (short-lived PKCE bundle)
	LoginPath     string   // default "/auth/login"
}

// Client oidc client
type Client struct {
	cfg      Config
	provider *oidc.Provider        // discovery doc + JWKS, from oidc.NewProvider
	verifier *oidc.IDTokenVerifier // provider.Verifier(&oidc.Config{ClientID})
	oauth    oauth2.Config         // Endpoint: provider.Endpoint(), PKCE per-login
}

// Claims OIDC claims
type Claims struct {
	Sub, Email, PreferredUsername string
	EmailVerified                 bool
	Groups                        []string
	Nonce                         string `json:"nonce"`
}

// Session data required for a session
type Session struct { // encrypted JSON in __Host-session cookie
	Sub          string    `json:"sub"`
	Email        string    `json:"email"`
	TenantID     string    `json:"tenant_id"`
	Groups       []string  `json:"groups"`
	Expiry       time.Time `json:"expiry"`        // ID-token expiry
	RefreshToken string    `json:"refresh_token"` // encrypted alongside, for silent refresh
}

// AuthRequest bundles everything the login handler persists
type AuthRequest struct {
	URL, State, Nonce, Verifier string
}

// WithDefaults fills zero values: Scopes, SessionCookie, LoginPath.
func (c *Config) WithDefaults() {
	if len(c.Scopes) == 0 {
		c.Scopes = []string{"openid", "email", "profile", "groups"}
	}
	if c.SessionCookie == "" {
		c.SessionCookie = "__Host-session"
	}
	if c.StateCookie == "" {
		c.StateCookie = "__Host-oauth-state"
	}
	if c.LoginPath == "" {
		c.LoginPath = "/auth/login"
	}
}

// NewClient client constructor. Does OIDC discovery against IssuerURL,
// builds the ID-token verifier and oauth2 config. Fail-hard: a nil ctx,
// missing fields, or unreachable issuer returns an error so callers never
// get a client that can't verify.
func NewClient(ctx context.Context, cfg Config) (*Client, error) {
	if ctx == nil {
		return nil, fmt.Errorf("oidc new client context is nil")
	}
	if cfg.IssuerURL == "" {
		return nil, fmt.Errorf("oidc: IssuerURL is required")
	}

	if cfg.ClientID == "" {
		return nil, fmt.Errorf("oidc: ClientID is required")
	}
	if cfg.ClientSecret == "" {
		return nil, fmt.Errorf("oidc: ClientSecret is required")
	}
	if cfg.RedirectURL == "" {
		return nil, fmt.Errorf("oidc: RedirectURL is required")
	}
	if len(cfg.SessionKey) != 32 {
		return nil, fmt.Errorf("oidc: SessionKey must be 32 bytes")
	}
	cfg.WithDefaults()

	provider, err := oidc.NewProvider(ctx, cfg.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery %s: %w", cfg.IssuerURL, err)
	}

	return &Client{
		cfg:      cfg,
		provider: provider,
		verifier: provider.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
		oauth: oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			Endpoint:     provider.Endpoint(),
			RedirectURL:  cfg.RedirectURL,
			Scopes:       cfg.Scopes,
		},
	}, nil
}

// SessionCookieName is the login session cookie name.
func (c *Client) SessionCookieName() string {
	cfg := c.cfg
	cfg.WithDefaults()
	return cfg.SessionCookie
}

// StateCookieName is the short-lived PKCE/state cookie name.
func (c *Client) StateCookieName() string {
	cfg := c.cfg
	cfg.WithDefaults()
	return cfg.StateCookie
}

// SessionMiddleware shared session middleware
func (c *Client) SessionMiddleware(next http.Handler) http.Handler {
	cfg := c.cfg
	cfg.WithDefaults()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if cookie, err := r.Cookie(cfg.SessionCookie); err == nil {
			if s, err := c.Open(cookie.Value); err == nil {
				trace.SpanFromContext(r.Context()).SetAttributes(attribute.String("tenant.id", s.TenantID))
				ctx := auth.WithIdentity(r.Context(), auth.Identity{
					TenantID: s.TenantID,
					User:     s.Sub,
					Email:    s.Email,
					Groups:   s.Groups,
				})
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}
		if r.Header.Get("HX-Request") == "true" {
			w.Header().Set("HX-Redirect", cfg.LoginPath)
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		http.Redirect(w, r, cfg.LoginPath, http.StatusFound)
	})
}

// Seal serializes s as JSON, encrypts with AES-GCM under cfg.SessionKey
// Returns b64(nonce || cipher) for the cookie val
func (c *Client) Seal(s Session) (string, error) {
	data, err := json.Marshal(&s)
	if err != nil {
		return "", err
	}

	gcm, err := c.newGCM()
	if err != nil {
		return "", fmt.Errorf("new gcm: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	out := gcm.Seal(nonce, nonce, data, nil)

	return base64.RawURLEncoding.EncodeToString(out), nil
}

// Open reverses Seal, decodes, decrypts, decodes and rejects expired sessions
// and empty tenants
func (c *Client) Open(encoded string) (*Session, error) {
	var s Session
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode session: %w", err)
	}

	gcm, err := c.newGCM()
	if err != nil {
		return nil, fmt.Errorf("new gcm: %w", err)
	}

	if len(raw) < gcm.NonceSize()+16 {
		return nil, fmt.Errorf("invalid session")
	}

	nonce := raw[:gcm.NonceSize()]
	ct := raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, fmt.Errorf("open gcm: %w", err)
	}

	err = json.Unmarshal(plain, &s)
	if err != nil {
		return nil, fmt.Errorf("json decode: %w", err)
	}

	if time.Now().After(s.Expiry) {
		return nil, fmt.Errorf("session expired")
	}

	if s.TenantID == "" {
		return nil, fmt.Errorf("missing tenant")
	}

	return &s, nil

}

type stateData struct {
	State    string    `json:"state"`
	Nonce    string    `json:"nonce"`
	Verifier string    `json:"verifier"`
	Expiry   time.Time `json:"expiry"` // +5min
}

// AuthURL for oauth2 client
func (c *Client) AuthURL() (AuthRequest, error) {
	verifier := oauth2.GenerateVerifier()

	stateBytes := make([]byte, 32)
	if _, err := rand.Read(stateBytes); err != nil {
		return AuthRequest{}, fmt.Errorf("state: %w", err)
	}
	state := base64.RawURLEncoding.EncodeToString(stateBytes)

	nonceBytes := make([]byte, 32)
	if _, err := rand.Read(nonceBytes); err != nil {
		return AuthRequest{}, fmt.Errorf("nonce: %w", err)
	}
	nonce := base64.RawURLEncoding.EncodeToString(nonceBytes)
	url := c.oauth.AuthCodeURL(state, oauth2.AccessTypeOffline, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier))

	return AuthRequest{URL: url, State: state, Nonce: nonce, Verifier: verifier}, nil
}

func (c *Client) newGCM() (cipher.AEAD, error) {
	block, err := aes.NewCipher(c.cfg.SessionKey)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new gcm cipher: %w", err)
	}
	return gcm, nil
}

// SealState seals the auth request state
func (c *Client) SealState(a AuthRequest) (string, error) {
	return c.sealState(a, time.Now().Add(5*time.Minute))
}

func (c *Client) sealState(a AuthRequest, expiry time.Time) (string, error) {
	sd := stateData{
		State:    a.State,
		Nonce:    a.Nonce,
		Verifier: a.Verifier,
		Expiry:   expiry,
	}

	data, err := json.Marshal(sd)
	if err != nil {
		return "", err
	}

	gcm, err := c.newGCM()
	if err != nil {
		return "", fmt.Errorf("new gcm: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	out := gcm.Seal(nonce, nonce, data, nil)

	return base64.RawURLEncoding.EncodeToString(out), nil
}

// OpenState decrypts the auth request state
func (c *Client) OpenState(encoded string) (*AuthRequest, error) {
	var s stateData
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode state: %w", err)
	}

	gcm, err := c.newGCM()
	if err != nil {
		return nil, fmt.Errorf("new gcm: %w", err)
	}

	if len(raw) < gcm.NonceSize()+16 {
		return nil, fmt.Errorf("invalid state")
	}

	nonce := raw[:gcm.NonceSize()]
	ct := raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, fmt.Errorf("open state: %w", err)
	}

	err = json.Unmarshal(plain, &s)
	if err != nil {
		return nil, fmt.Errorf("json decode: %w", err)
	}

	if time.Now().After(s.Expiry) {
		return nil, fmt.Errorf("state expired")
	}

	return &AuthRequest{
		State:    s.State,
		Nonce:    s.Nonce,
		Verifier: s.Verifier,
	}, nil
}

// Exchange swaps code for tokens, verifies the ID token, checks nonce,
// and builds the session, verifier/nonce come from OpenState
func (c *Client) Exchange(ctx context.Context, code, verifier, storedNonce string) (*Session, error) {
	token, err := c.oauth.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return nil, fmt.Errorf("token exchange: %w", err)
	}

	raw, ok := token.Extra("id_token").(string)
	if !ok || raw == "" {
		return nil, fmt.Errorf("token not okay")
	}

	idToken, err := c.verifier.Verify(ctx, raw)
	if err != nil {
		return nil, fmt.Errorf("unable to verify token: %w", err)
	}
	var claims Claims
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("decode claims: %w", err)
	}

	if storedNonce == "" || claims.Nonce != storedNonce {
		return nil, fmt.Errorf("nonce mismatch")
	}

	tenant := claims.Email
	if len(claims.Groups) > 0 {
		tenant = claims.Groups[0]
	}
	if tenant == "" {
		return nil, fmt.Errorf("no tenant: empty groups and email")
	}

	return &Session{
		Sub:          claims.Sub,
		Email:        claims.Email,
		Groups:       claims.Groups,
		TenantID:     tenant,
		Expiry:       idToken.Expiry,
		RefreshToken: token.RefreshToken,
	}, nil
}
