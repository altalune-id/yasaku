package org_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	"altalune.id/yasaku/internal/apperror"

	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform/authz"
	"altalune.id/yasaku/internal/platform/capabilities"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/role"
	"altalune.id/yasaku/internal/testutil/fakes"
)

type customPerms map[uuid.UUID][]authz.Permission

func (c customPerms) PermissionsOf(_ context.Context, r string, id uuid.UUID) ([]authz.Permission, error) {
	if id == uuid.Nil {
		return authz.Builtin(r), nil
	}
	p, ok := c[id]
	if !ok {
		return nil, &role.NotFoundError{ID: id.String()}
	}
	return p, nil
}

// TestSetRole_CustomRoleCoversGuard proves the escalation guard reads a custom role's permissions, for both the new role and the target's current one.
func TestSetRole_CustomRoleCoversGuard(t *testing.T) {
	clerk, lead, reader := uuid.New(), uuid.New(), uuid.New()
	perms := customPerms{
		reader: {authz.WalletsRead},
		clerk:  {authz.WalletsRead, authz.WalletsWrite},
		lead:   {authz.MembersWrite, authz.WalletsRead},
	}
	store := fakes.NewOrg()
	unexpected := func(_ context.Context, msg string, cause error, _ ...any) *apperror.AppError {
		return apperror.New("yasaku.unexpected", msg, codes.Internal).WithCause(cause)
	}
	svc := org.NewService(store, capabilities.Capabilities{OrgCreation: true}, slog.New(slog.NewTextHandler(io.Discard, nil)), unexpected,
		fakes.UnitOfWork, org.WithPermissions(perms))
	o, err := svc.Create(context.Background(), org.CreateRequest{Slug: "acme", Name: "Acme", OwnerID: uuid.New()})
	require.NoError(t, err)
	seat := func(r org.Role, id uuid.UUID) uuid.UUID {
		u := uuid.New()
		require.NoError(t, store.SaveMembership(context.Background(), &org.Membership{OrgID: o.ID, UserID: u, Role: r, RoleID: id}))
		return u
	}
	readerHolder, clerkHolder := seat(org.RoleCustom, reader), seat(org.RoleCustom, clerk)
	narrow := fakes.Allow(context.Background(), tenant.Context{OrgID: o.ID, UserID: uuid.New()}, authz.MembersWrite, authz.WalletsRead)

	err = svc.SetRole(narrow, o.ID, readerHolder, string(org.RoleCustom), clerk)
	assert.True(t, authz.IsEscalationError(err), "granting wallets:write the caller lacks, got %T: %v", err, err)
	err = svc.SetRole(narrow, o.ID, clerkHolder, string(org.RoleCustom), reader)
	assert.True(t, authz.IsEscalationError(err), "stripping a custom role the caller does not cover, got %T: %v", err, err)

	require.NoError(t, svc.SetRole(narrow, o.ID, readerHolder, string(org.RoleCustom), lead))
	m, err := store.MembershipOf(context.Background(), o.ID, readerHolder)
	require.NoError(t, err)
	assert.Equal(t, org.RoleCustom, m.Role)
	assert.Equal(t, lead, m.RoleID)

	admin := fakes.Allow(context.Background(), tenant.Context{OrgID: o.ID, UserID: uuid.New()}, authz.Builtin(string(org.RoleAdmin))...)
	for _, bad := range []struct {
		role string
		id   uuid.UUID
	}{{string(org.RoleCustom), uuid.Nil}, {string(org.RoleMember), lead}, {string(org.RoleCustom), uuid.New()}} {
		err = svc.SetRole(admin, o.ID, clerkHolder, bad.role, bad.id)
		assert.True(t, org.IsInvalidRoleError(err), "%s %s: got %T: %v", bad.role, bad.id, err, err)
	}
}
