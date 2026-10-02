package blog_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/blog"
	"altalune.id/yasaku/internal/platform/tenant"
)

type taggedFixture struct {
	svc  *blog.Service
	ctx  context.Context
	cat  uuid.UUID
	tagA uuid.UUID
	tagB uuid.UUID
}

func newTaggedFixture(t *testing.T) taggedFixture {
	t.Helper()
	store, sqlDB, tc, cat := newBlogStoreForTest(t)
	svc, _, _ := newHooked(t, store, sqliteUnitOfWork(sqlDB))
	return taggedFixture{
		svc:  svc,
		ctx:  tenant.Into(t.Context(), tc),
		cat:  cat,
		tagA: seedTag(t, sqlDB, tc),
		tagB: seedTag(t, sqlDB, tc),
	}
}

func TestSQLite_UpdateWithTagsHonoursThePreconditionWhole(t *testing.T) {
	for _, tt := range []struct {
		name string
		run  func(t *testing.T, f taggedFixture)
	}{
		{
			name: "a stale writer lands neither its body nor its tags",
			run: func(t *testing.T, f taggedFixture) {
				t.Helper()
				p, err := f.svc.Create(f.ctx, f.cat, "Original", "original", "body")
				require.NoError(t, err)
				n := p.Version

				_, err = f.svc.UpdateWithTags(f.ctx, p.ID, "A body", "original", "a", f.cat, []uuid.UUID{f.tagA}, n)
				require.NoError(t, err, "the first conditional write must win")

				_, err = f.svc.UpdateWithTags(f.ctx, p.ID, "B body", "original", "b", f.cat, []uuid.UUID{f.tagB}, n)
				require.True(t, blog.IsStaleVersionError(err), "got %T: %v", err, err)

				got, err := f.svc.ByID(f.ctx, p.ID)
				require.NoError(t, err)
				assert.Equal(t, "A body", got.Title)
				assert.Equal(t, []uuid.UUID{f.tagA}, got.TagIDs, "the refused writer's tags must not have landed")
			},
		},
		{
			name: "one update is one write, so no unconditional tail can follow it",
			run: func(t *testing.T, f taggedFixture) {
				t.Helper()
				p, err := f.svc.Create(f.ctx, f.cat, "Original", "original", "body")
				require.NoError(t, err)

				got, err := f.svc.UpdateWithTags(f.ctx, p.ID, "Edited", "original", "b", f.cat, []uuid.UUID{f.tagA, f.tagB}, p.Version)
				require.NoError(t, err)
				assert.Equal(t, p.Version+1, got.Version)

				stored, err := f.svc.ByID(f.ctx, p.ID)
				require.NoError(t, err)
				assert.Equal(t, p.Version+1, stored.Version)
				assert.ElementsMatch(t, []uuid.UUID{f.tagA, f.tagB}, stored.TagIDs)
			},
		},
		{
			name: "an empty tag set clears the tags under the same precondition",
			run: func(t *testing.T, f taggedFixture) {
				t.Helper()
				p, err := f.svc.Create(f.ctx, f.cat, "Original", "original", "body")
				require.NoError(t, err)
				tagged, err := f.svc.UpdateWithTags(f.ctx, p.ID, "Tagged", "original", "b", f.cat, []uuid.UUID{f.tagA}, p.Version)
				require.NoError(t, err)

				cleared, err := f.svc.UpdateWithTags(f.ctx, p.ID, "Cleared", "original", "c", f.cat, nil, tagged.Version)
				require.NoError(t, err)
				assert.Empty(t, cleared.TagIDs)

				stored, err := f.svc.ByID(f.ctx, p.ID)
				require.NoError(t, err)
				assert.Empty(t, stored.TagIDs)
			},
		},
		{
			name: "ifVersion 0 stays last-write-wins for the console",
			run: func(t *testing.T, f taggedFixture) {
				t.Helper()
				p, err := f.svc.Create(f.ctx, f.cat, "Original", "original", "body")
				require.NoError(t, err)
				_, err = f.svc.UpdateWithTags(f.ctx, p.ID, "A body", "original", "a", f.cat, []uuid.UUID{f.tagA}, p.Version)
				require.NoError(t, err)

				got, err := f.svc.UpdateWithTags(f.ctx, p.ID, "B body", "original", "b", f.cat, []uuid.UUID{f.tagB}, 0)
				require.NoError(t, err, "an unconditional update keeps today's console semantics")
				assert.Equal(t, []uuid.UUID{f.tagB}, got.TagIDs)
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tt.run(t, newTaggedFixture(t))
		})
	}
}
