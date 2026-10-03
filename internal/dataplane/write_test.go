package dataplane_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/dataplane"
)

func TestPutRequiresIfMatch(t *testing.T) {
	h := newTestHandler(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/orgs/acme/projects/main/posts/hello",
		strings.NewReader(`{"title":"x","body":"y"}`))
	req.Header.Set("Authorization", "Bearer "+testKey)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusPreconditionRequired {
		t.Fatalf("code = %d, want 428", rec.Code)
	}
}

func TestPutRejectsStaleIfMatch(t *testing.T) {
	e := newEnv(true)
	h := e.handler()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/orgs/acme/projects/main/posts/hello",
		strings.NewReader(`{"title":"x","body":"y"}`))
	req.Header.Set("Authorization", "Bearer "+testKey)
	req.Header.Set("If-Match", `W/"2"`)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusPreconditionFailed {
		t.Fatalf("code = %d, want 412", rec.Code)
	}
	post, err := e.posts.BySlug(t.Context(), e.projectID, "hello")
	if err != nil {
		t.Fatalf("post vanished: %v", err)
	}
	if post.Title != "Hello" || post.Version != 3 {
		t.Fatalf("stale If-Match overwrote the post: %+v", post)
	}
}

func TestPutAcceptsCurrentIfMatch(t *testing.T) {
	e := newEnv(true)
	h := e.handler()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/orgs/acme/projects/main/posts/hello",
		strings.NewReader(`{"title":"x","body":"y"}`))
	req.Header.Set("Authorization", "Bearer "+testKey)
	req.Header.Set("If-Match", `W/"3"`)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if got, want := rec.Header().Get("ETag"), `W/"4"`; got != want {
		t.Fatalf("ETag = %q, want %q", got, want)
	}
}

// SECURITY: version 0 is the store's "skip the check" sentinel and must never be reachable from a header.
func TestPutRejectsUnparseableIfMatch(t *testing.T) {
	for _, tag := range []string{`W/"0"`, `*`, `W/"abc"`, `""`} {
		t.Run(tag, func(t *testing.T) {
			e := newEnv(true)
			h := e.handler()
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPut, "/api/v1/orgs/acme/projects/main/posts/hello",
				strings.NewReader(`{"title":"x","body":"y"}`))
			req.Header.Set("Authorization", "Bearer "+testKey)
			req.Header.Set("If-Match", tag)
			h.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("code = %d, want 400", rec.Code)
			}
			post, err := e.posts.BySlug(t.Context(), e.projectID, "hello")
			if err != nil || post.Title != "Hello" {
				t.Fatalf("the post was written despite a malformed If-Match: %+v", post)
			}
		})
	}
}

func TestPostIsIdempotentPerKey(t *testing.T) {
	e := newEnv(true)
	h := e.handler()
	body := `{"title":"New","slug":"new","body":"b"}`
	do := func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/orgs/acme/projects/main/posts", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+testKey)
		req.Header.Set("Idempotency-Key", "abc-123")
		h.ServeHTTP(rec, req)
		return rec
	}
	first, second := do(), do()

	if first.Code != http.StatusCreated {
		t.Fatalf("first = %d, want 201: %s", first.Code, first.Body.String())
	}
	if second.Code != http.StatusCreated {
		t.Fatalf("replay = %d, want 201", second.Code)
	}
	if first.Body.String() != second.Body.String() {
		t.Fatal("replay created a second post; the key was not honoured")
	}
	if e.posts.len() != 2 {
		t.Fatalf("store holds %d posts, want 2 (the seed plus one)", e.posts.len())
	}
}

func TestPostRejectsAReusedKeyWithADifferentBody(t *testing.T) {
	h := newTestHandler(t)
	send := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/orgs/acme/projects/main/posts", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+testKey)
		req.Header.Set("Idempotency-Key", "abc-123")
		h.ServeHTTP(rec, req)
		return rec
	}
	if got := send(`{"title":"New","slug":"new","body":"b"}`).Code; got != http.StatusCreated {
		t.Fatalf("first = %d, want 201", got)
	}
	if got := send(`{"title":"Other","slug":"other","body":"b"}`).Code; got != http.StatusConflict {
		t.Fatalf("differing body under the same key = %d, want 409", got)
	}
}

