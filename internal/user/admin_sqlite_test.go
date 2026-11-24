package user_test

import (
	"database/sql"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pdb "altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/user"
)

func TestSQLite_StampSignIn(t *testing.T) {
	t.Parallel()
	db, cfg := openMemSQLite(t)
	assertStampSignIn(t, user.NewStore(cfg, pdb.Pool{W: db, R: db}))
}

func TestSQLite_AdminListOrgs_ReturnsCountsNotLedgerData(t *testing.T) {
	t.Parallel()
	db, cfg := openMemSQLite(t)
	assertAdminLists(t, user.NewStore(cfg, pdb.Pool{W: db, R: db}), db, cfg.TablePrefix)
}

func assertStampSignIn(t *testing.T, store user.Store) {
	t.Helper()
	ctx := t.Context()
	u, err := user.New("stamp@example.com", "Stamp", user.SourceLocal)
	require.NoError(t, err)
	require.NoError(t, store.Save(ctx, u))

	fresh, err := store.ByID(ctx, u.ID)
	require.NoError(t, err)
	assert.Nil(t, fresh.LastSignInAt, "a user who never signed in has no stamp")

	at := time.Date(2026, 11, 24, 8, 30, 0, 0, time.UTC)
	require.NoError(t, store.StampSignIn(ctx, u.ID, at))
	got, err := store.ByID(ctx, u.ID)
	require.NoError(t, err)
	require.NotNil(t, got.LastSignInAt)
	assert.True(t, at.Equal(*got.LastSignInAt), "want %s, got %s", at, got.LastSignInAt)

	require.NoError(t, store.Save(ctx, got), "a later Save must not clear the stamp")
	again, err := store.ByID(ctx, u.ID)
	require.NoError(t, err)
	require.NotNil(t, again.LastSignInAt)

	err = store.StampSignIn(ctx, uuid.New(), at)
	assert.True(t, user.IsNotFoundError(err), "want NotFoundError, got %T: %v", err, err)
}

