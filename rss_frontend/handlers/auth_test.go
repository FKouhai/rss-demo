package handlers

import (
	"net/http/httptest"
	"testing"
)

func TestCookieFlags_ForwardedProto(t *testing.T) {
	plain := httptest.NewRequest("GET", "/", nil)
	if cookieFlags(plain) {
		t.Error("plain HTTP: want insecure")
	}
	proxied := httptest.NewRequest("GET", "/", nil)
	proxied.Header.Set("X-Forwarded-Proto", "https")
	if !cookieFlags(proxied) {
		t.Error("X-Forwarded-Proto: https behind Traefik: want secure")
	}
	proxiedHTTP := httptest.NewRequest("GET", "/", nil)
	proxiedHTTP.Header.Set("X-Forwarded-Proto", "http")
	if cookieFlags(proxiedHTTP) {
		t.Error("X-Forwarded-Proto: http: want insecure")
	}
}
