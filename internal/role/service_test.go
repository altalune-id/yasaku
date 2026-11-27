package role_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/limits"
	"altalune.id/yasaku/internal/platform/authz"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/role"
	"altalune.id/yasaku/internal/testutil/fakes"
)

type orgs struct{ system map[uuid.UUID]bool }

func (o orgs) IsStaffOrg(_ context.Context, id uuid.UUID) (bool, error) { return o.system[id], nil }

type orgNames map[uuid.UUID]string

func (n orgNames) OrgName(_ context.Context, id uuid.UUID) (string, error) { return n[id], nil }

type fixture struct {
	svc   *role.Service
	store *fakes.Role
	audit *fakes.Audit
	orgs  orgs
	tc    tenant.Context
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	f := fixture{store: fakes.NewRole(), audit: &fakes.Audit{}, tc: tenant.Context{OrgID: uuid.New(), UserID: uuid.New()}}
	f.orgs = orgs{system: map[uuid.UUID]bool{}}
	lstore := fakes.NewLimits()
	unexpected := func(_ context.Context, op string, err error, _ ...any) *apperror.AppError {
		t.Fatalf("unexpected %s: %v", op, err)
		return nil
	}
	f.svc = role.NewService(f.store, f.orgs, slog.New(slog.NewTextHandler(io.Discard, nil)), unexpected, fakes.UnitOfWork,
		role.WithLimits(limits.NewService(lstore, config.ModeCloud), orgNames{f.tc.OrgID: "Acme"}), role.WithAudit(f.audit))
	return f
}

func (f fixture) owner() context.Context { return fakes.Owner(context.Background(), f.tc) }

func TestRole_CoversGuard(t *testing.T) {
	f := newFixture(t)
	narrow := fakes.Allow(context.Background(), f.tc, authz.RolesWrite, authz.MembersWrite, authz.WalletsRead)

	_, err := f.svc.Create(narrow, "Writer", "", []authz.Permission{authz.WalletsRead, authz.WalletsWrite})
	assert.True(t, authz.IsEscalationError(err), "create past the caller's own role: got %T: %v", err, err)

	r, err := f.svc.Create(narrow, "Reader", "", []authz.Permission{authz.WalletsRead})
	require.NoError(t, err)
	_, err = f.svc.Update(narrow, r.ID, "Reader", "", []authz.Permission{authz.WalletsRead, authz.LedgerSettings}, 0)
	assert.True(t, authz.IsEscalationError(err), "update past the caller's own role: got %T: %v", err, err)

	noRoles := fakes.Allow(context.Background(), f.tc, authz.Builtin("admin")...)
	_, err = f.svc.Create(noRoles, "Other", "", []authz.Permission{authz.WalletsRead})
	assert.True(t, authz.IsDeniedError(err), "create needs roles:write: got %T: %v", err, err)
	assert.True(t, authz.IsDeniedError(f.svc.Delete(noRoles, r.ID)), "delete needs roles:write")
	_, err = f.svc.List(fakes.Allow(context.Background(), f.tc, authz.WalletsRead))
	assert.True(t, authz.IsDeniedError(err), "list needs members:write: got %T: %v", err, err)

	got, err := f.svc.List(noRoles)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, []authz.Permission{authz.WalletsRead}, got[0].Permissions)
}

func TestRole_InvalidPermission(t *testing.T) {
	f := newFixture(t)
	for _, p := range []authz.Permission{"*", "wallets:*", "nope:read", ""} {
		_, err := f.svc.Create(f.owner(), "Bad", "", []authz.Permission{authz.WalletsRead, p})
		assert.True(t, role.IsInvalidPermissionError(err), "%q: got %T: %v", p, err, err)
	}
	assert.Empty(t, f.audit.Actions())
}

func TestRole_NameUnique(t *testing.T) {
	f := newFixture(t)
	r, err := f.svc.Create(f.owner(), "  Viewer ", "", []authz.Permission{authz.WalletsRead})
	require.NoError(t, err)
	assert.Equal(t, "Viewer", r.Name)

	_, err = f.svc.Create(f.owner(), "viewer", "", []authz.Permission{authz.WalletsRead})
	assert.True(t, role.IsAlreadyExistsError(err), "names are unique ignoring case: got %T: %v", err, err)

	for _, name := range []string{"", "   ", strings.Repeat("a", 41)} {
		_, err = f.svc.Create(f.owner(), name, "", []authz.Permission{authz.WalletsRead})
		assert.True(t, role.IsInvalidNameError(err), "%q: got %T: %v", name, err, err)
	}
	_, err = f.svc.Create(f.owner(), strings.Repeat("é", 40), "", []authz.Permission{authz.WalletsRead})
	require.NoError(t, err, "40 characters, not bytes")

	other, err := f.svc.Create(f.owner(), "Auditor", "", []authz.Permission{authz.WalletsRead})
	require.NoError(t, err)
	_, err = f.svc.Update(f.owner(), other.ID, "VIEWER", "", []authz.Permission{authz.WalletsRead}, 0)
	assert.True(t, role.IsAlreadyExistsError(err), "rename onto another role: got %T: %v", err, err)
	_, err = f.svc.Update(f.owner(), other.ID, "Auditor", "", []authz.Permission{authz.WalletsRead}, other.Version+5)
	assert.True(t, role.IsStaleVersionError(err), "a stale version is refused: got %T: %v", err, err)
}

func TestRole_DeleteInUse(t *testing.T) {
	f := newFixture(t)
	r, err := f.svc.Create(f.owner(), "Viewer", "", []authz.Permission{authz.WalletsRead})
	require.NoError(t, err)
	f.store.Hold(r.ID)

	assert.True(t, role.IsInUseError(f.svc.Delete(f.owner(), r.ID)))
	f.store.Release(r.ID)
	require.NoError(t, f.svc.Delete(f.owner(), r.ID))
	assert.True(t, role.IsNotFoundError(f.svc.Delete(f.owner(), r.ID)))
	assert.True(t, role.IsNotFoundError(f.svc.Delete(f.owner(), uuid.New())))
}

