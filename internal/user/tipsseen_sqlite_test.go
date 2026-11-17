package user_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pdb "altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/user"
)

func TestSQLiteStore_MarkTipsSeen(t *testing.T) {
	t.Parallel()
	db, dbcfg := openMemSQLite(t)
	assertTipsSeen(t, user.NewStore(dbcfg, pdb.Pool{W: db, R: db}))
}

func TestSQLiteStore_MarkTipsSeenConcurrent(t *testing.T) {
	t.Parallel()
	db, dbcfg := openMemSQLite(t)
	assertTipsSeenConcurrent(t, user.NewStore(dbcfg, pdb.Pool{W: db, R: db}))
}

func assertTipsSeenConcurrent(t *testing.T, store user.Store) {
	t.Helper()
	ctx := t.Context()
	u, err := user.New("tips-race-"+uuid.NewString()+"@example.com", "Race", user.SourceLocal)
	require.NoError(t, err)
	require.NoError(t, store.Save(ctx, u))

	const rounds, writers = 20, 3
	for round := range rounds {
		ids := make([]string, writers)
		var wg sync.WaitGroup
		errs := make(chan error, writers)
		for i := range writers {
			ids[i] = fmt.Sprintf("r%d-w%d", round, i)
			wg.Go(func() {
				_, err := store.MarkTipsSeen(ctx, u.ID, []string{ids[i]})
				errs <- err
			})
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			require.NoError(t, err)
		}
		got, err := store.ByID(ctx, u.ID)
		require.NoError(t, err)
		for _, id := range ids {
			require.Contains(t, got.TipsSeen, id, "round %d lost a concurrent dismissal", round)
		}
	}
}

func assertTipsSeen(t *testing.T, store user.Store) {
	t.Helper()
	ctx := t.Context()
	me, err := user.New("tips-me@example.com", "Me", user.SourceLocal)
	require.NoError(t, err)
	other, err := user.New("tips-other@example.com", "Other", user.SourceLocal)
	require.NoError(t, err)
	require.NoError(t, store.Save(ctx, me))
	require.NoError(t, store.Save(ctx, other))

	fresh, err := store.ByID(ctx, me.ID)
	require.NoError(t, err)
	assert.Empty(t, fresh.TipsSeen)

	got, err := store.MarkTipsSeen(ctx, me.ID, []string{"a"})
	require.NoError(t, err)
	assert.Equal(t, []string{"a"}, got)

	got, err = store.MarkTipsSeen(ctx, me.ID, []string{"a", "b", "b"})
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, got, "appending is idempotent")

	require.NoError(t, store.Save(ctx, me))
	byID, err := store.ByID(ctx, me.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, byID.TipsSeen, "Save leaves tips_seen alone")

	byEmail, err := store.ByEmail(ctx, me.Email)
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, byEmail.TipsSeen)

	untouched, err := store.ByID(ctx, other.ID)
	require.NoError(t, err)
	assert.Empty(t, untouched.TipsSeen)

	_, err = store.MarkTipsSeen(ctx, uuid.New(), []string{"a"})
	assert.True(t, user.IsNotFoundError(err), "%v", err)

	require.NoError(t, store.ResetTipsSeen(ctx, me.ID))
	cleared, err := store.ByID(ctx, me.ID)
	require.NoError(t, err)
	assert.Empty(t, cleared.TipsSeen, "reset forgets every dismissal")
	got, err = store.MarkTipsSeen(ctx, me.ID, []string{"c"})
	require.NoError(t, err)
	assert.Equal(t, []string{"c"}, got, "a tip can be dismissed again after a reset")
	untouched, err = store.ByID(ctx, other.ID)
	require.NoError(t, err)
	assert.Empty(t, untouched.TipsSeen, "reset touches only the named user")
	assert.True(t, user.IsNotFoundError(store.ResetTipsSeen(ctx, uuid.New())))
}
