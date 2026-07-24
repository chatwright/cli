package server

import (
	"net"
	"net/http"
	"net/url"
	"strings"
)

// studioOrigin and studioLocalOrigin are the two origins the CORS/PNA layer
// allows out of the box: the hosted Studio web app, and the local-dev
// convention of serving it from a *.localhost host (browsers resolve any
// "<label>.localhost" name to 127.0.0.1 with no DNS lookup, per RFC 6761 —
// "chatwright.localhost" specifically so a local Studio build's origin
// reads as "chatwright", not a bare "localhost" shared by every other local
// dev server on the machine). Both are exact-match defaults; isLocalFamilyOrigin
// below additionally allows any port on either "localhost" itself or any
// "*.localhost" host (including studioLocalOrigin's own host at a port other
// than 80), and any loopback IP literal — the general "a local dev server on
// this machine" case the original brief also called for.
const (
	studioOrigin      = "https://chatwright.dev"
	studioLocalOrigin = "http://chatwright.localhost"
)

// defaultAllowedOrigins are the CORS/PNA allowlist's built-in entries,
// always present regardless of Config.AllowedOrigins.
var defaultAllowedOrigins = []string{studioOrigin, studioLocalOrigin}

// originAllowlist decides which request Origins receive CORS/PNA headers:
// an exact match against defaultAllowedOrigins plus any operator-configured
// extra origins (--allow-origin / CHATWRIGHT_SERVER_ALLOW_ORIGIN), or a
// generic "this is a local dev server on the same machine" pattern match
// (see isLocalFamilyOrigin) — that pattern match is what makes "any port"
// on chatwright.localhost (and on plain localhost) work without an operator
// having to enumerate every port.
type originAllowlist struct {
	exact map[string]struct{}
}

// newOriginAllowlist builds an allowlist from defaultAllowedOrigins plus
// extra (blank entries are ignored, so a caller can pass an unfiltered
// flag/env split without special-casing "no extra origins configured").
func newOriginAllowlist(extra []string) *originAllowlist {
	a := &originAllowlist{exact: make(map[string]struct{}, len(defaultAllowedOrigins)+len(extra))}
	for _, o := range defaultAllowedOrigins {
		a.exact[o] = struct{}{}
	}
	for _, o := range extra {
		if o = strings.TrimSpace(o); o != "" {
			a.exact[o] = struct{}{}
		}
	}
	return a
}

func (a *originAllowlist) allows(origin string) bool {
	if origin == "" {
		return false
	}
	if _, ok := a.exact[origin]; ok {
		return true
	}
	return isLocalFamilyOrigin(origin)
}

// isLocalFamilyOrigin reports whether origin is some local dev server on
// this machine: any http/https origin whose host is exactly "localhost", a
// "*.localhost" name (including studioLocalOrigin's own "chatwright.localhost"
// at any port), or a loopback IP literal (127.0.0.1, [::1], ...).
func isLocalFamilyOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	host := u.Hostname()
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	return net.ParseIP(host).IsLoopback()
}

// withCORS wraps next so that every response — including a route that does
// not match any handler — carries CORS and Private Network Access headers
// for an origin allowlist allows, and every OPTIONS request is answered as
// a preflight (204, no body) without reaching next. This is what lets a page
// served from https://chatwright.dev (or http://chatwright.localhost) call
// http://127.0.0.1:4319 at all: without an explicit
// Access-Control-Allow-Private-Network response, Chromium's PNA check blocks
// the request before it ever reaches next, and without
// Access-Control-Allow-Origin the browser discards the response even when
// the request itself succeeded.
//
// When the Studio UI is served same-origin from this same server (see
// ui.go's --ui-dir seam), the browser never sends a cross-origin request in
// the first place, so none of this applies — that is intended, not a gap.
func withCORS(allowlist *originAllowlist, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if allowlist.allows(origin) {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Vary", "Origin")
			h.Set("Access-Control-Allow-Private-Network", "true")
			h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			if reqHeaders := r.Header.Get("Access-Control-Request-Headers"); reqHeaders != "" {
				h.Set("Access-Control-Allow-Headers", reqHeaders)
			} else {
				h.Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			}
		}

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
