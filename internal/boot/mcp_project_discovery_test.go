package boot_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/gen/go/yasaku/v1/yasakuv1mcp"
	"altalune.id/yasaku/internal/apikey"
	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform/authn"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/user"
)

type toolProject struct {
	Org         string `json:"org"`
	OrgName     string `json:"orgName"`
	Project     string `json:"project"`
	ProjectName string `json:"projectName"`
}

func projectsFromTool(t *testing.T, rec *httptest.ResponseRecorder) []toolProject {
	t.Helper()
	resp := decodeRPC(t, rec)
	require.Nil(t, resp.Error, "a tool failure must answer in the result, not the JSON-RPC envelope")

	var result struct {
		IsError           bool `json:"isError"`
		StructuredContent struct {
			Projects []toolProject `json:"projects"`
		} `json:"structuredContent"`
	}
	require.NoError(t, json.Unmarshal(resp.Result, &result), "result=%s", string(resp.Result))
	require.False(t, result.IsError, "list_projects reported an error: %s", string(resp.Result))
	return result.StructuredContent.Projects
}

func listWalletsIn(project string) map[string]any {
	if project == "" {
		return callToolBody(yasakuv1mcp.ListWalletsToolName, map[string]any{})
	}
	return callToolBody(yasakuv1mcp.ListWalletsToolName, map[string]any{"target": map[string]any{"project": project}})
}

// TestMCP_ListProjectsIsScopedToTheCallersOrg drives the discovery tool over the real surface. SECURITY: list_projects is the only tool that hands an agent project slugs, so a foreign row surfacing here is a target the agent can then feed to every other tool.
func TestMCP_ListProjectsIsScopedToTheCallersOrg(t *testing.T) {
	f := newMCPFixture(t, mcpOpts{enabled: true})
	foreign := seedForeignTenant(t, f)

	credentials := map[string]string{
		"key": f.readKey,
		"jwt": f.issuer.mint(t, mcpAudience, []string{authn.ScopeYasakuRead}),
	}
	for name, credential := range credentials {
		t.Run(name, func(t *testing.T) {
			rec := f.call(t, credential, callToolBody(yasakuv1mcp.ListProjectsToolName, map[string]any{}))
			require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
			require.NotContains(t, rec.Body.String(), foreign.projectSlug,
				"list_projects leaked another org's project; body=%s", rec.Body.String())
			require.NotContains(t, rec.Body.String(), foreign.orgSlug,
				"list_projects leaked another org; body=%s", rec.Body.String())

			projects := projectsFromTool(t, rec)
			require.NotEmpty(t, projects, "list_projects returned nothing; an agent cannot discover a target")

			slugs := make([]string, 0, len(projects))
			for _, p := range projects {
				slugs = append(slugs, p.Project)
				require.NotEqual(t, foreign.orgSlug, p.Org,
					"list_projects returned a project owned by the foreign org")
			}
			require.Contains(t, slugs, "mcp-project", "list_projects omitted the caller's own project; got %v", slugs)
			require.NotContains(t, slugs, foreign.projectSlug,
				"list_projects returned a project from an org the caller is not a member of; got %v", slugs)
		})
	}
}

