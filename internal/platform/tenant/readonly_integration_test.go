//go:build integration

package tenant_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pdb "altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/testutil/pgtest"
)

func TestSupportScope_ReadOnlyTxRejectsWrite(t *testing.T) {
	h := pgtest.New(t)
	sqlDB := h.OpenDB(t)
	pc := tenant.NewPgConn(sqlDB)

	write := func(ctx context.Context) error {
		tx, _ := pdb.CurrentTx(ctx)
		_, err := tx.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS "+h.Schema+".ro_probe (id int)")
		return err
	}

	err := tenant.RunInTx(t.Context(), pc, tenant.Context{OrgID: uuid.New(), ReadOnly: true}, write)
	var pgErr *pgconn.PgError
	require.True(t, errors.As(err, &pgErr), "want a Postgres refusal, got %T: %v", err, err)
	assert.Equal(t, "25006", pgErr.Code, "a support scope's transaction is READ ONLY")

	require.NoError(t, tenant.RunInTx(t.Context(), pc, tenant.Context{OrgID: uuid.New()}, write), "an ordinary scope still writes")
}
