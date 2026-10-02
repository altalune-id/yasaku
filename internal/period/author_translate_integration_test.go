//go:build integration

package period

import (
	"testing"

	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/testutil/pgtest"
)

func TestTranslatePgError_KeyAuthorCheckIsAuthorMissing(t *testing.T) {
	h := pgtest.New(t)
	db := h.OpenDB(t)
	_, err := db.ExecContext(t.Context(), `CREATE TEMP TABLE x (a uuid, b uuid, CONSTRAINT x_period_closings_author_one CHECK ((a IS NULL) <> (b IS NULL)))`)
	require.NoError(t, err)
	_, execErr := db.ExecContext(t.Context(), `INSERT INTO x (a, b) VALUES (NULL, NULL)`)
	require.Error(t, execErr)
	require.True(t, IsAuthorMissingError(translatePgError(execErr, nil)), "got %v", execErr)
}
