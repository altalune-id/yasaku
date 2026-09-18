package mcp_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"altalune.id/yasaku/internal/mcp"
)

func serveChallenge(t *testing.T, basePath, token, requestPath string) *httptest.ResponseRecorder {
	t.Helper()

	mux := http.NewServeMux()
	for pattern, h := range mcp.ChallengeRoutes(basePath, token) {
		mux.Handle(pattern, h)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, requestPath, nil))
	return rec
}

func TestChallengeRoutes_ServesTheConfiguredToken(t *testing.T) {
	t.Parallel()

	const token = "abc123XYZ-_.~"
	rec := serveChallenge(t, "", token, "/.well-known/authalune-challenge/"+token)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != token {
		t.Fatalf("body = %q, want %q", got, token)
	}
}

// SECURITY: a handler echoing its own path segment would verify any token an attacker submits,
// letting another tenant claim this resource URI.
func TestChallengeRoutes_RefusesAnyTokenButTheConfiguredOne(t *testing.T) {
	t.Parallel()

	rec := serveChallenge(t, "", "the-real-token", "/.well-known/authalune-challenge/attacker-token")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for a token we were never issued", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "attacker-token") {
		t.Fatal("the response echoed the request path; the proof would pass for any token")
	}
}

func TestChallengeRoutes_AnswersUnderBasePathToo(t *testing.T) {
	t.Parallel()

	const token = "t0ken"
	for _, path := range []string{
		"/.well-known/authalune-challenge/" + token,
		"/yasaku/.well-known/authalune-challenge/" + token,
	} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			rec := serveChallenge(t, "/yasaku", token, path)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			if got := rec.Body.String(); got != token {
				t.Fatalf("body = %q, want %q", got, token)
			}
		})
	}
}

func TestChallengeRoutes_NoRoutesWhenUnusable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		token string
	}{
		{"empty", ""},
		{"whitespace only", "   "},
		{"contains a path separator", "aa/bb"},
		{"contains a wildcard brace", "a{b}"},
		{"percent encoded", "a%2Fb"},
		{"contains a space", "a b"},
		{"non-ascii", "tökén"},
		{"too long", strings.Repeat("a", 513)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := mcp.ChallengeRoutes("", tt.token); got != nil {
				t.Fatalf("ChallengeRoutes(%q) = %v, want nil", tt.token, got)
			}
		})
	}
}

func TestChallengeRoutes_TrimsSurroundingWhitespace(t *testing.T) {
	t.Parallel()

	routes := mcp.ChallengeRoutes("", "  padded  ")
	if _, ok := routes["GET /.well-known/authalune-challenge/padded"]; !ok {
		t.Fatalf("routes = %v, want the trimmed token registered", routes)
	}
}

func TestChallengeRoutes_IsNotCached(t *testing.T) {
	t.Parallel()

	rec := serveChallenge(t, "", "tok", "/.well-known/authalune-challenge/tok")
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store so a rotated token is not served stale", got)
	}
}
