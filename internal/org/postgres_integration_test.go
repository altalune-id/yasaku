//go:build integration

package org_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	apperrorv1 "altalune.id/yasaku/gen/go/apperror/v1"
	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform/capabilities"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/testutil/pgtest"
	"altalune.id/yasaku/schema"
)

func newPostgresStoreForTest(t *testing.T) (store org.Store, tc tenant.Context, prefix string) { //nolint:nonamedreturns // three heterogeneous returns read cleaner named
	t.Helper()
	h := pgtest.New(t)
	sqlDB := h.OpenDB(t)

	cfg := config.Defaults()
	cfg.DB.Driver = "postgres"
	cfg.DB.DSN = h.DSN
	cfg.DB.Schema = h.Schema
	cfg.DB.AllowBypassRLS = true

	require.NoError(t, schema.MigrateUp(t.Context(), sqlDB, cfg))

	pc := tenant.NewPgConn(sqlDB)
	prefix = cfg.DB.TablePrefix
	store = org.NewStore(db.DBConfig{Driver: db.DriverPostgres, Schema: h.Schema, TablePrefix: prefix}, db.Pool{W: sqlDB, R: sqlDB}, pc)

	ownerID := uuid.New()
	now := time.Now().UTC()
	_, err := sqlDB.ExecContext(t.Context(),
		"INSERT INTO "+prefix+"users (id, email, name, avatar_url, is_admin, created_at, updated_at) VALUES ($1, $2, '', '', false, $3, $3)",
		ownerID, ownerID.String()+"@x.co", now,
	)
	require.NoError(t, err)

	// NOTE: OrgID is left for each test to fill in with the org it operates on — the store derives its
	// RLS scope from the context, so a placeholder here would only prove that the superuser bypasses RLS.
	tc = tenant.Context{UserID: ownerID}
	return store, tc, prefix
}

func scopedTo(t *testing.T, tc tenant.Context, orgID uuid.UUID) context.Context {
	t.Helper()
	tc.OrgID = orgID
	return tenant.Into(t.Context(), tc)
}

func TestPostgres_SaveAndLookup(t *testing.T) {
	store, tc, _ := newPostgresStoreForTest(t)

	o, err := org.NewOrg("acme", "Acme", tc.UserID)
	require.NoError(t, err)
	ctx := scopedTo(t, tc, o.ID)
	require.NoError(t, store.Save(ctx, o))

	got, err := store.BySlug(ctx, "acme")
	require.NoError(t, err)
	assert.Equal(t, o.ID, got.ID)
	assert.Equal(t, "Acme", got.Name)
	assert.Equal(t, tc.UserID, got.OwnerID)

	got2, err := store.ByID(ctx, o.ID)
	require.NoError(t, err)
	assert.Equal(t, "acme", got2.Slug)
}

func TestPostgres_NotFound(t *testing.T) {
	store, tc, _ := newPostgresStoreForTest(t)
	ctx := scopedTo(t, tc, uuid.New())

	_, err := store.ByID(ctx, uuid.New())
	assert.True(t, org.IsNotFoundError(err), "ByID: want NotFoundError, got %T: %v", err, err)

	_, err = store.BySlug(ctx, "missing")
	assert.True(t, org.IsNotFoundError(err), "BySlug: want NotFoundError, got %T: %v", err, err)
}

func TestPostgres_SaveIsUpsert(t *testing.T) {
	store, tc, _ := newPostgresStoreForTest(t)

	o, err := org.NewOrg("acme", "Acme", tc.UserID)
	require.NoError(t, err)
	ctx := scopedTo(t, tc, o.ID)
	require.NoError(t, store.Save(ctx, o))

	o.Name = "Acme Corp"
	require.NoError(t, store.Save(ctx, o))

	got, err := store.ByID(ctx, o.ID)
	require.NoError(t, err)
	assert.Equal(t, "Acme Corp", got.Name)
}

func TestPostgres_MembershipRoundTrip(t *testing.T) {
	store, tc, _ := newPostgresStoreForTest(t)

	o, err := org.NewOrg("acme", "Acme", tc.UserID)
	require.NoError(t, err)
	ctx := scopedTo(t, tc, o.ID)
	require.NoError(t, store.Save(ctx, o))

	m, err := org.NewMembership(o.ID, tc.UserID, org.RoleOwner)
	require.NoError(t, err)
	require.NoError(t, store.SaveMembership(ctx, m))

	got, err := store.MembershipOf(ctx, o.ID, tc.UserID)
	require.NoError(t, err)
	assert.Equal(t, org.RoleOwner, got.Role)

	members, err := store.ListMembers(ctx, o.ID)
	require.NoError(t, err)
	assert.Len(t, members, 1)
	assert.Equal(t, tc.UserID, members[0].UserID)

	profiles, err := store.ListMemberProfiles(ctx, o.ID)
	require.NoError(t, err)
	require.Len(t, profiles, 1)
	assert.Contains(t, profiles[0].Email, "@x.co")

	require.NoError(t, store.RemoveMember(ctx, o.ID, tc.UserID))
	_, err = store.MembershipOf(ctx, o.ID, tc.UserID)
	assert.True(t, org.IsMembershipMissingError(err) || org.IsNotFoundError(err), "want missing, got %T: %v", err, err)
}

type failingMembershipStore struct{ org.Store }

