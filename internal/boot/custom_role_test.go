package boot_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/gen/go/yasaku/v1/yasakuv1connect"
	"altalune.id/yasaku/gen/go/yasaku/v1/yasakuv1mcp"
	"altalune.id/yasaku/internal/apikey"
	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/invite"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform/authn"
	"altalune.id/yasaku/internal/platform/authz"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/role"
	"altalune.id/yasaku/internal/testutil/fakes"
	"altalune.id/yasaku/internal/user"
)

func (c *canSurfaces) ownerCtx() context.Context {
	return fakes.Owner(context.Background(), tenant.Context{OrgID: c.org.ID, UserID: c.owner})
}

func (c *canSurfaces) customRole(t *testing.T, name string, perms ...authz.Permission) *role.Role {
	t.Helper()
	r, err := c.srv.Roles.Create(c.ownerCtx(), name, "", perms)
	require.NoError(t, err)
	return r
}

func (c *canSurfaces) sendInvite(t *testing.T, email string, roleID uuid.UUID) *invite.Invite {
	t.Helper()
	inv, err := c.srv.Invites.Send(c.ownerCtx(), invite.SendRequest{Email: email, Role: invite.RoleCustom, RoleID: roleID})
	require.NoError(t, err)
	return inv
}

func (c *canSurfaces) requireCustomMember(t *testing.T, userID, roleID uuid.UUID) {
	t.Helper()
	m, err := c.srv.Orgs.MembershipOf(c.ownerCtx(), c.org.ID, userID)
	require.NoError(t, err)
	assert.Equal(t, org.RoleCustom, m.Role)
	assert.Equal(t, roleID, m.RoleID)
}

// TestAccept_CustomRoleInvite proves a custom-role invite lands as that custom role on both accept paths, through the real stores and their CHECK constraint.
func TestAccept_CustomRoleInvite(t *testing.T) {
	c := newCanSurfaces(t)
	viewer := c.customRole(t, "Viewer", authz.WalletsRead)

	narrow := fakes.Allow(context.Background(), tenant.Context{OrgID: c.org.ID, UserID: c.owner}, authz.MembersWrite)
	_, err := c.srv.Invites.Send(narrow, invite.SendRequest{Email: "sneaky@example.com", Role: invite.RoleCustom, RoleID: viewer.ID})
	assert.True(t, authz.IsEscalationError(err), "an invite cannot grant what the sender lacks, got %T: %v", err, err)
	esc, ok := errors.AsType[*authz.EscalationError](err)
	require.True(t, ok)
	assert.Equal(t, "Viewer", esc.Role, "the refusal names the custom role")
	err = c.srv.Orgs.SetRole(narrow, c.org.ID, c.plain.ID, string(org.RoleCustom), viewer.ID)
	esc, ok = errors.AsType[*authz.EscalationError](err)
	require.True(t, ok, "SetRole past the caller's own role, got %T: %v", err, err)
	assert.Equal(t, "Viewer", esc.Role, "SetRole names the custom role")
	_, err = c.srv.Invites.Send(c.ownerCtx(), invite.SendRequest{Email: "nil@example.com", Role: invite.RoleCustom})
	assert.True(t, invite.IsInvalidRoleError(err), "a custom invite names its role, got %T: %v", err, err)

	t.Run("token path", func(t *testing.T) {
		inv := c.sendInvite(t, "custom-token@example.com", viewer.ID)
		assert.Equal(t, viewer.ID, inv.RoleID)
		_, err := c.srv.Platform.Pool.W.ExecContext(t.Context(), "UPDATE "+c.srv.Cfg.DB.TablePrefix+"invites SET token_hash = ? WHERE id = ?",
			invite.HashToken("custom-token"), inv.ID.String())
		require.NoError(t, err)
		res, err := c.srv.Invites.Accept(t.Context(), invite.AcceptRequest{Token: "custom-token", Email: "custom-token@example.com", Name: "Token"})
		require.NoError(t, err)
		c.requireCustomMember(t, res.User.ID, viewer.ID)
	})

	t.Run("onboarding", func(t *testing.T) {
		c.sendInvite(t, "custom-onboard@example.com", viewer.ID)
		u, err := c.srv.Users.Create(t.Context(), user.CreateRequest{Email: "custom-onboard@example.com", Name: "Onboard", Source: user.SourceOIDC})
		require.NoError(t, err)
		res, err := c.srv.Onboard.Onboard(t.Context(), u.ID, u.Email)
		require.NoError(t, err)
		assert.Equal(t, c.org.ID, res.OrgID)
		c.requireCustomMember(t, u.ID, viewer.ID)
	})

	err = c.srv.Roles.Delete(c.ownerCtx(), viewer.ID)
	assert.True(t, role.IsInUseError(err), "a held role cannot be deleted, got %T: %v", err, err)
}

