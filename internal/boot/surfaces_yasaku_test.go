package boot_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform/authn"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/events"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/user"
)

// TestYasakuSurfaces pins what yasaku must not publish: blog, todo and the webhooks console stay reference code, mounted on no surface.
func TestYasakuSurfaces(t *testing.T) {
	t.Run("the console mounts no blog, todo or webhooks page and the nav links none", func(t *testing.T) {
		srv, _ := newScopeProbeServer(t, config.ModeCloud)
		owner, err := srv.Users.Create(context.Background(), user.CreateRequest{
			Email: "surface-owner@example.com", Name: "Surface Owner", Source: user.SourceOIDC,
		})
		require.NoError(t, err)
		o, err := srv.Orgs.Create(context.Background(), org.CreateRequest{Slug: "probe-org", Name: "Probe Org", OwnerID: owner.ID})
		require.NoError(t, err)
		orgCtx := tenant.Into(context.Background(), tenant.Context{OrgID: o.ID, UserID: owner.ID})
		_, err = srv.Projects.Create(orgCtx, o.ID, "probe-project", "Probe Project")
		require.NoError(t, err)
		cookie := probeCookie(t, srv, session.Principal{UserID: owner.ID, ActiveOrgID: o.ID})

		get := func(t *testing.T, path string) *httptest.ResponseRecorder {
			t.Helper()
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.AddCookie(cookie)
			rec := httptest.NewRecorder()
			srv.Web.ServeHTTP(rec, req)
			return rec
		}

		const pbase = "/orgs/probe-org/projects/probe-project"
		for _, page := range []string{"/blog", "/posts", "/tags", "/todos", "/webhooks", "/webhooks/new"} {
			t.Run(page, func(t *testing.T) {
				rec := get(t, pbase+page)
				require.Equal(t, http.StatusNotFound, rec.Code, "%s is mounted; body=%s", page, rec.Body.String())
			})
		}

		overview := get(t, pbase+"/overview")
		require.Equal(t, http.StatusOK, overview.Code, "the overview must render, or the nav check below proves nothing")
		body := overview.Body.String()
		require.Contains(t, body, pbase+"/wallets", "the project nav did not render")
		for _, page := range []string{"/posts", "/tags", "/todos", "/webhooks"} {
			require.NotContains(t, body, `href="`+pbase+page+`"`, "the project nav links the unmounted %s page", page)
		}

		for _, pat := range srv.Routes {
			for _, page := range []string{"/posts", "/tags", "/todos", "/webhooks"} {
				require.NotContains(t, pat, "/projects/{project}"+page, "the console registered %q", pat)
			}
		}
	})

	t.Run("the control plane mounts no blog or todo procedure", func(t *testing.T) {
		f := newMCPFixture(t, mcpOpts{enabled: true})
		procs := f.srv.API.MountedProcedures()
		require.NotEmpty(t, procs, "the control plane mounted nothing, so this check proves nothing")
		for _, p := range procs {
			require.NotContains(t, p, "blog.v1.", "the control plane mounts %s", p)
			require.NotContains(t, p, "todo.v1.", "the control plane mounts %s", p)
		}
	})

	t.Run("the MCP surface lists exactly the 27 yasaku tools", func(t *testing.T) {
		f := newMCPFixture(t, mcpOpts{enabled: true})
		rec := f.call(t, f.issuer.mint(t, mcpAudience, []string{authn.ScopeYasakuRead}), listToolsBody())
		require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())

		var result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		}
		require.NoError(t, json.Unmarshal(decodeRPC(t, rec).Result, &result))
		names := make([]string, 0, len(result.Tools))
		for _, tool := range result.Tools {
			names = append(names, tool.Name)
		}
		require.ElementsMatch(t, yasakuToolNames(), names)
	})

	t.Run("the data plane serves no posts even when enabled", func(t *testing.T) {
		f := newDataplaneFixture(t, dataplaneOpts{enabled: true, publicReads: true})
		rec := f.get(t, f.postURL, f.readKey)
		require.NotEqual(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
		require.NotContains(t, rec.Body.String(), `"slug":"hello"`, "the data plane served a post: %s", rec.Body.String())
	})

	t.Run("the blog events stay in the catalog but nothing can emit or subscribe to them", func(t *testing.T) {
		for _, spec := range events.Subscribable() {
			require.True(t, strings.HasPrefix(string(spec.Type), "blog."),
				"%s is subscribable and not a blog event; the console gate below no longer covers every event", spec.Type)
		}

		srv, _ := newScopeProbeServer(t, config.ModeCloud)
		for _, pat := range srv.Routes {
			require.NotContains(t, pat, "/webhooks", "the console registers %q, a webhook endpoint route", pat)
			require.NotContains(t, pat, "/posts", "the console registers %q, a blog emitter route", pat)
		}

		f := newMCPFixture(t, mcpOpts{enabled: true})
		for _, p := range f.srv.API.MountedProcedures() {
			require.NotContains(t, p, "blog.v1.", "the control plane mounts %s, a blog emitter", p)
			require.NotContains(t, strings.ToLower(p), "webhook", "the control plane mounts %s, a webhook endpoint RPC", p)
		}
		for _, name := range f.srv.MCP.Registry().Names() {
			require.False(t, strings.HasPrefix(name, "blog_"), "MCP registers %s, a blog emitter", name)
			require.NotContains(t, name, "webhook", "MCP registers %s, a webhook tool", name)
		}

		dp := newDataplaneFixture(t, dataplaneOpts{enabled: true})
		for _, path := range []string{dp.postsURL, dp.postURL, dp.postURL + "/publish", dp.postURL + "/unpublish"} {
			req := httptest.NewRequest(http.MethodPost, path, nil)
			req.Header.Set("Authorization", "Bearer "+dp.readKey)
			rec := httptest.NewRecorder()
			dp.srv.Web.ServeHTTP(rec, req)
			require.NotContains(t, []int{http.StatusOK, http.StatusCreated, http.StatusPreconditionRequired}, rec.Code,
				"the data plane answered POST %s as a mounted emitter; body=%s", path, rec.Body.String())
		}
	})

	t.Run("the scope catalog carries no posts scope", func(t *testing.T) {
		for _, s := range authn.AllScopes() {
			require.False(t, strings.HasPrefix(s, "posts:"), "the scope catalog carries %q", s)
		}
	})
}

func yasakuToolNames() []string {
	return []string{
		"list_projects", "now",
		"list_wallets", "get_wallet", "wallet_totals", "create_wallet", "update_wallet", "archive_wallet", "adjust_balance",
		"list_categories", "create_category", "seed_default_categories",
		"list_recent_tx", "search_tx", "record_expense", "record_income", "record_transfer", "record_batch", "revise_tx", "delete_tx",
		"current_period", "list_periods", "preview_close", "close_period", "reopen_period",
		"period_report", "cashflow_report",
	}
}
