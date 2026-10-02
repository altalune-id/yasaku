package dataplane_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/dataplane"
)

// TestUnresolvableScopeIsIndistinguishable is the anti-enumeration guard: every unresolvable scope is one answer.
func TestUnresolvableScopeIsIndistinguishable(t *testing.T) {
	item := []string{
		"/api/v1/orgs/nosuchorg/projects/main/posts/hello",
		"/api/v1/orgs/acme/projects/nosuchproject/posts/hello",
		"/api/v1/orgs/acme/projects/main/posts/nosuchpost",
	}
	collection := []string{
		"/api/v1/orgs/nosuchorg/projects/main/posts",
		"/api/v1/orgs/acme/projects/nosuchproject/posts",
	}
	transition := func(action string) []string {
		out := make([]string, 0, len(item))
		for _, p := range item {
			out = append(out, p+"/"+action)
		}
		return out
	}
	create := `{"title":"New","slug":"new","body":"b","categoryId":"` + uuid.New().String() + `"}`

	tests := []struct {
		name      string
		method    string
		paths     []string
		body      string
		wantAnon  int
		wantKeyed int
	}{
		{"get", http.MethodGet, item, "", http.StatusNotFound, http.StatusNotFound},
		{"list", http.MethodGet, collection, "", http.StatusNotFound, http.StatusNotFound},
		{"create", http.MethodPost, collection, create, http.StatusUnauthorized, http.StatusNotFound},
		{"replace", http.MethodPut, item, `{"title":"x"}`, http.StatusUnauthorized, http.StatusNotFound},
		{"patch", http.MethodPatch, item, `{"title":"x"}`, http.StatusUnauthorized, http.StatusNotFound},
		{"delete", http.MethodDelete, item, "", http.StatusUnauthorized, http.StatusNotFound},
		{"publish", http.MethodPost, transition("publish"), "", http.StatusUnauthorized, http.StatusNotFound},
		{"unpublish", http.MethodPost, transition("unpublish"), "", http.StatusUnauthorized, http.StatusNotFound},
	}

	for _, tt := range tests {
		for _, keyed := range []bool{false, true} {
			name := tt.name + "/anonymous"
			want := tt.wantAnon
			if keyed {
				name, want = tt.name+"/keyed", tt.wantKeyed
			}
			t.Run(name, func(t *testing.T) {
				h := newTestHandler(t)
				var first *httptest.ResponseRecorder
				for _, p := range tt.paths {
					rec := httptest.NewRecorder()
					req := httptest.NewRequest(tt.method, p, strings.NewReader(tt.body))
					if keyed {
						req.Header.Set("Authorization", "Bearer "+testKey)
					}
					h.ServeHTTP(rec, req)
					if rec.Code != want {
						t.Fatalf("%s %s => %d, want %d: %s", tt.method, p, rec.Code, want, rec.Body.String())
					}
					if first == nil {
						first = rec
						continue
					}
					if rec.Body.String() != first.Body.String() {
						t.Fatalf("%s %s body differs from the first path; existence is disclosed", tt.method, p)
					}
				}
			})
		}
	}
}

// TestResolvedScopeReachesThePost proves the resolver hands a genuine match through rather than masking it.
func TestResolvedScopeReachesThePost(t *testing.T) {
	h := newTestHandler(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/orgs/acme/projects/main/posts/hello", nil)
	req.Header.Set("Authorization", "Bearer "+testKey)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rec.Code)
	}
}

type erroringOrgs struct{ err error }

func (e erroringOrgs) BySlug(context.Context, string) (dataplane.OrgRef, error) {
	return dataplane.OrgRef{}, e.err
}

// TestUnknownLookupErrorNeverAdmitsTheRequest is the fail-closed guard for any shape of lookup failure.
func TestUnknownLookupErrorNeverAdmitsTheRequest(t *testing.T) {
	// NOTE: keyed to the zero-value org id, so a resolver that treats a failed lookup as the zero UUID returns 200.
	e := newEnv(true)
	h := dataplane.NewHandler(dataplane.HandlerParams{
		BasePath: "/api/v1",
		Orgs:     erroringOrgs{err: errors.New("some unrelated store failure")},
		Projects: fakeProjects{{orgID: uuid.Nil, slug: "main"}: {ID: uuid.New()}},
		Posts:    e.posts,
		Authz:    e.authz,
		Log:      discardLogger(),
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/orgs/acme/projects/main/posts/hello", nil)
	req.Header.Set("Authorization", "Bearer "+testKey)
	h.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatalf("an unrecognized org lookup error was admitted as success (fail open), got %d", rec.Code)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("got %d, want 404 (fail closed)", rec.Code)
	}
}
