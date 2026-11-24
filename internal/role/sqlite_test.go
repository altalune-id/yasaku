package role_test

import (
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/db"
	sqliteent "altalune.id/yasaku/internal/platform/db/entity/sqlite"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/role"
	"altalune.id/yasaku/schema"
)

func TestSQLiteStore_Contract(t *testing.T) {
	runStoreContract(t, func(t *testing.T) storeFixture {
		t.Helper()
		sqlDB, err := sql.Open("sqlite", ":memory:")
		require.NoError(t, err)
		sqlDB.SetMaxOpenConns(1)
		t.Cleanup(func() { _ = sqlDB.Close() })
		_, err = sqlDB.Exec("PRAGMA foreign_keys = ON;")
		require.NoError(t, err)
		cfg := config.Defaults()
		require.NoError(t, schema.MigrateUp(t.Context(), sqlDB, cfg))
		prefix := cfg.DB.TablePrefix
		now := sqliteent.SQLiteTime(time.Now())
		userID := uuid.New()
		_, err = sqlDB.Exec("INSERT INTO "+prefix+"users (id, email, name, avatar_url, is_admin, created_at, updated_at) VALUES (?, ?, '', '', 0, ?, ?)",
			userID.String(), userID.String()+"@x.com", now, now)
		require.NoError(t, err)
		org := func() tenant.Context {
			id := uuid.New()
			_, err := sqlDB.Exec("INSERT INTO "+prefix+"orgs (id, slug, name, created_by, created_at, updated_at) VALUES (?, ?, 'Org', ?, ?, ?)",
				id.String(), id.String()[:8], userID.String(), now, now)
			require.NoError(t, err)
			return tenant.Context{OrgID: id, UserID: userID}
		}
		return storeFixture{
			store: role.NewStore(db.DBConfig{Driver: db.DriverSQLite, TablePrefix: prefix}, db.Pool{W: sqlDB, R: sqlDB}, nil),
			org:   org(), other: org(),
			hold: func(t *testing.T, orgID, roleID uuid.UUID) {
				t.Helper()
				_, err := sqlDB.Exec("INSERT INTO "+prefix+"memberships (id, org_id, user_id, role, role_id, system, created_at) VALUES (?, ?, ?, 'custom', ?, 0, ?)",
					uuid.NewString(), orgID.String(), userID.String(), roleID.String(), now)
				require.NoError(t, err)
			},
		}
	})
}