func TestRole_RefusedInSystemOrg(t *testing.T) {
	f := newFixture(t)
	r, err := f.svc.Create(f.owner(), "Viewer", "", []authz.Permission{authz.WalletsRead})
	require.NoError(t, err)
	f.orgs.system[f.tc.OrgID] = true

	_, err = f.svc.Create(f.owner(), "Other", "", []authz.Permission{authz.WalletsRead})
	assert.True(t, role.IsSystemOrgError(err), "create: got %T: %v", err, err)
	_, err = f.svc.Update(f.owner(), r.ID, "Viewer", "", []authz.Permission{authz.WalletsRead}, 0)
	assert.True(t, role.IsSystemOrgError(err), "update: got %T: %v", err, err)
	assert.True(t, role.IsSystemOrgError(f.svc.Delete(f.owner(), r.ID)), "delete")
}

func TestRole_LimitReached(t *testing.T) {
	f := newFixture(t)
	for i := range 10 {
		_, err := f.svc.Create(f.owner(), "Role "+string(rune('A'+i)), "", []authz.Permission{authz.WalletsRead})
		require.NoError(t, err)
	}
	_, err := f.svc.Create(f.owner(), "Eleventh", "", []authz.Permission{authz.WalletsRead})
	re, ok := errors.AsType[*limits.ReachedError](err)
	require.True(t, ok, "the eleventh role hits org.custom_roles: got %T: %v", err, err)
	assert.Equal(t, limits.OrgCustomRoles, re.Key)
	assert.Equal(t, "Acme", re.Name)
}

func TestRole_Audited(t *testing.T) {
	f := newFixture(t)
	r, err := f.svc.Create(f.owner(), "Viewer", "Reads only", []authz.Permission{authz.WalletsRead})
	require.NoError(t, err)
	r, err = f.svc.Update(f.owner(), r.ID, "Reader", "Reads only", []authz.Permission{authz.WalletsRead, authz.CategoriesRead}, r.Version)
	require.NoError(t, err)
	assert.Equal(t, int64(2), r.Version)
	require.NoError(t, f.svc.Delete(f.owner(), r.ID))

	recs := f.audit.Records()
	require.Len(t, recs, 3)
	for i, action := range []string{"role.created", "role.updated", "role.deleted"} {
		assert.Equal(t, action, recs[i].Action)
		assert.True(t, recs[i].InTx, "%s is recorded in the write's unit of work", action)
		assert.Equal(t, f.tc.OrgID, recs[i].OrgID)
		assert.Equal(t, "role", recs[i].TargetKind)
		assert.Equal(t, r.ID.String(), recs[i].TargetID)
	}
	assert.Equal(t, "Reader", recs[1].Detail["name"])
}

func TestRole_ReadsForTheConsole(t *testing.T) {
	f := newFixture(t)
	r, err := f.svc.Create(f.owner(), "Viewer", "", []authz.Permission{authz.WalletsRead})
	require.NoError(t, err)
	f.store.Hold(r.ID)

	got, err := f.svc.Get(f.owner(), r.ID)
	require.NoError(t, err)
	assert.Equal(t, "Viewer", got.Name)
	_, err = f.svc.Get(f.owner(), uuid.New())
	assert.True(t, role.IsNotFoundError(err))
	held, err := f.svc.HeldBy(f.owner())
	require.NoError(t, err)
	assert.Equal(t, 1, held[r.ID])

	member := fakes.Allow(context.Background(), f.tc, authz.Builtin("member")...)
	_, err = f.svc.Get(member, r.ID)
	assert.True(t, authz.IsDeniedError(err), "Get needs members:write, got %T: %v", err, err)
	_, err = f.svc.HeldBy(member)
	assert.True(t, authz.IsDeniedError(err), "HeldBy needs members:write, got %T: %v", err, err)

	require.NoError(t, f.svc.CreateAllowed(f.owner()))
	for i := range 9 {
		_, err := f.svc.Create(f.owner(), "Role "+string(rune('A'+i)), "", []authz.Permission{authz.WalletsRead})
		require.NoError(t, err)
	}
	re, ok := errors.AsType[*limits.ReachedError](f.svc.CreateAllowed(f.owner()))
	require.True(t, ok, "CreateAllowed reports the limit Create would refuse")
	assert.Equal(t, "Acme", re.Name, "the refusal names the org")
}

// TestRole_UpdateCoversTheOldPermissions proves an editor cannot strip permissions they do not hold from a role others may hold.
func TestRole_UpdateCoversTheOldPermissions(t *testing.T) {
	f := newFixture(t)
	r, err := f.svc.Create(f.owner(), "Clerk", "", []authz.Permission{authz.WalletsRead, authz.WalletsWrite})
	require.NoError(t, err)
	narrow := fakes.Allow(context.Background(), f.tc, authz.RolesWrite, authz.WalletsRead)

	_, err = f.svc.Update(narrow, r.ID, "Clerk", "", []authz.Permission{authz.WalletsRead}, 0)
	esc, ok := errors.AsType[*authz.EscalationError](err)
	require.True(t, ok, "stripping wallets:write the editor lacks, got %T: %v", err, err)
	assert.Equal(t, "Clerk", esc.Role)
	got, err := f.svc.Get(f.owner(), r.ID)
	require.NoError(t, err)
	assert.Equal(t, []authz.Permission{authz.WalletsRead, authz.WalletsWrite}, got.Permissions, "the role is unchanged")
}