func (failingMembershipStore) SaveMembership(context.Context, *org.Membership) error {
	return errors.New("membership insert failed")
}

func TestOrgCreate_RollsBackOnMembershipFailure(t *testing.T) {
	h := pgtest.New(t)
	sqlDB := h.OpenDB(t)
	cfg := config.Defaults()
	cfg.DB.Driver = "postgres"
	cfg.DB.DSN = h.DSN
	cfg.DB.Schema = h.Schema
	cfg.DB.AllowBypassRLS = true
	require.NoError(t, schema.MigrateUp(t.Context(), sqlDB, cfg))

	dbCfg := db.DBConfig{Driver: db.DriverPostgres, Schema: h.Schema, TablePrefix: cfg.DB.TablePrefix}
	pool := db.Pool{W: sqlDB, R: sqlDB}
	pc := tenant.NewPgConn(sqlDB)
	store := org.NewStore(dbCfg, pool, pc)
	svc := org.NewService(failingMembershipStore{store}, capabilities.Capabilities{OrgCreation: true},
		slog.New(slog.NewTextHandler(io.Discard, nil)), func(_ context.Context, msg string, cause error, _ ...any) *apperror.AppError {
			return apperror.New("yasaku.unexpected", msg, codes.Internal, &apperrorv1.ErrorDetail{Code: "yasaku.unexpected"}).WithCause(cause)
		}, tenant.NewUnitOfWork(dbCfg, pool, pc))

	ownerID := uuid.New()
	_, err := sqlDB.ExecContext(t.Context(),
		"INSERT INTO "+cfg.DB.TablePrefix+"users (id, email, name, avatar_url, is_admin, created_at, updated_at) VALUES ($1, $2, '', '', false, now(), now())",
		ownerID, ownerID.String()+"@x.co")
	require.NoError(t, err)

	_, err = svc.Create(t.Context(), org.CreateRequest{Slug: "acme", Name: "Acme", OwnerID: ownerID})
	require.Error(t, err)

	var n int
	require.NoError(t, sqlDB.QueryRowContext(t.Context(), "SELECT count(*) FROM "+cfg.DB.TablePrefix+"orgs WHERE slug = 'acme'").Scan(&n))
	assert.Zero(t, n, "the org row must roll back with its failed membership")
}

func TestPostgres_IsStaff(t *testing.T) {
	store, tc, _ := newPostgresStoreForTest(t)
	sys, err := org.NewOrg("system", "System", tc.UserID)
	require.NoError(t, err)
	sys.System = true
	sysCtx := scopedTo(t, tc, sys.ID)
	require.NoError(t, store.Save(sysCtx, sys))
	m, err := org.NewMembership(sys.ID, tc.UserID, org.RoleAdmin)
	require.NoError(t, err)
	require.NoError(t, store.SaveMembership(sysCtx, m))

	other, err := org.NewOrg("acme", "Acme", tc.UserID)
	require.NoError(t, err)
	otherCtx := scopedTo(t, tc, other.ID)
	require.NoError(t, store.Save(otherCtx, other))

	staff, err := store.IsStaff(otherCtx, tc.UserID)
	require.NoError(t, err)
	assert.True(t, staff, "an admin of the system org is staff, whatever scope the caller holds")
	staff, err = store.IsStaff(otherCtx, uuid.New())
	require.NoError(t, err)
	assert.False(t, staff)
}

// SECURITY: IsStaff runs inside a limit-holding unit of work, so it must answer on that transaction's connection, never wait for a second one.
func TestPostgres_IsStaff_UsesTheUnitOfWorkConnection(t *testing.T) {
	h := pgtest.New(t)
	sqlDB := h.OpenDB(t)
	sqlDB.SetMaxOpenConns(1)
	cfg := config.Defaults()
	cfg.DB.Driver = "postgres"
	cfg.DB.DSN = h.DSN
	cfg.DB.Schema = h.Schema
	cfg.DB.AllowBypassRLS = true
	require.NoError(t, schema.MigrateUp(t.Context(), sqlDB, cfg))
	dbCfg := db.DBConfig{Driver: db.DriverPostgres, Schema: h.Schema, TablePrefix: cfg.DB.TablePrefix}
	pool := db.Pool{W: sqlDB, R: sqlDB}
	pc := tenant.NewPgConn(sqlDB)
	store := org.NewStore(dbCfg, pool, pc)
	creator := uuid.New()
	_, err := sqlDB.ExecContext(t.Context(),
		"INSERT INTO "+cfg.DB.TablePrefix+"users (id, email, name, avatar_url, is_admin, created_at, updated_at) VALUES ($1, $2, '', '', false, now(), now())",
		creator, creator.String()+"@x.co")
	require.NoError(t, err)
	sys, err := org.NewOrg("system", "System", creator)
	require.NoError(t, err)
	sys.System = true
	require.NoError(t, store.Save(tenant.Into(t.Context(), tenant.Context{OrgID: sys.ID}), sys))

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	ctx = tenant.Into(ctx, tenant.Context{OrgID: uuid.New(), UserID: uuid.New()})
	err = tenant.NewUnitOfWork(dbCfg, pool, pc)(ctx, func(ctx context.Context) error {
		_, err := store.IsStaff(ctx, uuid.New())
		return err
	})
	require.NoError(t, err, "with one pooled connection held by the unit of work, a second connection never comes")
}
