package handlers

import (
	"net/http"

	"github.com/FKouhai/rss-demo/libs/instrumentation"
	log "github.com/FKouhai/rss-demo/libs/logger"
)

// cookieFlags reports whether the request is TLS-terminated locally.
// Secure cookies are only set on HTTPS; homelab plain-HTTP keeps login
// working without them.
func cookieFlags(r *http.Request) (secure bool) { return r.TLS != nil }

// loginHandler starts OIDC: persists the PKCE/state bundle in a
// short-lived cookie and redirects to Pocket-ID (passkeys happen there).
func (d Deps) loginHandler(w http.ResponseWriter, r *http.Request) {
	_, span := tracer.StartServer(r.Context(), "handlers.loginHandler")
	defer span.End()
	req, err := d.OIDC.AuthURL()
	if err != nil {
		instrumentation.Fail(span, err, "auth url: %v", err)
		http.Error(w, "login unavailable", http.StatusInternalServerError)
		return
	}
	sealed, err := d.OIDC.SealState(req)
	if err != nil {
		instrumentation.Fail(span, err, "seal state: %v", err)
		http.Error(w, "login unavailable", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: d.OIDC.StateCookieName(), Value: sealed, Path: "/auth/",
		MaxAge: 300, HttpOnly: true, Secure: cookieFlags(r), SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, req.URL, http.StatusFound)
}

// callbackHandler completes OIDC: validates state, exchanges the code,
// seals the session, and lands on the front page.
func (d Deps) callbackHandler(w http.ResponseWriter, r *http.Request) {
	_, span := tracer.StartServer(r.Context(), "handlers.callbackHandler")
	defer span.End()
	qs := r.URL.Query()
	if qs.Get("error") != "" {
		log.ErrorFmt("oidc provider error: %s", qs.Get("error_description"))
		http.Error(w, "login failed", http.StatusUnauthorized)
		return
	}
	stateCookie, err := r.Cookie(d.OIDC.StateCookieName())
	if err != nil {
		http.Error(w, "login expired, retry", http.StatusBadRequest)
		return
	}
	stored, err := d.OIDC.OpenState(stateCookie.Value)
	if err != nil {
		http.Error(w, "login expired, retry", http.StatusBadRequest)
		return
	}
	if qs.Get("state") == "" || qs.Get("state") != stored.State {
		http.Error(w, "state mismatch", http.StatusBadRequest)
		return
	}
	sess, err := d.OIDC.Exchange(r.Context(), qs.Get("code"), stored.Verifier, stored.Nonce)
	if err != nil {
		log.ErrorFmt("exchange: %v", err)
		http.Error(w, "login failed", http.StatusUnauthorized)
		return
	}
	sealed, err := d.OIDC.Seal(*sess)
	if err != nil {
		instrumentation.Fail(span, err, "seal session: %v", err)
		http.Error(w, "login failed", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: d.OIDC.SessionCookieName(), Value: sealed, Path: "/",
		MaxAge: 86400, HttpOnly: true, Secure: cookieFlags(r), SameSite: http.SameSiteLaxMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name: d.OIDC.StateCookieName(), Value: "", Path: "/auth/",
		MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/", http.StatusFound)
}

// logoutHandler clears the session and returns to the front page.
func (d Deps) logoutHandler(w http.ResponseWriter, r *http.Request) {
	cookieName := "__Host-session"
	if d.OIDC != nil {
		cookieName = d.OIDC.SessionCookieName()
	}
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: "", Path: "/",
		MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/", http.StatusFound)
}
