package user_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pdb "altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/user"
)

func TestSQLiteStore_UpdateThemePrefs(t *testing.T) {
	t.Parallel()
	db, dbcfg := openMemSQLite(t)
	assertThemePrefs(t, user.NewStore(dbcfg, pdb.Pool{W: db, R: db}))

	var tips string
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT tips_seen FROM "+dbcfg.TablePrefix+"users LIMIT 1").Scan(&tips))
	assert.Equal(t, "[]", tips)
}

func assertThemePrefs(t *testing.T, store user.Store) {
	t.Helper()
	ctx := t.Context()
	me, err := user.New("me@example.com", "Me", user.SourceLocal)
	require.NoError(t, err)
	other, err := user.New("other@example.com", "Other", user.SourceLocal)
	require.NoError(t, err)
	require.NoError(t, store.Save(ctx, me))
	require.NoError(t, store.Save(ctx, other))

	fresh, err := store.ByID(ctx, me.ID)
	require.NoError(t, err)
	assert.Empty(t, fresh.Theme)
	assert.Empty(t, fresh.ColorMode)

	require.NoError(t, store.UpdateThemePrefs(ctx, me.ID, "fam", "dark"))
	require.NoError(t, store.Save(ctx, me))

	got, err := store.ByID(ctx, me.ID)
	require.NoError(t, err)
	assert.Equal(t, "fam", got.Theme)
	assert.Equal(t, "dark", got.ColorMode)

	byEmail, err := store.ByEmail(ctx, me.Email)
	require.NoError(t, err)
	assert.Equal(t, "dark", byEmail.ColorMode)

	untouched, err := store.ByID(ctx, other.ID)
	require.NoError(t, err)
	assert.Empty(t, untouched.ColorMode)

	assert.True(t, user.IsNotFoundError(store.UpdateThemePrefs(ctx, uuid.New(), "fam", "light")))
}
