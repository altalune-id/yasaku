package user_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/testutil/fakes"
	"altalune.id/yasaku/internal/user"
)

type staffDirectory struct {
	staff     map[uuid.UUID]bool
	systemOrg uuid.UUID
}

func (d staffDirectory) IsStaff(_ context.Context, id uuid.UUID) (bool, error) {
	return d.staff[id], nil
}
func (d staffDirectory) SystemOrgID(context.Context) (uuid.UUID, error) { return d.systemOrg, nil }

type suspendFixture struct {
	svc        *user.Service
	store      *fakes.User
	audit      *fakes.Audit
	dir        staffDirectory
	staff      *user.User
	otherStaff *user.User
	alice      *user.User
}

func newSuspendFixture(t *testing.T) suspendFixture {
	t.Helper()
	store := fakes.NewUser()
	f := suspendFixture{store: store, audit: &fakes.Audit{}}
	for _, u := range []**user.User{&f.staff, &f.otherStaff, &f.alice} {
		nu, err := user.New(uuid.NewString()+"@example.com", "N", user.SourceOIDC)
		require.NoError(t, err)
		require.NoError(t, store.Save(context.Background(), nu))
		*u = nu
	}
	f.dir = staffDirectory{staff: map[uuid.UUID]bool{f.staff.ID: true, f.otherStaff.ID: true}, systemOrg: uuid.New()}
	f.svc = user.NewService(store, user.GenesisConfig{}, newTestLogger(), noopUnexpected(),
		user.WithSuspension(f.dir, fakes.UnitOfWork, f.audit, "help@example.com"))
	return f
}

func (f suspendFixture) as(id uuid.UUID) context.Context {
	return session.PrincipalInto(context.Background(), session.Principal{UserID: id, Source: session.SourceOIDC})
}

func TestSuspend_RefusesStaffAndSelf(t *testing.T) {
	f := newSuspendFixture(t)

	err := f.svc.Suspend(f.as(f.staff.ID), f.staff.ID)
	assert.True(t, user.IsStaffProtectedError(err), "self: got %T: %v", err, err)
	err = f.svc.Suspend(f.as(f.staff.ID), f.otherStaff.ID)
	assert.True(t, user.IsStaffProtectedError(err), "staff: got %T: %v", err, err)

	err = f.svc.Suspend(f.as(f.alice.ID), f.staff.ID)
	assert.True(t, user.IsNotFoundError(err), "a non-staff caller learns nothing: got %T: %v", err, err)
	err = f.svc.Suspend(f.as(f.staff.ID), uuid.New())
	assert.True(t, user.IsNotFoundError(err), "unknown target: got %T: %v", err, err)

	assert.Empty(t, f.audit.Actions(), "a refusal records nothing")
	require.NoError(t, f.svc.RequireActive(context.Background(), f.otherStaff.ID))
}

func TestSuspend_Audited(t *testing.T) {
	f := newSuspendFixture(t)
	ctx := f.as(f.staff.ID)

	require.NoError(t, f.svc.Suspend(ctx, f.alice.ID))
	got, err := f.store.ByID(ctx, f.alice.ID)
	require.NoError(t, err)
	require.NotNil(t, got.SuspendedAt)
	err = f.svc.RequireActive(ctx, f.alice.ID)
	se, ok := err.(*user.SuspendedError) //nolint:errorlint // RequireActive returns the typed error unwrapped
	require.True(t, ok, "got %T: %v", err, err)
	assert.Equal(t, "USR008", se.ToAppError().Code())
	assert.Contains(t, se.ToAppError().Message(), "help@example.com")

	require.NoError(t, f.svc.Unsuspend(ctx, f.alice.ID))
	require.NoError(t, f.svc.RequireActive(ctx, f.alice.ID))

	recs := f.audit.Records()
	require.Len(t, recs, 2)
	for i, action := range []string{"user.suspended", "user.unsuspended"} {
		assert.Equal(t, action, recs[i].Action)
		assert.Equal(t, f.dir.systemOrg, recs[i].OrgID, "suspension is recorded under the system org")
		assert.True(t, recs[i].InTx)
		assert.Equal(t, "user", recs[i].TargetKind)
		assert.Equal(t, f.alice.ID.String(), recs[i].TargetID)
	}
}
