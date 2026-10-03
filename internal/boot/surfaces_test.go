package boot_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/dataplane"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/user"
)

func serveOn(t *testing.T, f *dataplaneFixture, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	f.srv.Web.ServeHTTP(rec, req)
	return rec
}

func validSessionCookie(t *testing.T, f *dataplaneFixture) *http.Cookie {
	t.Helper()
	u, err := f.srv.Users.Create(t.Context(), user.CreateRequest{
		Email: "dp-cookie@example.com", Name: "DP Cookie", Source: user.SourceOIDC,
	})
	require.NoError(t, err)
	return probeCookie(t, f.srv, session.Principal{UserID: u.ID})
}

// TestDataPlaneRejectsSessionCookie enforces R2: a session cookie must not authenticate a machine surface.
func TestDataPlaneRejectsSessionCookie(t *testing.T) {
	t.Skip("yasaku: blog is not mounted; see internal/boot/surfaces_yasaku.go")
	f := newDataplaneFixture(t, dataplaneOpts{enabled: true})
	cookie := validSessionCookie(t, f)

	console := httptest.NewRequest(http.MethodGet, "/orgs", nil)
	console.AddCookie(cookie)
	require.Less(t, serveOn(t, f, console).Code, 400,
		"the cookie must authenticate the console, or this guard proves nothing")

	for _, path := range []string{f.postsURL, f.postURL} {
		t.Run(path, func(t *testing.T) {
			withCookie := httptest.NewRequest(http.MethodGet, path, nil)
			withCookie.AddCookie(cookie)
			got := serveOn(t, f, withCookie)
			want := serveOn(t, f, httptest.NewRequest(http.MethodGet, path, nil))

			require.NotEqual(t, http.StatusOK, got.Code,
				"R2: a session cookie authenticated the data plane; body=%s", got.Body.String())
			require.NotContains(t, got.Body.String(), `"slug":"hello"`,
				"R2: a session cookie read a post from the data plane")
			require.Empty(t, got.Header().Values("Set-Cookie"),
				"R2: the data plane touched the session — a machine surface issues no cookies")
			require.Equal(t, want.Code, got.Code,
				"R2: the cookie changed the data plane's answer, so it is being read as a credential")
			require.Equal(t, want.Body.String(), got.Body.String(),
				"R2: the cookie changed the data plane's answer, so it is being read as a credential")
		})
	}
}

// TestDataPlaneRejectsJWT enforces R2 the other way: a JWT is 401, never downgraded to the anonymous read path.
func TestDataPlaneRejectsJWT(t *testing.T) {
	t.Skip("yasaku: blog is not mounted; see internal/boot/surfaces_yasaku.go")
	f := newDataplaneFixture(t, dataplaneOpts{enabled: true, publicReads: true})

	require.Equal(t, http.StatusOK, serveOn(t, f, httptest.NewRequest(http.MethodGet, f.postURL, nil)).Code,
		"an anonymous read must be 200 here, or a 401 below proves nothing")

	const jwt = "eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiJkcC1vd25lciJ9.c2lnbmF0dXJl"
	tests := []struct{ name, header, value string }{
		{"authorization bearer", "Authorization", "Bearer " + jwt},
		{"x-api-key", "X-API-Key", jwt},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, f.postURL, nil)
			req.Header.Set(tt.header, tt.value)
			rec := serveOn(t, f, req)

			require.Equal(t, http.StatusUnauthorized, rec.Code,
				"R4: a JWT is 401 on the data plane, got %d; body=%s", rec.Code, rec.Body.String())
			require.NotContains(t, rec.Body.String(), `"slug":"hello"`,
				"R4: a wrong-class credential was downgraded to an anonymous public read")
		})
	}
}

func adminKeyFor(t *testing.T, f *dataplaneFixture) string {
	t.Helper()
	_, key, err := f.srv.APIKeys.Mint(tenant.WithProject(f.orgCtx, f.project), "dp-admin",
		[]string{dataplane.ScopePostsAdmin}, nil, soon())
	require.NoError(t, err)
	return key
}

// TestSurfaceErrorShapes enforces R6: every data plane failure carries the declared JSON envelope and a code.
func TestSurfaceErrorShapes(t *testing.T) {
	t.Skip("yasaku: blog is not mounted; see internal/boot/surfaces_yasaku.go")
	f := newDataplaneFixture(t, dataplaneOpts{enabled: true})
	adminKey := adminKeyFor(t, f)

	tests := []struct {
		name, method, path, key string
		wantStatus              int
		wantCode                string
	}{
		{"unknown org", http.MethodGet, "/api/v1/orgs/nosuchorg/projects/dp-project/posts", "", http.StatusNotFound, "not_found"},
		{"unknown project", http.MethodGet, "/api/v1/orgs/dp-org/projects/nosuchproject/posts", f.readKey, http.StatusNotFound, "not_found"},
		{"unknown post", http.MethodGet, f.postsURL + "/nosuchpost", f.readKey, http.StatusNotFound, "not_found"},
		{"insufficient scope", http.MethodGet, f.postURL, f.noneKey, http.StatusNotFound, "not_found"},
		{"bogus credential", http.MethodGet, f.postURL, "key_not_a_real_key", http.StatusUnauthorized, "unauthorized"},
		{"uncredentialed read", http.MethodGet, f.postURL, "", http.StatusNotFound, "not_found"},
		{"write without if-match, unauthorized", http.MethodDelete, f.postURL, f.readKey, http.StatusNotFound, "not_found"},
		{"write without if-match, authorized", http.MethodDelete, f.postURL, adminKey, http.StatusPreconditionRequired, "precondition_required"},
		{"subtree root", http.MethodGet, "/api/v1/", "", http.StatusNotFound, "not_found"},
		{"typo'd path", http.MethodGet, f.postsURL + "z", f.readKey, http.StatusNotFound, "not_found"},
		{"method mismatch", http.MethodPatch, f.postsURL, f.readKey, http.StatusMethodNotAllowed, "method_not_allowed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			if tt.key != "" {
				req.Header.Set("Authorization", "Bearer "+tt.key)
			}
			rec := serveOn(t, f, req)
			body := rec.Body.String()

			require.Equal(t, tt.wantStatus, rec.Code, "body=%s", body)
			require.True(t, strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json"),
				"R6: Content-Type = %q, want application/json — the data plane's error shape is fixed",
				rec.Header().Get("Content-Type"))
			require.NotContains(t, body, "<html",
				"R6: an HTML error page reached a machine surface")

			var envelope struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope),
				"R6: the error body is not the declared envelope: %s", body)
			require.Equal(t, tt.wantCode, envelope.Code,
				"R6: the error envelope must carry the declared code; body=%s", body)
		})
	}

	mismatch := serveOn(t, f, httptest.NewRequest(http.MethodPatch, f.postsURL, nil))
	require.Equal(t, "GET, HEAD, POST", mismatch.Header().Get("Allow"),
		"a 405 from the data plane must name the methods the path accepts")

	console := serveOn(t, f, httptest.NewRequest(http.MethodGet, "/nosuchconsolepage", nil))
	require.NotContains(t, console.Header().Get("Content-Type"), "application/json",
		"R6 is per surface: the console must not share the data plane's error shape")
}
