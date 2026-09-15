package tenant

import (
	"testing"

	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/platform/db"
)

func TestNewOrgReader_PostgresReadsTheDefinerWrapper(t *testing.T) {
	r, ok := NewOrgReader(db.Pool{}, db.DriverPostgres, "public", "yasaku_").(*pgOrgReader)
	require.True(t, ok, "postgres must select the wrapper-backed reader")
	require.Contains(t, r.query, "public.yasaku_list_org_ids() o")
	require.Contains(t, r.query, "ORDER BY o.created_at ASC, o.id ASC",
		"the outer statement must re-order: Postgres may inline the wrapper and drop its internal ORDER BY")
	require.NotContains(t, r.query, "yasaku_orgs",
		"SECURITY: reading yasaku_orgs directly returns zero rows under FORCE row level security")
	require.Empty(t, r.args)
}

func TestNewOrgReader_PostgresDefaultsBlankSchemaToPublic(t *testing.T) {
	r, ok := NewOrgReader(db.Pool{}, db.DriverPostgres, "", "yasaku_").(*pgOrgReader)
	require.True(t, ok)
	require.Contains(t, r.query, "public.yasaku_list_org_ids() o")
}

func TestNewOrgReader_SQLiteReadsTheTable(t *testing.T) {
	r, ok := NewOrgReader(db.Pool{}, db.DriverSQLite, "", "yasaku_").(*sqliteOrgReader)
	require.True(t, ok, "sqlite carries no definer wrappers")
	require.Contains(t, r.query, "yasaku_orgs")
	require.Contains(t, r.query, "ORDER BY orgs.created_at ASC, orgs.id ASC",
		"created_at alone is not a total order")
	require.Empty(t, r.args)
}
