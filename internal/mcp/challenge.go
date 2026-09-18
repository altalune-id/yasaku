package mcp

import (
	"net/http"
	"strings"
)

const (
	challengePrefix       = "/.well-known/authalune-challenge/"
	challengeCacheControl = "no-store"
)

// ChallengeRoutes returns the outer-mux routes proving host control to the authorization server,
// or nil when no token is configured.
//
// SECURITY: the token is fixed in the registered pattern and echoed from configuration, never from
// the request path. A handler that replied with its own path segment would verify any token an
// attacker submitted, letting another tenant claim this resource URI.
//
// NOTE: authl fetches the domain root because the proof is about host control, so the resource
// path is ignored. The basePath variant is served too, for a deployment whose proxy forwards only
// the prefix; both answer the same body.
func ChallengeRoutes(basePath, token string) map[string]http.Handler {
	token = strings.TrimSpace(token)
	if token == "" || !servableChallengeToken(token) {
		return nil
	}
	body := []byte(token)
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", challengeCacheControl)
		_, _ = w.Write(body)
	})
	out := map[string]http.Handler{"GET " + challengePrefix + token: h}
	if p := strings.TrimRight(basePath, "/"); p != "" {
		out["GET "+p+challengePrefix+token] = h
	}
	return out
}

// servableChallengeToken reports whether token is a single path segment ServeMux can match literally.
func servableChallengeToken(token string) bool {
	if len(token) > 512 {
		return false
	}
	return strings.IndexFunc(token, func(r rune) bool {
		return r == '/' || r == '%' || r == '{' || r == '}' || r == '?' || r == '#' || r <= ' ' || r > '~'
	}) < 0
}