func (c *canSurfaces) giveCustom(t *testing.T, userID, roleID uuid.UUID) {
	t.Helper()
	require.NoError(t, c.srv.Orgs.SetRole(c.ownerCtx(), c.org.ID, userID, string(org.RoleCustom), roleID))
}

func (c *canSurfaces) personalKey(t *testing.T, userID uuid.UUID) string {
	t.Helper()
	_, key, err := c.srv.APIKeys.MintPersonal(fakes.Owner(t.Context(), tenant.Context{OrgID: c.org.ID, UserID: userID}), "mine",
		[]string{authn.ScopeYasakuRead, authn.ScopeYasakuWrite}, apikey.ProjectGrant{All: true}, new(time.Now().Add(time.Hour)))
	require.NoError(t, err)
	return key
}

// TestCustomRoleViewer_RefusedEverywhere proves a read-only custom role reads but never writes, on every surface and credential. SECURITY: the held set comes from the custom role's permissions.
func TestCustomRoleViewer_RefusedEverywhere(t *testing.T) {
	c := newCanSurfaces(t)
	viewer := c.customRole(t, "Viewer", authz.WalletsRead, authz.TransactionsRead)
	c.giveCustom(t, c.member.ID, viewer.ID)
	key := c.personalKey(t, c.member.ID)
	expense := map[string]any{"wallet": ownWallet, "amount": map[string]any{"amount": "1000"}, "confirm": true}
	newWallet := map[string]any{"name": "Sneaky", "kind": "cash", "confirm": true}

	for _, cred := range []struct{ name, value string }{{"token", c.memberToken(controlAudience)}, {"personal key", key}} {
		rec := c.rpc(t, cred.value, yasakuv1connect.WalletServiceListWalletsProcedure, map[string]any{})
		assert.Equal(t, http.StatusOK, rec.Code, "%s reads: %s", cred.name, rec.Body.String())
		requireRole001(t, c.rpc(t, cred.value, yasakuv1connect.TransactionServiceRecordExpenseProcedure, expense), cred.name+" records")
		requireRole001(t, c.rpc(t, cred.value, yasakuv1connect.WalletServiceCreateWalletProcedure, newWallet), cred.name+" creates a wallet")
	}
	payload := toolPayload(t, c.tool(t, c.memberToken(mcpAudience), yasakuv1mcp.RecordExpenseToolName, expense))
	assert.Equal(t, apperror.CodePermissionDenied, payload.Code, "MCP tool")

	rec := consolePOST(t, c, session.Principal{UserID: c.member.ID, ActiveOrgID: c.org.ID, Source: session.SourceOIDC},
		"/orgs/mcp-org/projects", url.Values{"name": {"Side"}, "slug": {"side"}})
	assert.Equal(t, http.StatusForbidden, rec.Code, "console: %s", rec.Body.String())
}

// TestRoleShrink_NarrowsExistingKey proves narrowing a custom role narrows a key minted under it at its next call.
func TestRoleShrink_NarrowsExistingKey(t *testing.T) {
	c := newCanSurfaces(t)
	clerk := c.customRole(t, "Clerk", authz.WalletsRead, authz.TransactionsRead, authz.TransactionsWrite)
	c.giveCustom(t, c.member.ID, clerk.ID)
	key := c.personalKey(t, c.member.ID)
	expense := map[string]any{"wallet": ownWallet, "amount": map[string]any{"amount": "700"}, "confirm": true}

	rec := c.rpc(t, key, yasakuv1connect.TransactionServiceRecordExpenseProcedure, expense)
	require.Equal(t, http.StatusOK, rec.Code, "the clerk's key records: %s", rec.Body.String())

	_, err := c.srv.Roles.Update(c.ownerCtx(), clerk.ID, "Clerk", "", []authz.Permission{authz.WalletsRead, authz.TransactionsRead}, 0)
	require.NoError(t, err)
	requireRole001(t, c.rpc(t, key, yasakuv1connect.TransactionServiceRecordExpenseProcedure, expense), "the narrowed key")
}