// TestMCP_ListWalletsResolvesItsProject pins the ways list_wallets arrives at a project over the MCP surface: the explicit target, the credential's active project, and neither.
func TestMCP_ListWalletsResolvesItsProject(t *testing.T) {
	t.Run("an omitted target reads the credential's active project", func(t *testing.T) {
		f := newMCPFixture(t, mcpOpts{enabled: true})
		f.seedOwnWallet(t)
		seedForeignTenant(t, f)

		rec := f.call(t, f.readKey, listWalletsIn(""))
		require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
		require.Equal(t, []string{ownWallet}, walletNamesFromTool(t, rec),
			"an argument-less list_wallets did not read the key's own project; body=%s", rec.Body.String())
		require.NotContains(t, rec.Body.String(), foreignWallet,
			"an argument-less list_wallets read another org's wallets; body=%s", rec.Body.String())
	})

	t.Run("an explicit target still reads that project", func(t *testing.T) {
		f := newMCPFixture(t, mcpOpts{enabled: true})
		f.seedOwnWallet(t)

		rec := f.call(t, f.readKey, listWalletsIn("mcp-project"))
		require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
		require.Equal(t, []string{ownWallet}, walletNamesFromTool(t, rec), "body=%s", rec.Body.String())
	})

	t.Run("an explicit target in another org stays unreadable", func(t *testing.T) {
		f := newMCPFixture(t, mcpOpts{enabled: true})
		foreign := seedForeignTenant(t, f)

		rec := f.call(t, f.readKey, callToolBody(yasakuv1mcp.ListWalletsToolName, map[string]any{
			"target": map[string]any{"org": foreign.orgSlug, "project": foreign.projectSlug},
		}))
		require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
		require.NotContains(t, rec.Body.String(), foreignWallet,
			"naming another org's project made its wallets readable; body=%s", rec.Body.String())
		require.Equal(t, apperror.CodeNotFound, toolPayload(t, rec).Code,
			"a cross-org read must fail with the masked not-found, not an empty list; body=%s", rec.Body.String())
	})

	t.Run("no target and no project names the field to resolve", func(t *testing.T) {
		f := newMCPFixture(t, mcpOpts{enabled: true})
		token := f.untenantedToken(t)

		rec := f.call(t, token, listWalletsIn(""))
		require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())

		payload := toolPayload(t, rec)
		require.Equal(t, apperror.CodeValidation, payload.Code,
			"an unresolvable project must answer with a validation code, not a generic failure; body=%s", rec.Body.String())
		require.Contains(t, payload.Message, "project",
			"the error must name the field that resolves it; message=%q", payload.Message)
	})
}

func (f *mcpFixture) untenantedToken(t *testing.T) string {
	t.Helper()
	const subject = "mcp-agent-no-project"

	u, err := f.srv.Users.EnsureFromOIDC(t.Context(), user.Claims{
		Issuer: f.issuer.url, Subject: subject, Email: "no-project@example.com", Name: "No Project",
	})
	require.NoError(t, err)
	_, err = f.srv.Orgs.Create(t.Context(), org.CreateRequest{
		Slug: "no-project-org", Name: "No Project Org", OwnerID: u.ID,
	})
	require.NoError(t, err)

	return f.issuer.mintWith(t, mcpAudience, []string{authn.ScopeYasakuRead}, map[string]any{
		"sub": subject, "email": "no-project@example.com",
	})
}

// TestMCP_KeyNeverReachesASiblingProject drives a project-bound key over the real surface. SECURITY: the fixture runs on SQLite, so the handler's reach check is the only guard between the key and its sibling project.
func TestMCP_KeyNeverReachesASiblingProject(t *testing.T) {
	f := newMCPFixture(t, mcpOpts{enabled: true})
	ctx := t.Context()

	owner, err := f.srv.Users.EnsureFromOIDC(ctx, user.Claims{
		Issuer: f.issuer.url, Subject: tokenSubject, Email: tokenEmail, Name: "MCP Agent",
	})
	require.NoError(t, err)
	o, err := f.srv.Orgs.BySlug(ctx, "mcp-org")
	require.NoError(t, err)
	orgCtx := tenant.Into(ctx, tenant.Context{OrgID: o.ID, UserID: owner.ID})
	sibling, err := f.srv.Projects.Create(orgCtx, o.ID, "sibling-project", "Sibling Project")
	require.NoError(t, err)
	f.seedWallet(t, f.issuer.mint(t, mcpAudience, readWrite()), map[string]any{"project": sibling.Slug}, "sibling-secret")

	t.Run("list_projects hides the sibling project", func(t *testing.T) {
		rec := f.call(t, f.readKey, callToolBody(yasakuv1mcp.ListProjectsToolName, map[string]any{}))
		require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
		slugs := make([]string, 0)
		for _, p := range projectsFromTool(t, rec) {
			slugs = append(slugs, p.Project)
		}
		require.Equal(t, []string{"mcp-project"}, slugs, "a project-bound key discovered a project it does not reach")
	})

	t.Run("list_wallets refuses the sibling project", func(t *testing.T) {
		rec := f.call(t, f.readKey, listWalletsIn(sibling.Slug))
		require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
		require.NotContains(t, rec.Body.String(), "sibling-secret",
			"a key read its sibling project's wallets; body=%s", rec.Body.String())
		require.Equal(t, apperror.CodeNotFound, toolPayload(t, rec).Code, "an unreachable project must answer the same masked not-found as an unknown one; body=%s", rec.Body.String())
	})

	t.Run("a person in the org still reaches the sibling project", func(t *testing.T) {
		token := f.issuer.mint(t, mcpAudience, []string{authn.ScopeYasakuRead})
		rec := f.call(t, token, listWalletsIn(sibling.Slug))
		require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
		require.Equal(t, []string{"sibling-secret"}, walletNamesFromTool(t, rec), "body=%s", rec.Body.String())
	})
}