// SECURITY: an idempotency record belongs to one project, so a replay against another must create there.
func TestIdempotencyRecordsAreProjectScoped(t *testing.T) {
	e := newEnv(true)
	h := e.handler()
	body := `{"title":"New","slug":"new","body":"b"}`
	header := http.Header{"Idempotency-Key": {"abc-123"}}

	first := send(t, h, http.MethodPost, "/api/v1/orgs/acme/projects/main/posts", body, header)
	if first.Code != http.StatusCreated {
		t.Fatalf("first = %d, want 201: %s", first.Code, first.Body.String())
	}
	second := send(t, h, http.MethodPost, "/api/v1/orgs/acme/projects/other/posts", body, header)
	if second.Code != http.StatusConflict {
		t.Fatalf("other project = %d, want 409 from the slug clash, not a cross-project replay", second.Code)
	}
	if first.Body.String() == second.Body.String() {
		t.Fatal("the other project was served the first project's stored response")
	}
}

func TestDeleteRequiresIfMatchAndAdminScope(t *testing.T) {
	t.Run("missing If-Match", func(t *testing.T) {
		h := newTestHandler(t)
		rec := send(t, h, http.MethodDelete, "/api/v1/orgs/acme/projects/main/posts/hello", "", nil)
		if rec.Code != http.StatusPreconditionRequired {
			t.Fatalf("code = %d, want 428", rec.Code)
		}
	})
	t.Run("stale If-Match", func(t *testing.T) {
		e := newEnv(true)
		rec := send(t, e.handler(), http.MethodDelete, "/api/v1/orgs/acme/projects/main/posts/hello", "",
			http.Header{"If-Match": {`W/"2"`}})
		if rec.Code != http.StatusPreconditionFailed {
			t.Fatalf("code = %d, want 412", rec.Code)
		}
		if e.posts.len() != 1 {
			t.Fatal("a stale If-Match deleted the post")
		}
	})
	t.Run("write scope is not admin scope", func(t *testing.T) {
		e := newEnv(true)
		e.authz.scopes = []string{"posts:read", "posts:write"}
		rec := send(t, e.handler(), http.MethodDelete, "/api/v1/orgs/acme/projects/main/posts/hello", "",
			http.Header{"If-Match": {`W/"3"`}})
		if rec.Code != http.StatusNotFound {
			t.Fatalf("code = %d, want 404", rec.Code)
		}
		if e.posts.len() != 1 {
			t.Fatal("a key without posts:admin deleted the post")
		}
	})
	t.Run("current If-Match with admin scope", func(t *testing.T) {
		e := newEnv(true)
		rec := send(t, e.handler(), http.MethodDelete, "/api/v1/orgs/acme/projects/main/posts/hello", "",
			http.Header{"If-Match": {`W/"3"`}})
		if rec.Code != http.StatusNoContent {
			t.Fatalf("code = %d, want 204", rec.Code)
		}
		if e.posts.len() != 0 {
			t.Fatal("the post survived a valid delete")
		}
	})
}

func TestPatchLeavesAbsentFieldsAlone(t *testing.T) {
	e := newEnv(true)
	rec := send(t, e.handler(), http.MethodPatch, "/api/v1/orgs/acme/projects/main/posts/hello",
		`{"title":"Renamed"}`, http.Header{"If-Match": {`W/"3"`}})
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	post, err := e.posts.BySlug(t.Context(), e.projectID, "hello")
	if err != nil {
		t.Fatalf("post vanished: %v", err)
	}
	if post.Title != "Renamed" || post.Body != "body" {
		t.Fatalf("patch clobbered an absent field: %+v", post)
	}
}

// SECURITY: a key narrowed to one category must not move a post into a category it has no grant on.
func TestPatchIntoAnotherCategoryIsAuthorizedAgainstTheDestination(t *testing.T) {
	e := newEnv(true)
	e.authz.resources = []uuid.UUID{e.categoryID}
	rec := send(t, e.handler(), http.MethodPatch, "/api/v1/orgs/acme/projects/main/posts/hello",
		`{"categoryId":"`+uuid.New().String()+`"}`, http.Header{"If-Match": {`W/"3"`}})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("code = %d, want 404", rec.Code)
	}
	post, err := e.posts.BySlug(t.Context(), e.projectID, "hello")
	if err != nil || post.CategoryID != e.categoryID {
		t.Fatalf("the post moved into an unauthorized category: %+v", post)
	}
}

func send(t *testing.T, h http.Handler, method, path, body string, header http.Header) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testKey)
	for k, vs := range header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	h.ServeHTTP(rec, req)
	return rec
}

