//go:build integration

package transaction

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pdb "altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/testutil/pgtest"
)

// TestAdvisoryKeyIsNamespacedAwayFromSchedulerJobLocks pins that a wallet lock and a scheduler job
// lock of the same numeric value do not collide. A collision would be silent and asymmetric:
// db.PgLocker.TryLock would report acquired=false and the job would skip its run.
func TestAdvisoryKeyIsNamespacedAwayFromSchedulerJobLocks(t *testing.T) {
	h := pgtest.New(t)
	sqlDB := h.OpenDB(t)

	orgID, projectID, walletID := uuid.New(), uuid.New(), uuid.New()
	collidingKey := int64(advisoryKey(orgID, projectID, walletID))

	conn, err := sqlDB.Conn(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	var taken bool
	require.NoError(t, conn.QueryRowContext(t.Context(),
		"SELECT pg_try_advisory_lock($1)", collidingKey).Scan(&taken))
	require.True(t, taken, "the scheduler-shaped lock must be held for this test to mean anything")

	store := newPostgresStore(pdb.Pool{W: sqlDB, R: sqlDB}, tenant.NewPgConn(sqlDB), h.Schema, "")
	tc := tenant.Context{OrgID: orgID, ProjectID: projectID, UserID: uuid.New()}

	bounded, cancel := context.WithTimeout(tenant.Into(t.Context(), tc), 5*time.Second)
	defer cancel()
	err = tenant.RunInTx(bounded, tenant.NewPgConn(sqlDB), tc, func(inner context.Context) error {
		return store.LockWallet(inner, orgID, projectID, walletID)
	})
	assert.NoError(t, err,
		"the wallet lock must live in the two-argument advisory space, not the single-argument one the scheduler uses")
}

func TestAdvisoryKeyIsStableAndScopeSensitive(t *testing.T) {
	orgID, projectID, walletID := uuid.New(), uuid.New(), uuid.New()
	assert.Equal(t, advisoryKey(orgID, projectID, walletID), advisoryKey(orgID, projectID, walletID),
		"the key must be stable across calls or the lock protects nothing")
	assert.NotEqual(t, advisoryKey(orgID, projectID, walletID), advisoryKey(orgID, projectID, uuid.New()),
		"two wallets must not normally share a key")
	assert.NotEqual(t, advisoryKey(orgID, projectID, walletID), advisoryKey(uuid.New(), projectID, walletID),
		"the tenant scope must take part in the key")
}
