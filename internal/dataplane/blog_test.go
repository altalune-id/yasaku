package dataplane_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/platform/authn"
)

func TestGetPostReturnsETagFromVersion(t *testing.T) {
	h := newTestHandler(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/orgs/acme/projects/main/posts/hello", nil)
	req.Header.Set("Authorization", "Bearer "+testKey)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	if got, want := rec.Header().Get("ETag"), `W/"3"`; got != want {
		t.Fatalf("ETag = %q, want %q", got, want)
	}
}

func TestGetPostHonoursIfNoneMatch(t *testing.T) {
	h := newTestHandler(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/orgs/acme/projects/main/posts/hello", nil)
	req.Header.Set("Authorization", "Bearer "+testKey)
	req.Header.Set("If-None-Match", `W/"3"`)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotModified {
		t.Fatalf("code = %d, want 304", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Fatal("304 carried a body")
	}
}

func TestListPostsReturnsTheCollection(t *testing.T) {
	h := newTestHandler(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/orgs/acme/projects/main/posts", nil)
	req.Header.Set("Authorization", "Bearer "+testKey)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	if got := rec.Body.String(); !strings.Contains(got, `"slug":"hello"`) {
		t.Fatalf("body = %s, want the seeded post", got)
	}
}

// TestInvalidCredentialIsAlwaysUnauthorized proves a bad key answers 401 on a live path and a bogus one alike.
func TestInvalidCredentialIsAlwaysUnauthorized(t *testing.T) {
	h := newTestHandler(t)
	for _, path := range []string{
		"/api/v1/orgs/acme/projects/main/posts/hello",
		"/api/v1/orgs/nosuchorg/projects/main/posts/hello",
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer key_wrong")
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s => %d, want 401", path, rec.Code)
		}
	}
}

// SECURITY: a key that authenticates but lacks the scope is answered exactly as a missing post is.
func TestScopeDenialIsIndistinguishableFromNotFound(t *testing.T) {
	e := newEnv(true)
	e.authz.scopes = nil
	h := e.handler()

	denied := get(t, h, "/api/v1/orgs/acme/projects/main/posts/hello")
	missing := get(t, h, "/api/v1/orgs/acme/projects/main/posts/nosuchpost")

	if denied.Code != http.StatusNotFound {
		t.Fatalf("scope denial = %d, want 404", denied.Code)
	}
	if denied.Body.String() != missing.Body.String() {
		t.Fatalf("scope denial body %q differs from not-found %q", denied.Body.String(), missing.Body.String())
	}
	if denied.Header().Get("ETag") != "" {
		t.Fatal("scope denial leaked an ETag")
	}
}

// SECURITY: a key narrowed to other categories must not read outside them, and the denial is the masked not-found.
func TestResourceNarrowedKeyIsDeniedOutsideItsCategories(t *testing.T) {
	e := newEnv(true)
	e.authz.resources = []uuid.UUID{uuid.New()}
	h := e.handler()

	rec := get(t, h, "/api/v1/orgs/acme/projects/main/posts/hello")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("code = %d, want 404", rec.Code)
	}
}

// TestUnknownScopeIsDenied proves an undeclared scope name never admits a request.
func TestUnknownScopeIsDenied(t *testing.T) {
	e := newEnv(true)
	e.authz.scopes = []string{"posts:nosuchscope"}
	h := e.handler()

	if rec := get(t, h, "/api/v1/orgs/acme/projects/main/posts/hello"); rec.Code != http.StatusNotFound {
		t.Fatalf("code = %d, want 404", rec.Code)
	}
	if authn.Valid("posts:nosuchscope") {
		t.Fatal("an undeclared scope entered the catalog")
	}
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+testKey)
	h.ServeHTTP(rec, req)
	return rec
}

// TestUnroutedRequestsKeepTheR6Envelope guards R6 on the paths no route claims.
func TestUnroutedRequestsKeepTheR6Envelope(t *testing.T) {
	const posts = "/api/v1/orgs/acme/projects/main/posts"

	tests := []struct {
		name, method, path string
		wantStatus         int
		wantCode           string
		wantAllow          string
	}{
		{"subtree root", http.MethodGet, "/api/v1/", http.StatusNotFound, "not_found", ""},
		{"typo'd collection", http.MethodGet, "/api/v1/orgs/acme/projects/main/postz", http.StatusNotFound, "not_found", ""},
		{"path below a route", http.MethodGet, posts + "/hello/nope", http.StatusNotFound, "not_found", ""},
		{"unknown method on the collection", http.MethodPatch, posts, http.StatusMethodNotAllowed, "method_not_allowed", "GET, HEAD, POST"},
		{"unknown method on the item", http.MethodPost, posts + "/hello", http.StatusMethodNotAllowed, "method_not_allowed", "GET, HEAD, PUT, PATCH, DELETE"},
		{"options on a routed path", http.MethodOptions, posts, http.StatusMethodNotAllowed, "method_not_allowed", "GET, HEAD, POST"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTestHandler(t)
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(tt.method, tt.path, nil)
			req.Header.Set("Authorization", "Bearer "+testKey)
			h.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if got := rec.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
				t.Fatalf("R6: Content-Type = %q, want application/json; charset=utf-8; body=%s", got, rec.Body.String())
			}
			var envelope struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
				t.Fatalf("R6: the body is not the declared {code,message} envelope: %s", rec.Body.String())
			}
			if envelope.Code != tt.wantCode {
				t.Fatalf("R6: code = %q, want %q; body=%s", envelope.Code, tt.wantCode, rec.Body.String())
			}
			if got := rec.Header().Get("Allow"); got != tt.wantAllow {
				t.Fatalf("Allow = %q, want %q", got, tt.wantAllow)
			}
		})
	}
}
