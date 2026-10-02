package boot_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/gen/go/org/v1/orgv1connect"
	"altalune.id/yasaku/internal/apikey"
	"altalune.id/yasaku/internal/platform/authn"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/user"
)

// TestControlPlane_ListMembersIsAnOrgLevelRead carries the member_list security assertions onto the Connect RPC yasaku still mounts: an org key holding members:read reads its own org's members, and nothing else can.
func TestControlPlane_ListMembersIsAnOrgLevelRead(t *testing.T) {
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

	procedure := "/api" + orgv1connect.MemberServiceListMembersProcedure

	rec := f.connectRPC(t, reader, procedure, map[string]any{})
	require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
	require.Contains(t, rec.Body.String(), tokenEmail, "the org's own member must be listed; body=%s", rec.Body.String())
	require.NotContains(t, rec.Body.String(), outsiderEmail, "another org's member must never be listed; body=%s", rec.Body.String())

	rec = f.connectRPC(t, f.readKey, procedure, map[string]any{})
	require.Equal(t, http.StatusForbidden, rec.Code, "a key without members:read must be refused; body=%s", rec.Body.String())
	require.NotContains(t, rec.Body.String(), tokenEmail, "the refusal leaked a member; body=%s", rec.Body.String())
}
