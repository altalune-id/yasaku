package dataplane_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"altalune.id/yasaku/internal/dataplane"
)

func publishRequest(t *testing.T, h http.Handler, path, key, ifMatch string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, nil)
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	if ifMatch != "" {
		req.Header.Set("If-Match", ifMatch)
	}
	h.ServeHTTP(rec, req)
	return rec
}

const publishURL = "/api/v1/orgs/acme/projects/main/posts/hello/publish"

const unpublishURL = "/api/v1/orgs/acme/projects/main/posts/hello/unpublish"

func TestPublishRequiresIfMatch(t *testing.T) {
	e := newEnv(false)
	rec := publishRequest(t, e.handler(), publishURL, testKey, "")

	if rec.Code != http.StatusPreconditionRequired {
		t.Fatalf("code = %d, want 428: %s", rec.Code, rec.Body.String())
	}
	post, err := e.posts.BySlug(t.Context(), e.projectID, "hello")
	if err != nil {
		t.Fatalf("post vanished: %v", err)
	}
	if post.Published {
		t.Fatal("a publish without If-Match changed the post's state")
	}
}

func TestPublishRejectsStaleIfMatch(t *testing.T) {
	e := newEnv(false)
	rec := publishRequest(t, e.handler(), publishURL, testKey, `W/"2"`)

	if rec.Code != http.StatusPreconditionFailed {
		t.Fatalf("code = %d, want 412: %s", rec.Code, rec.Body.String())
	}
	post, err := e.posts.BySlug(t.Context(), e.projectID, "hello")
	if err != nil {
		t.Fatalf("post vanished: %v", err)
	}
	if post.Published || post.Version != 3 {
		t.Fatalf("stale If-Match published the post anyway: %+v", post)
	}
}

// SECURITY: a malformed If-Match must not degrade into an unconditional transition.
func TestPublishRejectsUnparseableIfMatch(t *testing.T) {
	for _, tag := range []string{`W/"0"`, `*`, `W/"abc"`, `""`, `W/"2147483648"`} {
		t.Run(tag, func(t *testing.T) {
			e := newEnv(false)
			rec := publishRequest(t, e.handler(), publishURL, testKey, tag)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("code = %d, want 400: %s", rec.Code, rec.Body.String())
			}
			post, err := e.posts.BySlug(t.Context(), e.projectID, "hello")
			if err != nil {
				t.Fatalf("post vanished: %v", err)
			}
			if post.Published {
				t.Fatalf("%q published the post: %+v", tag, post)
			}
		})
	}
}

func TestPublishAcceptsCurrentIfMatch(t *testing.T) {
	e := newEnv(false)
	rec := publishRequest(t, e.handler(), publishURL, testKey, `W/"3"`)

	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if got, want := rec.Header().Get("ETag"), `W/"4"`; got != want {
		t.Fatalf("ETag = %q, want %q", got, want)
	}
	if body := rec.Body.String(); !strings.Contains(body, `"status":"published"`) {
		t.Fatalf("body = %s, want status published", body)
	}
	post, err := e.posts.BySlug(t.Context(), e.projectID, "hello")
	if err != nil {
		t.Fatalf("post vanished: %v", err)
	}
	if !post.Published {
		t.Fatalf("publish left the post in draft: %+v", post)
	}
}

func TestUnpublishAcceptsCurrentIfMatch(t *testing.T) {
	e := newEnv(true)
	rec := publishRequest(t, e.handler(), unpublishURL, testKey, `W/"3"`)

	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	post, err := e.posts.BySlug(t.Context(), e.projectID, "hello")
	if err != nil {
		t.Fatalf("post vanished: %v", err)
	}
	if post.Published {
		t.Fatalf("unpublish left the post published: %+v", post)
	}
}

// SECURITY: a key without posts:write gets the same masked not-found as a missing post.
func TestPublishWithoutScopeIsMaskedNotFound(t *testing.T) {
	for _, path := range []string{publishURL, unpublishURL} {
		t.Run(path, func(t *testing.T) {
			e := newEnv(false)
			e.authz.scopes = []string{dataplane.ScopePostsRead}
			rec := publishRequest(t, e.handler(), path, testKey, `W/"3"`)

			if rec.Code != http.StatusNotFound {
				t.Fatalf("code = %d, want 404: %s", rec.Code, rec.Body.String())
			}
			if body := rec.Body.String(); !strings.Contains(body, `"code":"not_found"`) {
				t.Fatalf("body = %s, want the masked not-found envelope", body)
			}
			post, err := e.posts.BySlug(t.Context(), e.projectID, "hello")
			if err != nil {
				t.Fatalf("post vanished: %v", err)
			}
			if post.Published || post.Version != 3 {
				t.Fatalf("an unscoped key changed the post: %+v", post)
			}
		})
	}
}

// SECURITY: publication is the switch that makes a post anonymously readable, so an uncredentialed caller must never reach it even while PublicReads is on.
func TestPublishWithoutCredentialIsUnauthorized(t *testing.T) {
	e := newEnv(false)
	e.caps.PublicReads = true
	rec := publishRequest(t, e.handler(), publishURL, "", `W/"3"`)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("code = %d, want 401: %s", rec.Code, rec.Body.String())
	}
	post, err := e.posts.BySlug(t.Context(), e.projectID, "hello")
	if err != nil {
		t.Fatalf("post vanished: %v", err)
	}
	if post.Published {
		t.Fatal("an anonymous request published the post")
	}
}

func TestPublishUnknownSlugIsNotFound(t *testing.T) {
	h := newTestHandler(t)
	rec := publishRequest(t, h, "/api/v1/orgs/acme/projects/main/posts/nosuchpost/publish", testKey, `W/"1"`)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("code = %d, want 404: %s", rec.Code, rec.Body.String())
	}
}

// TestPublishMakesAPostAnonymouslyReadable proves an API key alone can move a post to publicly readable.
func TestPublishMakesAPostAnonymouslyReadable(t *testing.T) {
	e := newEnv(false)
	e.caps.PublicReads = true
	h := e.handler()

	const readURL = "/api/v1/orgs/acme/projects/main/posts/hello"

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, readURL, nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("anonymous read of a draft = %d, want 404", rec.Code)
	}

	if pub := publishRequest(t, h, publishURL, testKey, `W/"3"`); pub.Code != http.StatusOK {
		t.Fatalf("publish = %d, want 200: %s", pub.Code, pub.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, readURL, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("anonymous read after publish = %d, want 200: %s", rec.Code, rec.Body.String())
	}
}
