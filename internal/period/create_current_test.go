package period_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/civil"
	"altalune.id/yasaku/internal/period"
	"altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/platform/tenant"
)

func TestSQLiteStore_CreateCurrentKeepsTheUnitOfWorkUsableAfterALostRace(t *testing.T) {
	sqlDB, cfg := newSQLiteDB(t)
	tc := seedTenant(t, sqlDB, cfg.DB.TablePrefix)
	pool := db.Pool{W: sqlDB, R: sqlDB}
	store := period.NewStore(db.DBConfig{Driver: db.DriverSQLite, TablePrefix: cfg.DB.TablePrefix}, pool, nil)
	ctx := tenant.Into(t.Context(), tc)

	first, err := period.New(tc.OrgID, tc.ProjectID, civil.Date{Year: 2026, Month: 8, Day: 25}, "")
	require.NoError(t, err)
	created, err := store.CreateCurrent(ctx, first)
	require.NoError(t, err)
	assert.True(t, created)

	second, err := period.New(tc.OrgID, tc.ProjectID, civil.Date{Year: 2026, Month: 9, Day: 25}, "")
	require.NoError(t, err)
	var reread *period.Period
	require.NoError(t, db.RunInTx(ctx, pool, func(ctx context.Context) error {
		lost, cErr := store.CreateCurrent(ctx, second)
		require.NoError(t, cErr)
		assert.False(t, lost, "a project with a current period keeps it")
		var rErr error
		reread, rErr = store.Current(ctx, tc.OrgID, tc.ProjectID)
		return rErr
	}))
	assert.Equal(t, first.ID, reread.ID)
}

func TestSQLiteStore_CreateCurrentRefusesAPeriodOutsideTheCallerScope(t *testing.T) {
	store, tc := newSQLiteStoreForTest(t)
	ctx := tenant.Into(t.Context(), tc)

	foreign, err := period.New(tc.OrgID, uuid.New(), civil.Date{Year: 2026, Month: 8, Day: 25}, "")
	require.NoError(t, err)
	created, err := store.CreateCurrent(ctx, foreign)
	assert.True(t, period.IsNotFoundError(err), "got %T: %v", err, err)
	assert.False(t, created)
}