// SECURITY: the stores narrow the version to int32, so an out-of-range If-Match must be rejected at the surface.
func TestPutRejectsAnOutOfRangeIfMatch(t *testing.T) {
	e := newEnv(true)
	rec := send(t, e.handler(), http.MethodPut, "/api/v1/orgs/acme/projects/main/posts/hello",
		`{"title":"x","body":"y"}`, http.Header{"If-Match": {`W/"4294967299"`}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400", rec.Code)
	}
	post, err := e.posts.BySlug(t.Context(), e.projectID, "hello")
	if err != nil || post.Title != "Hello" {
		t.Fatalf("an out-of-range If-Match wrote the post: %+v", post)
	}
}

// TestConcurrentCreatesUnderOneKeyCreateOnlyOnePost is the reservation guard for a retry arriving mid-Create.
func TestConcurrentCreatesUnderOneKeyCreateOnlyOnePost(t *testing.T) {
	e := newEnv(true)
	entered, release := make(chan struct{}), make(chan struct{})
	// NOTE: only the first create blocks, or a regression would deadlock here instead of failing.
	var held atomic.Bool
	e.posts.beforeCreate = func() {
		if held.CompareAndSwap(false, true) {
			close(entered)
			<-release
		}
	}
	h := e.handler()
	body := `{"title":"New","slug":"new","body":"b"}`
	header := http.Header{"Idempotency-Key": {"abc-123"}}

	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- send(t, h, http.MethodPost, "/api/v1/orgs/acme/projects/main/posts", body, header)
	}()

	<-entered
	second := send(t, h, http.MethodPost, "/api/v1/orgs/acme/projects/main/posts", body, header)
	close(release)
	first := <-done

	if first.Code != http.StatusCreated {
		t.Fatalf("first = %d, want 201: %s", first.Code, first.Body.String())
	}
	if second.Code != http.StatusConflict {
		t.Fatalf("concurrent duplicate = %d, want 409: %s", second.Code, second.Body.String())
	}
	if e.posts.len() != 2 {
		t.Fatalf("store holds %d posts, want 2 (the seed plus one); the key was created twice", e.posts.len())
	}
}

// TestAFailedCreateLeavesTheKeyRetryable keeps a failed create from pinning its key for the whole TTL.
func TestAFailedCreateLeavesTheKeyRetryable(t *testing.T) {
	e := newEnv(true)
	e.posts.createErr = errors.New("store is unreachable")
	h := e.handler()
	body := `{"title":"New","slug":"new","body":"b"}`
	header := http.Header{"Idempotency-Key": {"abc-123"}}

	first := send(t, h, http.MethodPost, "/api/v1/orgs/acme/projects/main/posts", body, header)
	if first.Code != http.StatusInternalServerError {
		t.Fatalf("first = %d, want 500: %s", first.Code, first.Body.String())
	}
	second := send(t, h, http.MethodPost, "/api/v1/orgs/acme/projects/main/posts", body, header)
	if second.Code != http.StatusCreated {
		t.Fatalf("retry after a failed create = %d, want 201: %s", second.Code, second.Body.String())
	}
}

const writeBase = "/api/v1/orgs/acme/projects/main/posts"

// SECURITY: R9 — every slug-addressed write must reach its authorization check before If-Match or the body can shape the reply.
func TestWriteRoutesMaskAuthorizationBeforeRequestShape(t *testing.T) {
	routes := []struct{ name, method, suffix string }{
		{"replace", http.MethodPut, ""},
		{"patch", http.MethodPatch, ""},
		{"delete", http.MethodDelete, ""},
		{"publish", http.MethodPost, "/publish"},
		{"unpublish", http.MethodPost, "/unpublish"},
	}
	shapes := []struct {
		name, body string
		header     http.Header
	}{
		{"no if-match", `{"title":"x","body":"y"}`, nil},
		{"stale if-match", `{"title":"x","body":"y"}`, http.Header{"If-Match": {`W/"2"`}}},
		{"unparseable if-match", `{"title":"x","body":"y"}`, http.Header{"If-Match": {`W/"abc"`}}},
		{"malformed body", `{`, http.Header{"If-Match": {`W/"3"`}}},
	}

	for _, route := range routes {
		for _, shape := range shapes {
			t.Run(route.name+"/"+shape.name, func(t *testing.T) {
				denied := newEnv(true)
				denied.authz.scopes = []string{dataplane.ScopePostsRead}
				got := send(t, denied.handler(), route.method, writeBase+"/hello"+route.suffix, shape.body, shape.header)

				absent := newEnv(true)
				want := send(t, absent.handler(), route.method, writeBase+"/nosuchslug"+route.suffix, shape.body, shape.header)

				if want.Code != http.StatusNotFound {
					t.Fatalf("a missing slug answered %d, want 404: %s", want.Code, want.Body.String())
				}
				if got.Code != want.Code {
					t.Fatalf("wrong scope = %d, missing slug = %d: the status tells an unauthorized caller the slug exists",
						got.Code, want.Code)
				}
				if got.Body.String() != want.Body.String() {
					t.Fatalf("wrong scope body = %q, missing slug body = %q: the body tells an unauthorized caller the slug exists",
						got.Body.String(), want.Body.String())
				}
				if denied.posts.len() != 1 {
					t.Fatal("an unauthorized write reached the store")
				}
			})
		}
	}
}

