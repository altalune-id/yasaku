//go:build integration

package boot

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/testutil/pgtest"
	"altalune.id/yasaku/schema"
)

type pgUowFixture struct {
	uow    func(context.Context, func(context.Context) error) error
	db     *sql.DB
	prefix string
	tc     tenant.Context
}

func (f pgUowFixture) ctx() context.Context {
	return tenant.Into(context.Background(), f.tc)
}

func newPgUowFixture(t *testing.T) pgUowFixture {
	t.Helper()
	h := pgtest.New(t)
	sqlDB := h.OpenDB(t)

	cfg := config.Defaults()
	cfg.DB.Driver = db.DriverPostgres
	cfg.DB.DSN = h.DSN
	cfg.DB.Schema = h.Schema
	cfg.DB.AllowBypassRLS = true
	require.NoError(t, schema.MigrateUp(t.Context(), sqlDB, cfg))

	prefix := cfg.DB.TablePrefix
	userID, orgID, projID := uuid.New(), uuid.New(), uuid.New()
	now := time.Now().UTC()
	_, err := sqlDB.ExecContext(t.Context(),
		"INSERT INTO "+prefix+"users (id, email, name, avatar_url, is_admin, created_at, updated_at) VALUES ($1, $2, '', '', false, $3, $3)",
		userID, userID.String()+"@x.co", now)
	require.NoError(t, err)
	_, err = sqlDB.ExecContext(t.Context(),
		"INSERT INTO "+prefix+"orgs (id, slug, name, created_by, created_at, updated_at) VALUES ($1, $2, 'Acme', $3, $4, $4)",
		orgID, orgID.String()[:8], userID, now)
	require.NoError(t, err)
	_, err = sqlDB.ExecContext(t.Context(),
		"INSERT INTO "+prefix+"projects (id, org_id, slug, name, created_by, created_at, updated_at) VALUES ($1, $2, $3, 'Cash', $4, $5, $5)",
		projID, orgID, projID.String()[:8], userID, now)
	require.NoError(t, err)

	pool := db.Pool{W: sqlDB, R: sqlDB}
	return pgUowFixture{
		uow:    unitOfWork(cfg.DB, pool, tenant.NewPgConn(sqlDB)),
		db:     sqlDB,
		prefix: prefix,
		tc:     tenant.Context{OrgID: orgID, ProjectID: projID, UserID: userID},
	}
}

func TestUnitOfWork_Postgres_IsARealTenantedTransaction(t *testing.T) {
	f := newPgUowFixture(t)

	var sawTx bool
	var scopedOrg string
	err := f.uow(f.ctx(), func(ctx context.Context) error {
		tx, ok := db.CurrentTx(ctx)
		sawTx = ok
		if !ok {
			return errors.New("no transaction on the context")
		}
		return tx.QueryRowContext(ctx, "SELECT current_setting('app.current_org_id', true)").Scan(&scopedOrg)
	})
	require.NoError(t, err)
	assert.True(t, sawTx, "the unit of work must expose a real transaction on the context")
	assert.Equal(t, f.tc.OrgID.String(), scopedOrg, "RLS must see the request's org inside the transaction")
}

func TestUnitOfWork_Postgres_RollsBackOnError(t *testing.T) {
	f := newPgUowFixture(t)
	walletID := uuid.New()
	boom := errors.New("boom")

	err := f.uow(f.ctx(), func(ctx context.Context) error {
		tx, ok := db.CurrentTx(ctx)
		require.True(t, ok)
		_, execErr := tx.ExecContext(ctx,
			"INSERT INTO "+f.prefix+"wallets (id, org_id, project_id, name, kind, provider, currency, exclude_from_total, archived_at, created_at, updated_at) "+
				"VALUES ($1, $2, $3, 'Rollback', 'cash', '', 'IDR', false, NULL, $4, $4)",
			walletID, f.tc.OrgID, f.tc.ProjectID, time.Now().UTC())
		require.NoError(t, execErr)
		return boom
	})
	require.ErrorIs(t, err, boom)

	var n int
	require.NoError(t, f.db.QueryRowContext(t.Context(),
		"SELECT count(*) FROM "+f.prefix+"wallets WHERE id = $1", walletID).Scan(&n))
	assert.Equal(t, 0, n, "a pass-through unit of work would have left the row behind")
}

func TestUnitOfWork_Postgres_CommitsOnSuccess(t *testing.T) {
	f := newPgUowFixture(t)
	walletID := uuid.New()

	err := f.uow(f.ctx(), func(ctx context.Context) error {
		tx, ok := db.CurrentTx(ctx)
		require.True(t, ok)
		_, execErr := tx.ExecContext(ctx,
			"INSERT INTO "+f.prefix+"wallets (id, org_id, project_id, name, kind, provider, currency, exclude_from_total, archived_at, created_at, updated_at) "+
				"VALUES ($1, $2, $3, 'Committed', 'cash', '', 'IDR', false, NULL, $4, $4)",
			walletID, f.tc.OrgID, f.tc.ProjectID, time.Now().UTC())
		return execErr
	})
	require.NoError(t, err)

	var n int
	require.NoError(t, f.db.QueryRowContext(t.Context(),
		"SELECT count(*) FROM "+f.prefix+"wallets WHERE id = $1", walletID).Scan(&n))
	assert.Equal(t, 1, n)
}

func TestUnitOfWork_Postgres_RefusesAContextWithoutTenantScope(t *testing.T) {
	f := newPgUowFixture(t)

	ran := false
	err := f.uow(context.Background(), func(context.Context) error {
		ran = true
		return nil
	})
	require.Error(t, err, "the postgres branch cannot set the org GUC without a tenant scope")
	assert.False(t, ran, "the body must not run when the scope is missing")
}