func assertAdminLists(t *testing.T, store user.Store, db *sql.DB, qualifiedPrefix string) {
	t.Helper()
	ctx := t.Context()

	assert.ElementsMatch(t,
		[]string{"ID", "Slug", "Name", "Plan", "CreatedAt", "MemberCount", "ProjectCount", "LastActive"},
		fieldNames(user.AdminOrg{}), "staff see counts, never ledger data")
	assert.ElementsMatch(t,
		[]string{"ID", "Email", "Name", "CreatedAt", "LastSignInAt", "SuspendedAt", "OrgCount"},
		fieldNames(user.AdminUser{}))

	alice, err := user.New("alice-admin@example.com", "Alice", user.SourceLocal)
	require.NoError(t, err)
	bob, err := user.New("bob-admin@example.com", "Bob", user.SourceLocal)
	require.NoError(t, err)
	require.NoError(t, store.Save(ctx, alice))
	require.NoError(t, store.Save(ctx, bob))
	signedIn := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	require.NoError(t, store.StampSignIn(ctx, bob.ID, signedIn))

	acme, quiet := uuid.New(), uuid.New()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	exec := func(q string) {
		t.Helper()
		_, err := db.ExecContext(ctx, q)
		require.NoError(t, err, q)
	}
	for _, o := range []struct {
		id   uuid.UUID
		slug string
	}{{acme, "acme-admin"}, {quiet, "quiet-admin"}} {
		exec(fmt.Sprintf(`INSERT INTO %sorgs (id, slug, name, system, created_by, created_at, updated_at) VALUES ('%s', '%s', '%s', '0', '%s', '%s', '%s')`,
			qualifiedPrefix, o.id, o.slug, o.slug, alice.ID, now, now))
	}
	for _, m := range []struct {
		org, user uuid.UUID
		role      string
	}{{acme, alice.ID, "owner"}, {acme, bob.ID, "member"}, {quiet, alice.ID, "owner"}} {
		exec(fmt.Sprintf(`INSERT INTO %smemberships (id, org_id, user_id, role, system, created_at) VALUES ('%s', '%s', '%s', '%s', '0', '%s')`,
			qualifiedPrefix, uuid.New(), m.org, m.user, m.role, now))
	}
	for _, slug := range []string{"one", "two", "three"} {
		exec(fmt.Sprintf(`INSERT INTO %sprojects (id, org_id, slug, name, system, created_by, created_at, updated_at) VALUES ('%s', '%s', '%s', '%s', '0', '%s', '%s', '%s')`,
			qualifiedPrefix, uuid.New(), acme, slug, slug, alice.ID, now, now))
	}

	orgs, err := store.AdminListOrgs(ctx)
	require.NoError(t, err)
	byID := map[uuid.UUID]user.AdminOrg{}
	for _, o := range orgs {
		byID[o.ID] = o
	}
	require.Contains(t, byID, acme)
	require.Contains(t, byID, quiet)
	assert.Equal(t, "acme-admin", byID[acme].Slug)
	assert.Equal(t, "first", byID[acme].Plan)
	assert.EqualValues(t, 2, byID[acme].MemberCount)
	assert.EqualValues(t, 3, byID[acme].ProjectCount)
	require.NotNil(t, byID[acme].LastActive, "an org is as active as its latest member sign-in")
	assert.True(t, signedIn.Equal(*byID[acme].LastActive))
	assert.EqualValues(t, 1, byID[quiet].MemberCount)
	assert.Zero(t, byID[quiet].ProjectCount)
	assert.Nil(t, byID[quiet].LastActive)

	users, err := store.AdminListUsers(ctx)
	require.NoError(t, err)
	byUser := map[uuid.UUID]user.AdminUser{}
	for _, u := range users {
		byUser[u.ID] = u
	}
	require.Contains(t, byUser, alice.ID)
	assert.Equal(t, "alice-admin@example.com", byUser[alice.ID].Email)
	assert.EqualValues(t, 2, byUser[alice.ID].OrgCount)
	assert.Nil(t, byUser[alice.ID].LastSignInAt)
	assert.Nil(t, byUser[alice.ID].SuspendedAt)
	require.NotNil(t, byUser[bob.ID].LastSignInAt)
	assert.True(t, signedIn.Equal(*byUser[bob.ID].LastSignInAt))
	assert.EqualValues(t, 1, byUser[bob.ID].OrgCount)
}

func fieldNames(v any) []string {
	rt := reflect.TypeOf(v)
	out := make([]string, 0, rt.NumField())
	for i := range rt.NumField() {
		out = append(out, rt.Field(i).Name)
	}
	return out
}

func TestSQLite_SetSuspended(t *testing.T) {
	t.Parallel()
	db, cfg := openMemSQLite(t)
	assertSetSuspended(t, user.NewStore(cfg, pdb.Pool{W: db, R: db}))
}

func assertSetSuspended(t *testing.T, store user.Store) {
	t.Helper()
	ctx := t.Context()
	u, err := user.New("suspend-store@example.com", "S", user.SourceOIDC)
	require.NoError(t, err)
	require.NoError(t, store.Save(ctx, u))

	at := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)
	require.NoError(t, store.SetSuspended(ctx, u.ID, &at))
	got, err := store.ByID(ctx, u.ID)
	require.NoError(t, err)
	require.NotNil(t, got.SuspendedAt)
	assert.True(t, at.Equal(*got.SuspendedAt))

	require.NoError(t, store.Save(ctx, got), "a later Save must not lift the suspension")
	got, err = store.ByEmail(ctx, u.Email)
	require.NoError(t, err)
	require.NotNil(t, got.SuspendedAt)

	require.NoError(t, store.SetSuspended(ctx, u.ID, nil))
	got, err = store.ByID(ctx, u.ID)
	require.NoError(t, err)
	assert.Nil(t, got.SuspendedAt)

	assert.True(t, user.IsNotFoundError(store.SetSuspended(ctx, uuid.New(), &at)))
}
