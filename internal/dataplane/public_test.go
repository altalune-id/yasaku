package dataplane_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublicReadMatrix(t *testing.T) {
	tests := []struct {
		name       string
		publicOn   bool
		published  bool
		credential string
		want       int
	}{
		{"published, flag on, anonymous", true, true, "", http.StatusOK},
		{"published, flag off, anonymous", false, true, "", http.StatusNotFound},
		{"draft, flag on, anonymous", true, false, "", http.StatusNotFound},
		{"draft, flag on, with key", true, false, "Bearer " + testKey, http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTestHandlerWith(t, tt.publicOn, tt.published)
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/orgs/acme/projects/main/posts/hello", nil)
			if tt.credential != "" {
				req.Header.Set("Authorization", tt.credential)
			}
			h.ServeHTTP(rec, req)
			if rec.Code != tt.want {
				t.Fatalf("code = %d, want %d", rec.Code, tt.want)
			}
		})
	}
}

// SECURITY: the capability gates reads only.
func TestPublicFlagNeverOpensWrites(t *testing.T) {
	const base = "/api/v1/orgs/acme/projects/main/posts"
	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"create", http.MethodPost, base, `{"title":"New","slug":"new","body":"b"}`},
		{"replace", http.MethodPut, base + "/hello", `{"title":"x"}`},
		{"patch", http.MethodPatch, base + "/hello", `{"title":"x"}`},
		{"delete", http.MethodDelete, base + "/hello", ""},
		{"publish", http.MethodPost, base + "/hello/publish", ""},
		{"unpublish", http.MethodPost, base + "/hello/unpublish", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(true)
			e.caps.PublicReads = true
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			req.Header.Set("If-Match", `W/"3"`)
			e.handler().ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("code = %d, want 401: %s", rec.Code, rec.Body.String())
			}
			post, err := e.posts.BySlug(t.Context(), e.projectID, "hello")
			if err != nil || post.Version != 3 || e.posts.len() != 1 {
				t.Fatalf("an uncredentialed %s changed the store: %+v (len %d, err %v)", tt.method, post, e.posts.len(), err)
			}
		})
	}
}

// SECURITY: an anonymous collection read must never list drafts, even with the flag on.
func TestPublicListHidesDrafts(t *testing.T) {
	e := newEnv(false)
	e.caps.PublicReads = true
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/orgs/acme/projects/main/posts", nil)
	e.handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	if strings.Contains(rec.Body.String(), `"slug":"hello"`) {
		t.Fatalf("an anonymous list exposed a draft: %s", rec.Body.String())
	}
}

// TestPublicListIsClosedWhenTheFlagIsOff proves the collection route is gated too, not only the item route.
func TestPublicListIsClosedWhenTheFlagIsOff(t *testing.T) {
	e := newEnv(true)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/orgs/acme/projects/main/posts", nil)
	e.handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("code = %d, want 404", rec.Code)
	}
}

func newTestHandlerWith(t *testing.T, publicOn, published bool) http.Handler {
	t.Helper()
	e := newEnv(published)
	e.caps.PublicReads = publicOn
	return e.handler()
}
