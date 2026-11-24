package boot_test

import (
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/controlplane"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform/authn"
	"altalune.id/yasaku/internal/user"
)

// TestEveryControlPlaneProcedureResolvesMultiOrgToken drives every procedure with a token whose person belongs to two orgs. SECURITY: none may fall through to an unscoped tenant; each resolves an org, asks for one, or refuses.
func TestEveryControlPlaneProcedureResolvesMultiOrgToken(t *testing.T) {
	f := newMCPFixture(t, mcpOpts{enabled: true})
	ctx := t.Context()

	agent, err := f.srv.Users.EnsureFromOIDC(ctx, user.Claims{Issuer: f.issuer.url, Subject: tokenSubject, Email: tokenEmail, EmailVerified: true, Name: "MCP Agent"})
	require.NoError(t, err)
	other, err := f.srv.Users.EnsureFromOIDC(ctx, user.Claims{Issuer: f.issuer.url, Subject: "second-org-owner", Email: "second@example.com", EmailVerified: true, Name: "Second"})
	require.NoError(t, err)
	second, err := f.srv.Orgs.Create(ctx, org.CreateRequest{Slug: "second-org", Name: "Second Org", OwnerID: other.ID})
	require.NoError(t, err)
	joinOrg(t, f, second.ID, agent.ID)

	token := f.issuer.mint(t, controlAudience, authn.AllScopes())
	procedures := make([]string, 0, len(controlplane.ScopeTable()))
	for proc := range controlplane.ScopeTable() {
		procedures = append(procedures, proc)
	}
	slices.Sort(procedures)
	for _, proc := range procedures {
		t.Run(proc, func(t *testing.T) {
			rec := f.connectRPC(t, token, "/api"+proc, map[string]any{})
			require.NotContains(t, rec.Body.String(), "Tenant context names no org",
				"a two-org token reached %s with no org; body=%s", proc, rec.Body.String())
		})
	}
}

func joinOrg(t *testing.T, f *mcpFixture, orgID, userID uuid.UUID) {
	t.Helper()
	_, err := f.srv.Platform.Pool.W.ExecContext(t.Context(),
		"INSERT INTO yasaku_memberships (id, org_id, user_id, role, created_at, system) VALUES (?, ?, ?, 'member', ?, 0)",
		uuid.NewString(), orgID.String(), userID.String(), time.Now().UTC().Format(time.RFC3339Nano))
	require.NoError(t, err)
}