// TestMCP_OrgKeyReachesOnlyItsGrantedProjects drives an org key over the real surface on SQLite, where no RLS backs the reach check.
func TestMCP_OrgKeyReachesOnlyItsGrantedProjects(t *testing.T) {
	f := newMCPFixture(t, mcpOpts{enabled: true})
	f.seedOwnWallet(t)
	ctx := t.Context()

	owner, err := f.srv.Users.EnsureFromOIDC(ctx, user.Claims{
		Issuer: f.issuer.url, Subject: tokenSubject, Email: tokenEmail, Name: "MCP Agent",
	})
	require.NoError(t, err)
	o, err := f.srv.Orgs.BySlug(ctx, "mcp-org")
	require.NoError(t, err)
	orgCtx := tenant.Into(ctx, tenant.Context{OrgID: o.ID, UserID: owner.ID})
	granted, err := f.srv.Projects.Create(orgCtx, o.ID, "granted-project", "Granted Project")
	require.NoError(t, err)
	_, err = f.srv.Projects.Create(orgCtx, o.ID, "ungranted-project", "Ungranted Project")
	require.NoError(t, err)

	_, selected, err := f.srv.APIKeys.MintOrg(orgCtx, "org-reader", []string{authn.ScopeYasakuRead},
		apikey.ProjectGrant{ProjectIDs: []uuid.UUID{granted.ID}}, soon())
	require.NoError(t, err)
	_, everything, err := f.srv.APIKeys.MintOrg(orgCtx, "org-all", []string{authn.ScopeYasakuRead}, apikey.ProjectGrant{All: true}, soon())
	require.NoError(t, err)

	slugsFor := func(t *testing.T, key string) []string {
		t.Helper()
		rec := f.call(t, key, callToolBody(yasakuv1mcp.ListProjectsToolName, map[string]any{}))
		require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
		out := []string{}
		for _, p := range projectsFromTool(t, rec) {
			out = append(out, p.Project)
		}
		return out
	}

	t.Run("a selected-projects key discovers only its grant", func(t *testing.T) {
		require.Equal(t, []string{"granted-project"}, slugsFor(t, selected))
	})
	t.Run("a selected-projects key cannot read an ungranted project", func(t *testing.T) {
		rec := f.call(t, selected, listWalletsIn("mcp-project"))
		require.Equal(t, apperror.CodeNotFound, toolPayload(t, rec).Code, "an unreachable project must answer the same masked not-found as an unknown one; body=%s", rec.Body.String())
	})
	t.Run("an omitted target picks only among the granted projects", func(t *testing.T) {
		f.seedWallet(t, f.issuer.mint(t, mcpAudience, readWrite()), map[string]any{"project": granted.Slug}, "granted-wallet")
		rec := f.call(t, selected, listWalletsIn(""))
		require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
		require.Equal(t, []string{"granted-wallet"}, walletNamesFromTool(t, rec),
			"an argument-less call by a selected-projects key must resolve to its one granted project; body=%s", rec.Body.String())
	})
	t.Run("an all-projects key discovers every project of its org", func(t *testing.T) {
		require.ElementsMatch(t, []string{"mcp-project", "granted-project", "ungranted-project"}, slugsFor(t, everything))
		rec := f.call(t, everything, listWalletsIn("mcp-project"))
		require.Equal(t, []string{ownWallet}, walletNamesFromTool(t, rec), "body=%s", rec.Body.String())
	})
}