// SECURITY: create authorizes against a body field, so an unparseable body must still answer the masked not-found.
func TestCreateMasksAuthorizationBeforeBodyShape(t *testing.T) {
	bodies := []struct{ name, body string }{
		{"well formed", `{"title":"New","slug":"new","body":"b"}`},
		{"malformed json", `{`},
		{"unparseable categoryId", `{"title":"New","slug":"new","categoryId":"not-a-uuid"}`},
		{"oversized", strings.Repeat("a", 1<<20+1)},
	}
	for _, tt := range bodies {
		t.Run(tt.name, func(t *testing.T) {
			denied := newEnv(true)
			denied.authz.scopes = []string{dataplane.ScopePostsRead}
			got := send(t, denied.handler(), http.MethodPost, writeBase, tt.body, nil)

			absent := newEnv(true)
			want := send(t, absent.handler(), http.MethodPost,
				"/api/v1/orgs/acme/projects/nosuchproject/posts", tt.body, nil)

			if want.Code != http.StatusNotFound {
				t.Fatalf("an unresolvable project answered %d, want 404: %s", want.Code, want.Body.String())
			}
			if got.Code != want.Code || got.Body.String() != want.Body.String() {
				t.Fatalf("wrong scope = %d %q, unresolvable project = %d %q: the reply tells an unauthorized caller the project exists",
					got.Code, got.Body.String(), want.Code, want.Body.String())
			}
			if denied.posts.len() != 1 {
				t.Fatal("an unauthorized create reached the store")
			}
		})
	}
}

// SECURITY: a malformed body is masked for a resource-narrowed key, or the 400 proves a project-wide grant.
func TestCreateMasksAMalformedBodyForAResourceNarrowedKey(t *testing.T) {
	e := newEnv(true)
	e.authz.resources = []uuid.UUID{e.categoryID}
	rec := send(t, e.handler(), http.MethodPost, writeBase, `{`, nil)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("code = %d, want 404: %s", rec.Code, rec.Body.String())
	}
}

// TestAuthorizedCallersStillSeeRequestShapeErrors checks masking did not swallow 428, 412 and 400 for an authorized caller.
func TestAuthorizedCallersStillSeeRequestShapeErrors(t *testing.T) {
	tests := []struct {
		name, method, path, body string
		header                   http.Header
		want                     int
	}{
		{"delete without if-match", http.MethodDelete, writeBase + "/hello", "", nil, http.StatusPreconditionRequired},
		{"publish without if-match", http.MethodPost, writeBase + "/hello/publish", "", nil, http.StatusPreconditionRequired},
		{"put without if-match", http.MethodPut, writeBase + "/hello", `{"title":"x"}`, nil, http.StatusPreconditionRequired},
		{"put with stale if-match", http.MethodPut, writeBase + "/hello", `{"title":"x"}`,
			http.Header{"If-Match": {`W/"2"`}}, http.StatusPreconditionFailed},
		{"put with malformed body", http.MethodPut, writeBase + "/hello", `{`,
			http.Header{"If-Match": {`W/"3"`}}, http.StatusBadRequest},
		{"create with malformed body", http.MethodPost, writeBase, `{`, nil, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := send(t, newEnv(true).handler(), tt.method, tt.path, tt.body, tt.header)
			if rec.Code != tt.want {
				t.Fatalf("code = %d, want %d: %s", rec.Code, tt.want, rec.Body.String())
			}
		})
	}
}
