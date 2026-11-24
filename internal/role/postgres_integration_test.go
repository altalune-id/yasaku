//go:build integration

package role_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/role"
	"altalune.id/yasaku/internal/testutil/pgtest"
	"altalune.id/yasaku/schema"
)

func TestPostgresStore_Contract(t *testing.T) {
	runStoreContract(t, func(t *testing.T) storeFixture {
		t.Helper()
		h := pgtest.New(t)
		sqlDB := h.OpenDB(t)
		cfg := config.Defaults()
		cfg.DB.Driver = "postgres"
		cfg.DB.DSN = h.DSN
		cfg.DB.Schema = h.Schema
		cfg.DB.AllowBypassRLS = true
		require.NoError(t, schema.MigrateUp(t.Context(), sqlDB, cfg))
		prefix := h.Schema + "." + cfg.DB.TablePrefix
		now := time.Now().UTC()
		userID := uuid.New()
		_, err := sqlDB.ExecContext(t.Context(), "INSERT INTO "+prefix+"users (id, email, name, avatar_url, is_admin, created_at, updated_at) VALUES ($1, $2, '', '', false, $3, $3)",
			userID, userID.String()+"@x.co", now)
		require.NoError(t, err)
		org := func() tenant.Context {
			id := uuid.New()
			_, err := sqlDB.ExecContext(t.Context(), "INSERT INTO "+prefix+"orgs (id, slug, name, created_by, created_at, updated_at) VALUES ($1, $2, 'Org', $3, $4, $4)",
				id, id.String()[:8], userID, now)
			require.NoError(t, err)
			return tenant.Context{OrgID: id, UserID: userID}
		}
		return storeFixture{
			store: role.NewStore(db.DBConfig{Driver: db.DriverPostgres, Schema: h.Schema, TablePrefix: cfg.DB.TablePrefix}, db.Pool{W: sqlDB, R: sqlDB}, tenant.NewPgConn(sqlDB)),
			org:   org(), other: org(),
			hold: func(t *testing.T, orgID, roleID uuid.UUID) {
				t.Helper()
				_, err := sqlDB.ExecContext(t.Context(), "INSERT INTO "+prefix+"memberships (id, org_id, user_id, role, role_id, created_at) VALUES ($1, $2, $3, 'custom', $4, $5)",
					uuid.New(), orgID, userID, roleID, now)
				require.NoError(t, err)
			},
		}
	})
}
