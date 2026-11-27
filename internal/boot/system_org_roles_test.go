package boot_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/boot"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform/authz"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/role"
	"altalune.id/yasaku/internal/testutil/fakes"
	"altalune.id/yasaku/internal/user"
)

func bootSystemOrg(t *testing.T, mode config.Mode) (*boot.Server, *org.Org, context.Context, *user.User, *user.User) {
	t.Helper()
	cfg := newSmokeCfg(t)
	if mode == config.ModeCloud {
		cfg.Mode = config.ModeCloud
		cfg.OIDC = config.OIDCConfig{Issuer: stubIssuer(t), ClientID: "roles-client", ClientSecret: "roles-secret"}
	}
	srv, err := boot.BootServer(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = srv.Close() })
	ctx := context.Background()
	owner, err := srv.Users.Create(ctx, user.CreateRequest{Email: "owner@example.com", Name: "Owner", Source: user.SourceOIDC})
	require.NoError(t, err)
	member, err := srv.Users.Create(ctx, user.CreateRequest{Email: "member@example.com", Name: "Member", Source: user.SourceOIDC})
	require.NoError(t, err)
	sys, err := srv.Orgs.BootstrapSingleton(ctx, "", "Default", owner.ID)
	require.NoError(t, err)
	ownerCtx := fakes.Owner(ctx, tenant.Context{OrgID: sys.ID, UserID: owner.ID})
	_, err = srv.Orgs.AddMember(ownerCtx, sys.ID, member.ID, org.RoleMember)
	require.NoError(t, err)
	return srv, sys, ownerCtx, owner, member
}

// TestCustomRoles_SelfHostedSystemOrg proves the only org of a self-hosted deployment takes custom roles.
func TestCustomRoles_SelfHostedSystemOrg(t *testing.T) {
	srv, sys, ownerCtx, _, member := bootSystemOrg(t, config.ModeSelfhosted)

	allowed, err := srv.Roles.Allowed(ownerCtx)
	require.NoError(t, err)
	assert.True(t, allowed)
	r, err := srv.Roles.Create(ownerCtx, "Viewer", "", []authz.Permission{authz.WalletsRead})
	require.NoError(t, err)
	require.NoError(t, srv.Orgs.SetRole(ownerCtx, sys.ID, member.ID, string(org.RoleCustom), r.ID))
	m, err := srv.Orgs.MembershipOf(ownerCtx, sys.ID, member.ID)
	require.NoError(t, err)
	assert.Equal(t, org.RoleCustom, m.Role)
	assert.Equal(t, r.ID, m.RoleID)
}

// TestCustomRoles_CloudStaffOrgRefused proves the cloud system org, whose built-in roles decide staff, refuses custom roles while a customer org keeps them.
func TestCustomRoles_CloudStaffOrgRefused(t *testing.T) {
	srv, _, ownerCtx, owner, _ := bootSystemOrg(t, config.ModeCloud)

	allowed, err := srv.Roles.Allowed(ownerCtx)
	require.NoError(t, err)
	assert.False(t, allowed)
	_, err = srv.Roles.Create(ownerCtx, "Viewer", "", []authz.Permission{authz.WalletsRead})
	assert.True(t, role.IsSystemOrgError(err), "got %T: %v", err, err)

	customer, err := srv.Orgs.Create(context.Background(), org.CreateRequest{Slug: "acme", Name: "Acme", OwnerID: owner.ID})
	require.NoError(t, err)
	customerCtx := fakes.Owner(context.Background(), tenant.Context{OrgID: customer.ID, UserID: owner.ID})
	allowed, err = srv.Roles.Allowed(customerCtx)
	require.NoError(t, err)
	assert.True(t, allowed)
	_, err = srv.Roles.Create(customerCtx, "Viewer", "", []authz.Permission{authz.WalletsRead})
	require.NoError(t, err)
}
