//go:build integration

package period_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/civil"
	"altalune.id/yasaku/internal/period"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/testutil/pgtest"
	"altalune.id/yasaku/schema"
)

func TestPostgres_Period_SaveRejectsSiblingProject(t *testing.T) {
	h := pgtest.New(t)
	sqlDB := h.OpenDB(t)
	cfg := config.Defaults()
	cfg.DB.Driver = "postgres"
	cfg.DB.DSN = h.DSN
	cfg.DB.Schema = h.Schema
	cfg.DB.AllowBypassRLS = true
	require.NoError(t, schema.MigrateUp(t.Context(), sqlDB, cfg))
	prefix := cfg.DB.TablePrefix
	tc := seedPgTenant(t, sqlDB, prefix)
	store := period.NewStore(
		db.DBConfig{Driver: db.DriverPostgres, Schema: h.Schema, TablePrefix: prefix},
		db.Pool{W: sqlDB, R: sqlDB}, tenant.NewPgConn(sqlDB))

	ownerCtx := tenant.Into(t.Context(), tc)
	victim, err := period.New(tc.OrgID, tc.ProjectID, civil.Date{Year: 2026, Month: 8, Day: 25}, "project A's period")
	require.NoError(t, err)
	require.NoError(t, store.Save(ownerCtx, victim))

	siblingProj := uuid.New()
	_, err = sqlDB.ExecContext(t.Context(),
		"INSERT INTO "+prefix+"projects (id, org_id, slug, name, created_by, created_at, updated_at) VALUES ($1, $2, $3, 'Sibling', $4, $5, $5)",
		siblingProj, tc.OrgID, siblingProj.String()[:8], tc.UserID, time.Now().UTC())
	require.NoError(t, err)
	siblingCtx := tenant.Into(t.Context(), tenant.Context{OrgID: tc.OrgID, ProjectID: siblingProj, UserID: tc.UserID})

	attack := *victim
	attack.Name = "Hijacked"
	err = store.Save(siblingCtx, &attack)
	assert.True(t, period.IsNotFoundError(err), "sibling-project Save: got %T: %v", err, err)

	got, err := store.ByID(ownerCtx, victim.ID)
	require.NoError(t, err)
	assert.Equal(t, "project A's period", got.Name)
}
