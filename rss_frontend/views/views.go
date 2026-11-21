// Package views holds templ page and fragment templates.
package views

import (
	"fmt"
	"net/url"
	"strings"
)

// ItemsURL builds the partial URL preserving search state.
func ItemsURL(q, feed string, page, perPage int) string {
	v := url.Values{}
	v.Set("q", q)
	v.Set("feed", feed)
	v.Set("page", fmt.Sprint(page))
	v.Set("per_page", fmt.Sprint(perPage))
	return "/partials/items?" + v.Encode()
}

// FrontURL is the full-page equivalent (for push-url / no-JS links).
func FrontURL(q, feed string, page, perPage int) string {
	v := url.Values{}
	v.Set("q", q)
	v.Set("feed", feed)
	v.Set("page", fmt.Sprint(page))
	v.Set("per_page", fmt.Sprint(perPage))
	return "/?" + v.Encode()
}

// DomainOf returns the registrable-looking host for a link.
func DomainOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	return strings.TrimPrefix(u.Host, "www.")
}
