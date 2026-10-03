//go:build integration

package schema_test

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/testutil/pgtest"
	"altalune.id/yasaku/schema"
)

type pgKeyLedger struct {
	org, project, key uuid.UUID
}

func seedPgKeyLedger(t *testing.T, migDB *sql.DB, prefix string, user uuid.UUID) pgKeyLedger {
	t.Helper()
	l := pgKeyLedger{org: uuid.New(), project: uuid.New(), key: uuid.New()}
	wallet, period := uuid.New(), uuid.New()
	exec := func(q string, args ...any) {
		t.Helper()
		_, err := migDB.ExecContext(t.Context(), q, args...)
		require.NoError(t, err, q)
	}
	tbl := func(name string) string { return "public." + prefix + name }
	const t0 = "2026-01-01T00:00:00Z"
	exec(`INSERT INTO `+tbl("orgs")+` (id, slug, name, created_by, created_at, updated_at) VALUES ($1, $2, 'O', $3, $4, $4)`, l.org, "o-"+l.org.String(), user, t0)
	exec(`INSERT INTO `+tbl("projects")+` (id, org_id, slug, name, created_by, created_at, updated_at) VALUES ($1, $2, 'p', 'P', $3, $4, $4)`, l.project, l.org, user, t0)
	exec(`INSERT INTO `+tbl("api_keys")+` (id, org_id, project_id, name, secret_hash, created_at) VALUES ($1, $2, $3, 'ci', $4, $5)`, l.key, l.org, l.project, l.key[:], t0)
	exec(`INSERT INTO `+tbl("wallets")+` (id, org_id, project_id, name, kind, currency, created_at, updated_at) VALUES ($1, $2, $3, 'Cash', 'cash', 'IDR', $4, $4)`, wallet, l.org, l.project, t0)
	exec(`INSERT INTO `+tbl("periods")+` (id, org_id, project_id, name, start_date, end_date, status, created_at, updated_at) VALUES ($1, $2, $3, 'Jan', '2026-01-01', '2026-01-31', 'closed', $4, $4)`, period, l.org, l.project, t0)
	exec(`INSERT INTO `+tbl("period_closings")+` (id, org_id, project_id, period_id, closed_at, closed_by_key_id, snapshot) VALUES ($1, $2, $3, $4, $5, $6, '{}')`, uuid.New(), l.org, l.project, period, t0, l.key)
	exec(`INSERT INTO `+tbl("transactions")+` (id, org_id, project_id, wallet_id, kind, amount_minor, currency, occurred_at, created_by_key_id, created_at, updated_at) VALUES ($1, $2, $3, $4, 'opening', 100, 'IDR', $5, $6, $5, $5)`, uuid.New(), l.org, l.project, wallet, t0, l.key)
	return l
}

func TestMigrate_Postgres_KeyAuthorFKRestrictsKeyDeleteButNotCascades(t *testing.T) {
	h := pgtest.New(t)
	suffix := uniqueSuffix(t)
	prefix := "t" + suffix + "_"
	migDB := migrationRoleDB(t, h, "yasaku_keyrestrict_"+suffix, "BYPASSRLS")
	cfg := migrationConfig(prefix)
	require.NoError(t, schema.MigrateUp(t.Context(), migDB, cfg))
	tbl := func(name string) string { return "public." + prefix + name }

	user := uuid.New()
	_, err := migDB.ExecContext(t.Context(),
		`INSERT INTO `+tbl("users")+` (id, email, name, avatar_url, is_admin, created_at, updated_at) VALUES ($1, $2, '', '', false, now(), now())`,
		user, user.String()+"@x.co")
	require.NoError(t, err)
	count := func(table string, org uuid.UUID) int {
		t.Helper()
		var n int
		require.NoError(t, migDB.QueryRowContext(t.Context(), `SELECT count(*) FROM `+tbl(table)+` WHERE org_id = $1`, org).Scan(&n))
		return n
	}

	byKey := seedPgKeyLedger(t, migDB, prefix, user)
	_, err = migDB.ExecContext(t.Context(), `DELETE FROM `+tbl("api_keys")+` WHERE id = $1`, byKey.key)
	var pgErr *pgconn.PgError
	require.True(t, errors.As(err, &pgErr), "deleting a key that authored ledger rows must fail, got %v", err)
	require.Equal(t, "23503", pgErr.Code, "the refusal must be a foreign key violation, not %s (%s)", pgErr.Code, pgErr.Message)
	require.Equal(t, 1, count("transactions", byKey.org))
	require.Equal(t, 1, count("period_closings", byKey.org))

	_, err = migDB.ExecContext(t.Context(), `DELETE FROM `+tbl("projects")+` WHERE id = $1`, byKey.project)
	require.NoError(t, err, "deleting the project must cascade through its keys and ledger rows")
	require.Zero(t, count("transactions", byKey.org))
	require.Zero(t, count("period_closings", byKey.org))
	require.Zero(t, count("api_keys", byKey.org))

	byOrg := seedPgKeyLedger(t, migDB, prefix, user)
	_, err = migDB.ExecContext(t.Context(), `DELETE FROM `+tbl("orgs")+` WHERE id = $1`, byOrg.org)
	require.NoError(t, err, "deleting the org must cascade through its keys and ledger rows")
	require.Zero(t, count("transactions", byOrg.org))
	require.Zero(t, count("period_closings", byOrg.org))
	require.Zero(t, count("api_keys", byOrg.org))

	require.NoError(t, schema.MigrateDownTo(t.Context(), migDB, cfg, 15), "Down must work once no key-authored rows remain")
	require.NoError(t, schema.MigrateUp(t.Context(), migDB, cfg))
}
