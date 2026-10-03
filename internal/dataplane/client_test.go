package dataplane_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"altalune.id/yasaku/internal/dataplane"
)

func TestClientListPostsSendsBearerKey(t *testing.T) {
	var gotAuth, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath = r.Header.Get("Authorization"), r.URL.Path
		_, _ = w.Write([]byte(`{"posts":[{"slug":"hello","title":"Hello","status":"published","version":3}]}`))
	}))
	defer srv.Close()

	posts, err := dataplane.NewClient(srv.URL, "key_test").ListPosts(context.Background(), "acme", "main")
	if err != nil {
		t.Fatalf("ListPosts: %v", err)
	}
	if gotAuth != "Bearer key_test" {
		t.Errorf("Authorization = %q, want the API key as a bearer token", gotAuth)
	}
	if gotPath != "/api/v1/orgs/acme/projects/main/posts" {
		t.Errorf("path = %q", gotPath)
	}
	if len(posts) != 1 || posts[0].Slug != "hello" || posts[0].Version != 3 {
		t.Fatalf("posts = %+v", posts)
	}
}

func TestClientWriteSendsIfMatchAndIdempotencyKey(t *testing.T) {
	var gotMatch, gotIdem, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMatch = r.Header.Get("If-Match")
		gotIdem = r.Header.Get("Idempotency-Key")
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		_, _ = w.Write([]byte(`{"slug":"hello","version":8}`))
	}))
	defer srv.Close()

	title := "New"
	c := dataplane.NewClient(srv.URL, "key_test")
	post, err := c.PatchPost(context.Background(), "acme", "main", "hello",
		dataplane.PostInput{Title: &title},
		dataplane.WriteOpts{IfVersion: 7, IdempotencyKey: "idem-1"})
	if err != nil {
		t.Fatalf("PatchPost: %v", err)
	}
	if gotMatch != `W/"7"` {
		t.Errorf("If-Match = %q, want the weak validator for version 7", gotMatch)
	}
	if gotIdem != "idem-1" {
		t.Errorf("Idempotency-Key = %q", gotIdem)
	}
	var sent map[string]any
	if err := json.Unmarshal([]byte(gotBody), &sent); err != nil {
		t.Fatalf("request body is not json: %q", gotBody)
	}
	if _, present := sent["body"]; present {
		t.Errorf("a patch must omit the fields the caller did not set, got %q", gotBody)
	}
	if post.Version != 8 {
		t.Errorf("Version = %d, want 8", post.Version)
	}
}

func TestClientMapsStatusesToTypedErrors(t *testing.T) {
	tests := []struct {
		name   string
		status int
		is     func(error) bool
	}{
		{"stale If-Match", http.StatusPreconditionFailed, dataplane.IsPreconditionFailedError},
		{"missing If-Match", http.StatusPreconditionRequired, dataplane.IsPreconditionRequiredError},
		{"bad key", http.StatusUnauthorized, dataplane.IsUnauthorizedError},
		{"absent", http.StatusNotFound, dataplane.IsNotFoundError},
		{"replayed key", http.StatusConflict, dataplane.IsConflictError},
		{"malformed", http.StatusBadRequest, dataplane.IsBadRequestError},
		{"unmapped", http.StatusTeapot, dataplane.IsStatusError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(`{"code":"x","message":"x"}`))
			}))
			defer srv.Close()

			_, err := dataplane.NewClient(srv.URL, "key_test").ReplacePost(
				context.Background(), "acme", "main", "hello",
				dataplane.PostInput{}, dataplane.WriteOpts{IfVersion: 2})
			if err == nil {
				t.Fatal("want an error")
			}
			if !tt.is(err) {
				t.Fatalf("%d mapped to the wrong type: %v", tt.status, err)
			}
		})
	}
}

// TestClientPreconditionFailedIsDistinctFromNotFound pins the 412 branch a caller retries on.
func TestClientPreconditionFailedIsDistinctFromNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusPreconditionFailed)
	}))
	defer srv.Close()

	err := dataplane.NewClient(srv.URL, "key_test").DeletePost(
		context.Background(), "acme", "main", "hello", dataplane.WriteOpts{IfVersion: 1})
	if !dataplane.IsPreconditionFailedError(err) {
		t.Fatalf("want a PreconditionFailedError, got %#v", err)
	}
	if dataplane.IsNotFoundError(err) || dataplane.IsStatusError(err) {
		t.Fatal("a 412 must be its own failure mode, not a generic HTTP error")
	}
}

func TestClientDeleteAcceptsNoContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %s", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	if err := dataplane.NewClient(srv.URL, "key_test").DeletePost(
		context.Background(), "acme", "main", "hello", dataplane.WriteOpts{IfVersion: 4}); err != nil {
		t.Fatalf("DeletePost: %v", err)
	}
}

func TestClientEscapesPathSegments(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	if _, err := dataplane.NewClient(srv.URL, "k").GetPost(
		context.Background(), "acme", "main", "a/../b"); err != nil {
		t.Fatalf("GetPost: %v", err)
	}
	if gotPath != "/api/v1/orgs/acme/projects/main/posts/a%2F..%2Fb" {
		t.Errorf("a slug must not escape its path segment, got %q", gotPath)
	}
}

func TestClientPublishPostPostsToTheSubresource(t *testing.T) {
	for _, tc := range []struct {
		name string
		call func(*dataplane.Client) (dataplane.Post, error)
		path string
	}{
		{"publish", func(c *dataplane.Client) (dataplane.Post, error) {
			return c.PublishPost(context.Background(), "acme", "main", "hello", dataplane.WriteOpts{IfVersion: 3})
		}, "/api/v1/orgs/acme/projects/main/posts/hello/publish"},
		{"unpublish", func(c *dataplane.Client) (dataplane.Post, error) {
			return c.UnpublishPost(context.Background(), "acme", "main", "hello", dataplane.WriteOpts{IfVersion: 3})
		}, "/api/v1/orgs/acme/projects/main/posts/hello/unpublish"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gotMethod, gotPath, gotMatch string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod, gotPath, gotMatch = r.Method, r.URL.Path, r.Header.Get("If-Match")
				_, _ = w.Write([]byte(`{"slug":"hello","status":"published","version":4}`))
			}))
			defer srv.Close()

			post, err := tc.call(dataplane.NewClient(srv.URL, "key_test"))
			if err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if gotMethod != http.MethodPost || gotPath != tc.path {
				t.Errorf("%s %s, want POST %s", gotMethod, gotPath, tc.path)
			}
			if gotMatch != `W/"3"` {
				t.Errorf("If-Match = %q, want the weak validator for version 3", gotMatch)
			}
			if post.Version != 4 {
				t.Errorf("Version = %d, want 4", post.Version)
			}
		})
	}
}
