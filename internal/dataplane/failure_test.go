package dataplane_test

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"
)

// TestStoreFailureIsReportedNotMasked keeps an infrastructure failure out of the masked 404.
func TestStoreFailureIsReportedNotMasked(t *testing.T) {
	tests := []struct {
		name    string
		arrange func(e *env)
		path    string
		want    int
		wantLog bool
	}{
		{
			name:    "item read, store unreachable",
			arrange: func(e *env) { e.posts.bySlugErr = errors.New("connection refused") },
			path:    "/api/v1/orgs/acme/projects/main/posts/hello",
			want:    http.StatusInternalServerError,
			wantLog: true,
		},
		{
			name:    "collection read, store unreachable",
			arrange: func(e *env) { e.posts.listErr = errors.New("connection refused") },
			path:    "/api/v1/orgs/acme/projects/main/posts",
			want:    http.StatusInternalServerError,
			wantLog: true,
		},
		{
			name:    "item read, post genuinely missing",
			arrange: func(*env) {},
			path:    "/api/v1/orgs/acme/projects/main/posts/nosuchpost",
			want:    http.StatusNotFound,
			wantLog: false,
		},
		{
			name:    "item read, key denied on the category",
			arrange: func(e *env) { e.authz.scopes = nil },
			path:    "/api/v1/orgs/acme/projects/main/posts/hello",
			want:    http.StatusNotFound,
			wantLog: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logged bytes.Buffer
			e := newEnv(true)
			e.log = slog.New(slog.NewTextHandler(&logged, nil))
			tt.arrange(e)

			rec := send(t, e.handler(), http.MethodGet, tt.path, "", nil)
			if rec.Code != tt.want {
				t.Fatalf("code = %d, want %d: %s", rec.Code, tt.want, rec.Body.String())
			}
			if got := strings.Contains(logged.String(), "data plane request failed"); got != tt.wantLog {
				t.Fatalf("logged = %v, want %v: %q", got, tt.wantLog, logged.String())
			}
		})
	}
}
