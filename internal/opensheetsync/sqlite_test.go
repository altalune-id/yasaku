package opensheetsync_test

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/opensheetsync"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/db"
	sqliteent "altalune.id/yasaku/internal/platform/db/entity/sqlite"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/testutil/fakes"
	"altalune.id/yasaku/schema"
)

func TestSQLiteStore_Contract(t *testing.T) {
	runStoreContract(t, func(t *testing.T) storeFixture {
		f, _ := newSQLiteFixture(t)
		return f
	})
}

func TestFakeStore_Contract(t *testing.T) {
	runStoreContract(t, func(t *testing.T) storeFixture {
		f := fakes.NewOpensheetSync()
		tc := tenant.Context{OrgID: uuid.New(), ProjectID: uuid.New(), UserID: uuid.New()}
		return storeFixture{
			store: f, tc: tc, sibling: uuid.New(),
			other: tenant.Context{OrgID: uuid.New(), ProjectID: uuid.New(), UserID: uuid.New()},
			wallet: func(_ *testing.T, tc tenant.Context) uuid.UUID {
				id := uuid.New()
				f.SeedEntity(tc.OrgID, tc.ProjectID, opensheetsync.EntityWallet, id)
				return id
			},
			category: func(_ *testing.T, tc tenant.Context) uuid.UUID {
				id := uuid.New()
				f.SeedEntity(tc.OrgID, tc.ProjectID, opensheetsync.EntityCategory, id)
				return id
			},
			transaction: func(_ *testing.T, tc tenant.Context, w, to, cat uuid.UUID) uuid.UUID {
				id := uuid.New()
				f.SeedTransaction(tc.OrgID, tc.ProjectID, id, w, to, cat)
				return id
			},
			remove: func(_ *testing.T, tc tenant.Context, e opensheetsync.Entity, id uuid.UUID) {
				f.DropEntity(tc.OrgID, tc.ProjectID, e, id)
			},
		}
	})
}

func TestSQLite_TheReconcilerDrainsWhatAnOutageOrALostKickLeftDirty(t *testing.T) {
	f, _ := newSQLiteFixture(t)
	runReconcileScenario(t, f)
}

// NOTE: a t.TempDir() file through db.OpenPool, never ":memory:": the foreign_keys pragma must reach every pooled connection, and the pool's unit-of-work handle begins IMMEDIATE as in production.
func newSQLiteFixture(t *testing.T) (storeFixture, db.Pool) {
	t.Helper()
	cfg := config.Defaults()
	cfg.DB.Driver = db.DriverSQLite
	cfg.DB.DSN = filepath.Join(t.TempDir(), "opensheetsync.db")
	pool, err := db.OpenPool(t.Context(), cfg.DB, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })
	sqlDB := pool.W
	require.NoError(t, schema.MigrateUp(t.Context(), sqlDB, cfg))
	prefix := cfg.DB.TablePrefix
	tc, sibling := seedSQLiteTenant(t, sqlDB, prefix)
	other, _ := seedSQLiteTenant(t, sqlDB, prefix)
	now := sqliteent.SQLiteTime(time.Now())
	exec := func(t *testing.T, q string, args ...any) {
		t.Helper()
		_, err := sqlDB.Exec(q, args...)
		require.NoError(t, err)
	}
	return storeFixture{
		store: opensheetsync.NewStore(db.DBConfig{Driver: db.DriverSQLite, TablePrefix: prefix}, pool, nil),
		tc:    tc, other: other, sibling: sibling,
		wallet: func(t *testing.T, tc tenant.Context) uuid.UUID {
			id := uuid.New()
			exec(t, "INSERT INTO "+prefix+"wallets (id, org_id, project_id, name, kind, currency, created_at, updated_at) VALUES (?, ?, ?, ?, 'cash', 'IDR', ?, ?)",
				id.String(), tc.OrgID.String(), tc.ProjectID.String(), "w-"+id.String()[:8], now, now)
			return id
		},
		category: func(t *testing.T, tc tenant.Context) uuid.UUID {
			id := uuid.New()
			exec(t, "INSERT INTO "+prefix+"categories (id, org_id, project_id, name, kind, created_at, updated_at) VALUES (?, ?, ?, ?, 'expense', ?, ?)",
				id.String(), tc.OrgID.String(), tc.ProjectID.String(), "c-"+id.String()[:8], now, now)
			return id
		},
		transaction: func(t *testing.T, tc tenant.Context, w, to, cat uuid.UUID) uuid.UUID {
			id := uuid.New()
			kind, toArg, catArg := "expense", any(nil), any(nil)
			if to != uuid.Nil {
				kind, toArg = "transfer", to.String()
			}
			if cat != uuid.Nil {
				catArg = cat.String()
			}
			exec(t, "INSERT INTO "+prefix+"transactions (id, org_id, project_id, wallet_id, to_wallet_id, kind, amount_minor, currency, category_id, occurred_at, created_by, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, 100, 'IDR', ?, ?, ?, ?, ?)",
				id.String(), tc.OrgID.String(), tc.ProjectID.String(), w.String(), toArg, kind, catArg, now, tc.UserID.String(), now, now)
			return id
		},
		remove: func(t *testing.T, tc tenant.Context, e opensheetsync.Entity, id uuid.UUID) {
			exec(t, "DELETE FROM "+prefix+entityTable(e)+" WHERE id = ? AND org_id = ?", id.String(), tc.OrgID.String())
		},
	}, pool
}

func seedSQLiteTenant(t *testing.T, sqlDB *sql.DB, prefix string) (tenant.Context, uuid.UUID) {
	t.Helper()
	userID, orgID, projID, sibling := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	now := sqliteent.SQLiteTime(time.Now())
	_, err := sqlDB.Exec("INSERT INTO "+prefix+"users (id, email, name, avatar_url, is_admin, created_at, updated_at) VALUES (?, ?, '', '', 0, ?, ?)",
		userID.String(), userID.String()+"@example.com", now, now)
	require.NoError(t, err)
	_, err = sqlDB.Exec("INSERT INTO "+prefix+"orgs (id, slug, name, created_by, created_at, updated_at) VALUES (?, ?, 'Org', ?, ?, ?)",
		orgID.String(), orgID.String()[:8], userID.String(), now, now)
	require.NoError(t, err)
	for _, p := range []uuid.UUID{projID, sibling} {
		_, err = sqlDB.Exec("INSERT INTO "+prefix+"projects (id, org_id, slug, name, created_by, created_at, updated_at) VALUES (?, ?, ?, 'P', ?, ?, ?)",
			p.String(), orgID.String(), p.String()[:8], userID.String(), now, now)
		require.NoError(t, err)
	}
	return tenant.Context{OrgID: orgID, ProjectID: projID, UserID: userID}, sibling
}

func entityTable(e opensheetsync.Entity) string {
	switch e {
	case opensheetsync.EntityTransaction:
		return "transactions"
	case opensheetsync.EntityCategory:
		return "categories"
	}
	return "wallets"
}
