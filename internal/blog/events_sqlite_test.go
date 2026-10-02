package blog_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/blog"
	"altalune.id/yasaku/internal/platform/tenant"
)

func TestSQLite_EnqueueFailureRollsBackTheWrite(t *testing.T) {
	store, sqlDB, tc, cat := newBlogStoreForTest(t)
	svc, unex, hooks := newHooked(t, store, sqliteUnitOfWork(sqlDB))
	ctx := tenant.Into(t.Context(), tc)

	draft, err := svc.Create(ctx, cat, "Draft", "", "body")
	require.NoError(t, err)
	live, err := svc.Create(ctx, cat, "Live", "", "body")
	require.NoError(t, err)
	live, err = svc.Publish(ctx, live.ID, 0)
	require.NoError(t, err)
	require.Len(t, hooks.Recorded(), 1)
	require.True(t, hooks.Recorded()[0].InTx)

	hooks.Err = errors.New("outbox down")

	_, err = svc.Publish(ctx, draft.ID, 0)
	require.Error(t, err)
	got, err := svc.ByID(ctx, draft.ID)
	require.NoError(t, err)
	assert.Equal(t, blog.StatusDraft, got.Status, "a failed enqueue must roll the publish back")
	assert.Equal(t, draft.Version, got.Version)

	require.Error(t, svc.Delete(ctx, live.ID, 0))
	got, err = svc.ByID(ctx, live.ID)
	require.NoError(t, err, "a failed enqueue must roll the delete back")
	assert.Equal(t, blog.StatusPublished, got.Status)

	assert.Equal(t, 2, *unex)
}
