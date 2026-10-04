package opensheetsync_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/opensheetsync"
)

func TestParseEntity(t *testing.T) {
	for _, s := range []string{"transaction", "wallet", "category"} {
		e, ok := opensheetsync.ParseEntity(s)
		require.True(t, ok)
		require.Equal(t, s, string(e))
	}
	_, ok := opensheetsync.ParseEntity("period")
	require.False(t, ok)
}

func TestDedupe_MergesFlagsAndKeepsFirstSeenOrder(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	got := opensheetsync.Dedupe([]opensheetsync.Ref{
		{Entity: opensheetsync.EntityWallet, ID: a},
		{Entity: opensheetsync.EntityTransaction, ID: b},
		{Entity: opensheetsync.EntityWallet, ID: a, Cascade: true},
		{Entity: opensheetsync.EntityTransaction, ID: b, Deleted: true},
		{Entity: opensheetsync.EntityCategory, ID: a},
	})
	require.Equal(t, []opensheetsync.Ref{
		{Entity: opensheetsync.EntityWallet, ID: a, Cascade: true},
		{Entity: opensheetsync.EntityTransaction, ID: b, Deleted: true},
		{Entity: opensheetsync.EntityCategory, ID: a},
	}, got)
}

func TestEntity_LockRankIsTransactionsThenCategoriesThenWallets(t *testing.T) {
	require.Equal(t, 0, opensheetsync.EntityTransaction.LockRank())
	require.Equal(t, 1, opensheetsync.EntityCategory.LockRank())
	require.Equal(t, 2, opensheetsync.EntityWallet.LockRank())
}

func TestRowBackoff_DoublesFromOneMinuteAndCapsAtEight(t *testing.T) {
	for attempts, want := range map[int]time.Duration{
		0: time.Minute, 1: time.Minute, 2: 2 * time.Minute, 3: 4 * time.Minute, 4: 8 * time.Minute, 5: 8 * time.Minute, 50: 8 * time.Minute,
	} {
		require.Equal(t, want, opensheetsync.RowBackoff(attempts), "attempts=%d", attempts)
	}
	require.Equal(t, 5, opensheetsync.MaxRowAttempts)
}

func TestState_DirtyWhileTheSheetTrails(t *testing.T) {
	require.True(t, opensheetsync.State{Version: 2, SyncedVersion: 1}.Dirty())
	require.False(t, opensheetsync.State{Version: 2, SyncedVersion: 2}.Dirty())
}

func TestLockOrder_DedupesThenSortsByLockRankThenID(t *testing.T) {
	lo := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	hi := uuid.MustParse("ffffffff-0000-0000-0000-000000000000")
	in := []opensheetsync.Ref{
		{Entity: opensheetsync.EntityWallet, ID: hi},
		{Entity: opensheetsync.EntityWallet, ID: lo},
		{Entity: opensheetsync.EntityCategory, ID: lo},
		{Entity: opensheetsync.EntityTransaction, ID: hi, Deleted: true},
		{Entity: opensheetsync.EntityTransaction, ID: lo},
		{Entity: opensheetsync.EntityWallet, ID: hi, Cascade: true},
	}
	got := opensheetsync.LockOrder(in)
	require.Equal(t, []opensheetsync.Ref{
		{Entity: opensheetsync.EntityTransaction, ID: lo},
		{Entity: opensheetsync.EntityTransaction, ID: hi, Deleted: true},
		{Entity: opensheetsync.EntityCategory, ID: lo},
		{Entity: opensheetsync.EntityWallet, ID: lo},
		{Entity: opensheetsync.EntityWallet, ID: hi, Cascade: true},
	}, got)
	require.Equal(t, opensheetsync.EntityWallet, in[0].Entity, "the caller's slice is left alone")
	require.Equal(t, hi, in[0].ID)
}
