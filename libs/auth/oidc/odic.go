package oidc

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// Config odic config struct
type Config struct {
	IssuerURL    string   // OIDC_ISSUER_URL, e.g. https://id.example.com
	ClientID     string   // OIDC_CLIENT_ID      (from Pocket-ID client setup)
	ClientSecret string   // OIDC_CLIENT_SECRET  (confidential client)
	RedirectURL  string   // OIDC_REDIRECT_URL, e.g. https://rss.example.com/auth/callback
	Scopes       []string // default ["openid","email","profile","groups"]
	SessionKey   [32]byte // AUTH_SESSION_KEY (base64 32B) — AES-GCM for the cookie
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
}

// Session data required for a session
type Session struct { // encrypted JSON in __Host-session cookie
	Sub, Email, TenantID string
	Groups               []string
	Expiry               time.Time // ID-token expiry
	RefreshToken         string    // encrypted alongside, for silent refresh
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
	var zeroKey [32]byte
	if cfg.SessionKey == zeroKey {
		return nil, fmt.Errorf("oidc: SessionKey is required")
	}
	if len(cfg.Scopes) == 0 {
		cfg.Scopes = []string{"openid", "email", "profile", "groups"}
	}

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

// LoginHandler shared login handler
func (c *Client) LoginHandler() http.Handler { // GET /auth/login
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not implemented", http.StatusNotImplemented)
	})
}

// CallbackHandler shared callback handler
func (c *Client) CallbackHandler() http.Handler { // GET /auth/callback
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not implemented", http.StatusNotImplemented)
	})
}

// LogoutHandler shared logout handler
func (c *Client) LogoutHandler() http.Handler { // GET /auth/logout
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not implemented", http.StatusNotImplemented)
	})
}

// SessionMiddleware shared session middleware
func (c *Client) SessionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
	})
}
