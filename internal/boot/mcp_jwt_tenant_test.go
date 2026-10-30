package boot_test

import (
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/gen/go/yasaku/v1/yasakuv1mcp"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform/authn"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/user"
)

const (
	outsiderSubject = "mcp-outsider-subject"
	outsiderEmail   = "outsider@example.com"
	ownWallet       = "hello"
	foreignWallet   = "foreign-secret"
)

func (i *tokenIssuer) mintWith(t *testing.T, audience string, scopes []string, extra map[string]any) string {
	t.Helper()
	claims := map[string]any{"aud": audience, "scope": strings.Join(scopes, " ")}
	maps.Copy(claims, extra)
	return i.idp.AccessToken(claims)
}

type foreignTenant struct {
	orgID       string
	projectID   string
	orgSlug     string
	projectSlug string
}

func readWrite() []string { return []string{authn.ScopeYasakuRead, authn.ScopeYasakuWrite} }

func seedForeignTenant(t *testing.T, f *mcpFixture) foreignTenant {
	t.Helper()

	ctx := t.Context()
	outsider, err := f.srv.Users.EnsureFromOIDC(ctx, user.Claims{
		Issuer: f.issuer.url, Subject: outsiderSubject, Email: outsiderEmail, EmailVerified: true, Name: "Outsider",
	})
	require.NoError(t, err)
	o, err := f.srv.Orgs.Create(ctx, org.CreateRequest{Slug: "foreign-org", Name: "Foreign Org", OwnerID: outsider.ID})
	require.NoError(t, err)

	orgCtx := tenant.Into(ctx, tenant.Context{OrgID: o.ID, UserID: outsider.ID})
	p, err := f.srv.Projects.Create(orgCtx, o.ID, "foreign-project", "Foreign Project")
	require.NoError(t, err)

	token := f.issuer.mintWith(t, mcpAudience, readWrite(), map[string]any{"sub": outsiderSubject, "email": outsiderEmail})
	f.seedWallet(t, token, map[string]any{"org": o.Slug, "project": p.Slug}, foreignWallet)

	return foreignTenant{orgID: o.ID.String(), projectID: p.ID.String(), orgSlug: o.Slug, projectSlug: p.Slug}
}

func (f *mcpFixture) seedOwnWallet(t *testing.T) {
	t.Helper()
	f.seedWallet(t, f.issuer.mint(t, mcpAudience, readWrite()), map[string]any{"org": "mcp-org", "project": "mcp-project"}, ownWallet)
}

func (f *mcpFixture) seedWallet(t *testing.T, credential string, target map[string]any, name string) {
	t.Helper()
	rec := f.call(t, credential, callToolBody(yasakuv1mcp.CreateWalletToolName, map[string]any{
		"target": target, "name": name, "kind": "cash", "confirm": true,
	}))
	require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
	require.NotNil(t, toolResult(t, rec)["result"], "create_wallet %q saved nothing; body=%s", name, rec.Body.String())
}

func walletNamesFromTool(t *testing.T, rec *httptest.ResponseRecorder) []string {
	t.Helper()
	wallets, _ := toolResult(t, rec)["wallets"].([]any)
	names := make([]string, 0, len(wallets))
	for _, w := range wallets {
		row, _ := w.(map[string]any)
		name, _ := row["name"].(string)
		names = append(names, name)
	}
	return names
}

// TestMCP_JWTResolvesItsTenantFromMembership drives the S7 mount with a Bearer JWT end to end. SECURITY: a token never names its org.
func TestMCP_JWTResolvesItsTenantFromMembership(t *testing.T) {
	f := newMCPFixture(t, mcpOpts{enabled: true})
	f.seedOwnWallet(t)
	foreign := seedForeignTenant(t, f)

	read := []string{authn.ScopeYasakuRead}

	t.Run("a token with no org_id resolves the subject's only membership", func(t *testing.T) {
		token := f.issuer.mintWith(t, mcpAudience, read, nil)
		rec := f.call(t, token, callToolBody(yasakuv1mcp.ListWalletsToolName, map[string]any{}))

		require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
		require.NotContains(t, rec.Body.String(), "Tenant context names no org",
			"a verified JWT reached a tenant-scoped tool with no org: membership never resolved; body=%s", rec.Body.String())
		require.Equal(t, []string{ownWallet}, walletNamesFromTool(t, rec),
			"the JWT call must read the org's own wallets; body=%s", rec.Body.String())
	})

	t.Run("an org_id claim naming another org is ignored and membership decides", func(t *testing.T) {
		token := f.issuer.mintWith(t, mcpAudience, read, map[string]any{"org_id": foreign.orgID})

		own := f.call(t, token, callToolBody(yasakuv1mcp.ListWalletsToolName, map[string]any{}))
		require.Equal(t, http.StatusOK, own.Code, "body=%s", own.Body.String())
		require.Equal(t, []string{ownWallet}, walletNamesFromTool(t, own),
			"membership must still resolve the subject's own org; body=%s", own.Body.String())

		rec := f.call(t, token, callToolBody(yasakuv1mcp.ListWalletsToolName, map[string]any{
			"target": map[string]any{"org": foreign.orgSlug, "project": foreign.projectSlug},
		}))
		require.NotContains(t, rec.Body.String(), foreignWallet,
			"a token naming org_id=%s read that org's data; an issuer must never name a tenant; body=%s", foreign.orgID, rec.Body.String())
	})
}
