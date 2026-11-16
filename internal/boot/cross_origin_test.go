package boot_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/web"
)

func postWithOrigin(t *testing.T, h http.Handler, path, fetchSite, origin string, header http.Header, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	for k, v := range header {
		req.Header[k] = v
	}
	if fetchSite != "" {
		req.Header.Set("Sec-Fetch-Site", fetchSite)
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// SECURITY: subdomains of one site are same-site, so SameSite=Lax alone lets a sibling subdomain post a console form.
func TestConsoleRefusesCrossOriginPosts(t *testing.T) {
	f := newEdgeFixture(t)
	form := http.Header{"Content-Type": {"application/x-www-form-urlencoded"}}
	login := url.Values{"email": {"root@example.com"}, "password": {"wrong"}}.Encode()

	for _, tc := range []struct {
		name, fetchSite, origin string
		refused                 bool
	}{
		{"a cross-site browser post", "cross-site", "https://evil.example", true},
		{"a same-site sibling subdomain post", "same-site", "https://other.127.0.0.1", true},
		{"an old browser posting from another origin", "", "https://evil.example", true},
		{"a same-origin browser post", "same-origin", "", false},
		{"an old browser posting from the configured base URL behind a proxy", "", "http://127.0.0.1", false},
		{"a non-browser post", "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := postWithOrigin(t, f.srv.Web, "/login", tc.fetchSite, tc.origin, form, login)
			if tc.refused {
				require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
				return
			}
			require.NotEqual(t, http.StatusForbidden, rec.Code, rec.Body.String())
		})
	}
}

func TestCrossOriginCheckLeavesBearerSurfacesAlone(t *testing.T) {
	f := newMCPFixture(t, mcpOpts{enabled: true})
	bearer := "Bearer " + f.readKey

	t.Run("control plane", func(t *testing.T) {
		header := http.Header{"Content-Type": {"application/json"}, "Connect-Protocol-Version": {"1"}, "Authorization": {bearer}}
		rec := postWithOrigin(t, f.srv.Web, walletListProcedure, "cross-site", "https://evil.example", header, `{}`)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	})

	t.Run("mcp", func(t *testing.T) {
		raw, err := json.Marshal(listToolsBody())
		require.NoError(t, err)
		header := http.Header{"Content-Type": {"application/json"}, "Accept": {mcpProtocolHeader}, "Authorization": {bearer}}
		rec := postWithOrigin(t, f.srv.Web, mcpPath, "cross-site", "https://evil.example", header, string(raw))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	})
}

// SECURITY: a sibling site must not flip a signed-in person's stored theme.
func TestConsoleRefusesCrossOriginThemePost(t *testing.T) {
	f := newEdgeFixture(t)
	form := http.Header{"Content-Type": {"application/x-www-form-urlencoded"}}
	body := url.Values{"mode": {"dark"}, "redirect": {"/"}}.Encode()

	rec := postWithOrigin(t, f.srv.Web, "/theme", "cross-site", "https://evil.example", form, body)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
}

// SECURITY: a sibling site must not mark a signed-in person's tips as seen.
func TestConsoleRefusesCrossOriginTipSeenPost(t *testing.T) {
	f := newEdgeFixture(t)
	form := http.Header{"Content-Type": {"application/x-www-form-urlencoded"}}
	body := url.Values{"all": {"1"}, "redirect": {"/"}}.Encode()

	rec := postWithOrigin(t, f.srv.Web, "/tips/"+web.Tips()[0].ID+"/seen", "cross-site", "https://evil.example", form, body)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
}

// SECURITY: a sibling site must not rename a project or provision a workspace for a signed-in person.
func TestConsoleRefusesCrossOriginRenameAndSignupPosts(t *testing.T) {
	f := newEdgeFixture(t)
	form := http.Header{"Content-Type": {"application/x-www-form-urlencoded"}}
	for _, path := range []string{"/orgs/acme/projects/alpha/rename", "/signup/complete"} {
		t.Run(path, func(t *testing.T) {
			rec := postWithOrigin(t, f.srv.Web, path, "cross-site", "https://evil.example", form, url.Values{"name": {"Evil"}}.Encode())
			require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
		})
	}
}