// TestMCP_PersonalTokenActsForItsOwner drives a member's personal token over the real surface: it reaches its grant, and dies the moment the member leaves.
func TestMCP_PersonalTokenActsForItsOwner(t *testing.T) {
	f := newMCPFixture(t, mcpOpts{enabled: true})
	f.seedOwnWallet(t)
	ctx := t.Context()

	o, err := f.srv.Orgs.BySlug(ctx, "mcp-org")
	require.NoError(t, err)
	member, err := f.srv.Users.Create(ctx, user.CreateRequest{Email: "pat-member@example.com", Name: "PAT Member", Source: user.SourceOIDC})
	require.NoError(t, err)
	memberCtx := tenant.Into(ctx, tenant.Context{OrgID: o.ID, UserID: member.ID})
	_, err = f.srv.Orgs.AddMember(memberCtx, o.ID, member.ID, org.RoleMember)
	require.NoError(t, err)

	_, token, err := f.srv.APIKeys.MintPersonal(memberCtx, "laptop", []string{authn.ScopeYasakuRead}, apikey.ProjectGrant{All: true}, soon())
	require.NoError(t, err)

	rec := f.call(t, token, listWalletsIn("mcp-project"))
	require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
	require.Equal(t, []string{ownWallet}, walletNamesFromTool(t, rec), "a member's token must read what the member can; body=%s", rec.Body.String())

	owner, err := f.srv.Users.EnsureFromOIDC(ctx, user.Claims{Issuer: f.issuer.url, Subject: tokenSubject, Email: tokenEmail, Name: "MCP Agent"})
	require.NoError(t, err)
	require.NoError(t, f.srv.Orgs.RemoveMember(tenant.Into(ctx, tenant.Context{OrgID: o.ID, UserID: owner.ID}), o.ID, member.ID))
	rec = f.call(t, token, listWalletsIn("mcp-project"))
	require.Equal(t, http.StatusUnauthorized, rec.Code, "a departed member's token must be refused at the door; body=%s", rec.Body.String())

	_, err = f.srv.Orgs.AddMember(memberCtx, o.ID, member.ID, org.RoleMember)
	require.NoError(t, err)
	rec = f.call(t, token, listWalletsIn("mcp-project"))
	require.Equal(t, http.StatusUnauthorized, rec.Code, "removal revoked the token for good, so re-joining must not revive it; body=%s", rec.Body.String())
}

// TestMCP_MemberListIsAnOrgLevelRead drives the org-level member_list tool: an org key holding members:read reads its own org's members, and nothing else can.
func TestMCP_MemberListIsAnOrgLevelRead(t *testing.T) {
	t.Skip("yasaku: org.v1 member_list is not published; see publishedMCPDomains in internal/boot/surfaces_yasaku.go")
	f := newMCPFixture(t, mcpOpts{enabled: true})
	ctx := t.Context()
	seedForeignTenant(t, f)

	owner, err := f.srv.Users.EnsureFromOIDC(ctx, user.Claims{Issuer: f.issuer.url, Subject: tokenSubject, Email: tokenEmail, Name: "MCP Agent"})
	require.NoError(t, err)
	o, err := f.srv.Orgs.BySlug(ctx, "mcp-org")
	require.NoError(t, err)
	orgCtx := tenant.Into(ctx, tenant.Context{OrgID: o.ID, UserID: owner.ID})

	_, reader, err := f.srv.APIKeys.MintOrg(orgCtx, "members", []string{authn.ScopeMembersRead}, apikey.ProjectGrant{All: true}, soon())
	require.NoError(t, err)

	rec := f.call(t, reader, callToolBody("member_list", map[string]any{}))
	require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
	require.Contains(t, rec.Body.String(), tokenEmail, "the org's own member must be listed; body=%s", rec.Body.String())
	require.NotContains(t, rec.Body.String(), "outsider@example.com", "another org's member must never be listed; body=%s", rec.Body.String())

	rec = f.call(t, f.readKey, callToolBody("member_list", map[string]any{}))
	require.Equal(t, apperror.CodeForbidden, toolPayload(t, rec).Code, "a key without members:read must be refused; body=%s", rec.Body.String())
}
