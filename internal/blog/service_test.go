package blog_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	apperrorv1 "altalune.id/yasaku/gen/go/apperror/v1"
	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/blog"
	"altalune.id/yasaku/internal/platform/events"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/testutil/fakes"
)

func newSvc(t *testing.T, store blog.Store) (*blog.Service, *int) {
	t.Helper()
	svc, calls, _ := newHooked(t, store, fakes.UnitOfWork)
	return svc, calls
}

func newHooked(t *testing.T, store blog.Store, uow tenant.UnitOfWork) (*blog.Service, *int, *fakes.Webhooks) {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	calls := 0
	unexpected := func(_ context.Context, _ string, err error, _ ...any) *apperror.AppError {
		calls++
		return apperror.New("yasaku.unexpected", err.Error(), codes.Internal,
			&apperrorv1.ErrorDetail{Code: "yasaku.unexpected"}).WithCause(err)
	}
	hooks := &fakes.Webhooks{}
	return blog.NewService(store, log, unexpected, uow, hooks), &calls, hooks
}

func tenantCtx(t *testing.T) (context.Context, tenant.Context) {
	t.Helper()
	tc := tenant.Context{OrgID: uuid.New(), ProjectID: uuid.New(), UserID: uuid.New()}
	return tenant.Into(context.Background(), tc), tc
}

func TestService_Create(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		svc, unex := newSvc(t, fakes.NewBlog())
		ctx, tc := tenantCtx(t)
		cat := uuid.New()

		got, err := svc.Create(ctx, cat, "  Hello World  ", "", "# body")
		require.NoError(t, err)
		assert.Equal(t, "Hello World", got.Title)
		assert.Equal(t, "hello-world", got.Slug)
		assert.Equal(t, blog.StatusDraft, got.Status)
		assert.Equal(t, cat, got.CategoryID)
		assert.Equal(t, tc.OrgID, got.OrgID)
		assert.Equal(t, tc.ProjectID, got.ProjectID)
		assert.Nil(t, got.FirstPublishedAt)
		assert.Zero(t, *unex)
	})

	t.Run("invalid title is a typed error, not unexpected", func(t *testing.T) {
		svc, unex := newSvc(t, fakes.NewBlog())
		ctx, _ := tenantCtx(t)

		_, err := svc.Create(ctx, uuid.New(), "   ", "", "body")
		assert.True(t, blog.IsInvalidTitleError(err), "got %T: %v", err, err)
		assert.Zero(t, *unex)
	})

	t.Run("oversized body is a typed error", func(t *testing.T) {
		svc, unex := newSvc(t, fakes.NewBlog())
		ctx, _ := tenantCtx(t)

		_, err := svc.Create(ctx, uuid.New(), "Title", "", strings.Repeat("a", blog.MaxBodyBytes+1))
		assert.True(t, blog.IsInvalidBodyError(err), "got %T: %v", err, err)
		assert.Zero(t, *unex)
	})

	t.Run("a missing category is a typed error", func(t *testing.T) {
		svc, unex := newSvc(t, fakes.NewBlog())
		ctx, _ := tenantCtx(t)

		_, err := svc.Create(ctx, uuid.Nil, "Title", "", "body")
		assert.True(t, blog.IsCategoryRequiredError(err), "got %T: %v", err, err)
		assert.Zero(t, *unex)
	})

	t.Run("duplicate slug surfaces AlreadyExists, not unexpected", func(t *testing.T) {
		svc, unex := newSvc(t, fakes.NewBlog())
		ctx, _ := tenantCtx(t)
		cat := uuid.New()

		_, err := svc.Create(ctx, cat, "Hello World", "", "body")
		require.NoError(t, err)

		_, err = svc.Create(ctx, cat, "hello world", "", "body")
		assert.True(t, blog.IsAlreadyExistsError(err), "got %T: %v", err, err)
		assert.Zero(t, *unex)
	})

	t.Run("store failure is reported as unexpected", func(t *testing.T) {
		store := fakes.NewBlog()
		store.SaveFn = func(context.Context, *blog.Post, int) error { return errors.New("boom") }
		svc, unex := newSvc(t, store)
		ctx, _ := tenantCtx(t)

		_, err := svc.Create(ctx, uuid.New(), "Title", "", "body")
		require.Error(t, err)
		assert.Equal(t, 1, *unex)
	})

	t.Run("missing tenant", func(t *testing.T) {
		svc, unex := newSvc(t, fakes.NewBlog())
		_, err := svc.Create(context.Background(), uuid.New(), "Title", "", "body")
		assert.True(t, tenant.IsMissingError(err), "got %T: %v", err, err)
		assert.Zero(t, *unex)
	})
}

func TestService_Update(t *testing.T) {
	t.Run("replaces the editable fields", func(t *testing.T) {
		store := fakes.NewBlog()
		svc, unex := newSvc(t, store)
		ctx, _ := tenantCtx(t)
		cat, newCat := uuid.New(), uuid.New()

		created, err := svc.Create(ctx, cat, "Hello World", "", "body")
		require.NoError(t, err)

		got, err := svc.Update(ctx, created.ID, "Goodbye", "custom-slug", "new body", newCat, 0)
		require.NoError(t, err)
		assert.Equal(t, "Goodbye", got.Title)
		assert.Equal(t, "custom-slug", got.Slug)
		assert.Equal(t, "new body", got.BodyMarkdown)
		assert.Equal(t, newCat, got.CategoryID)
		assert.Equal(t, 1, store.Len(), "Update must not insert a second row")
		assert.Zero(t, *unex)
	})

	t.Run("leaves status and first publication alone", func(t *testing.T) {
		svc, unex := newSvc(t, fakes.NewBlog())
		ctx, _ := tenantCtx(t)
		cat := uuid.New()

		created, err := svc.Create(ctx, cat, "Hello", "", "body")
		require.NoError(t, err)
		published, err := svc.Publish(ctx, created.ID, 0)
		require.NoError(t, err)

		got, err := svc.Update(ctx, created.ID, "Edited", "", "body", cat, 0)
		require.NoError(t, err)
		assert.Equal(t, blog.StatusPublished, got.Status)
		require.NotNil(t, got.FirstPublishedAt)
		assert.True(t, got.FirstPublishedAt.Equal(*published.FirstPublishedAt))
		assert.Zero(t, *unex)
	})

	t.Run("unknown id", func(t *testing.T) {
		svc, unex := newSvc(t, fakes.NewBlog())
		ctx, _ := tenantCtx(t)

		_, err := svc.Update(ctx, uuid.New(), "Title", "", "body", uuid.New(), 0)
		assert.True(t, blog.IsNotFoundError(err), "got %T: %v", err, err)
		assert.Zero(t, *unex)
	})

	t.Run("invalid title", func(t *testing.T) {
		svc, unex := newSvc(t, fakes.NewBlog())
		ctx, _ := tenantCtx(t)

		created, err := svc.Create(ctx, uuid.New(), "Hello", "", "body")
		require.NoError(t, err)

		_, err = svc.Update(ctx, created.ID, "  ", "", "body", uuid.New(), 0)
		assert.True(t, blog.IsInvalidTitleError(err), "got %T: %v", err, err)
		assert.Zero(t, *unex)
	})

	t.Run("a colliding slug surfaces AlreadyExists", func(t *testing.T) {
		svc, unex := newSvc(t, fakes.NewBlog())
		ctx, _ := tenantCtx(t)
		cat := uuid.New()

		first, err := svc.Create(ctx, cat, "Taken", "", "body")
		require.NoError(t, err)
		second, err := svc.Create(ctx, cat, "Free", "", "body")
		require.NoError(t, err)

		_, err = svc.Update(ctx, second.ID, "Free", first.Slug, "body", cat, 0)
		assert.True(t, blog.IsAlreadyExistsError(err), "got %T: %v", err, err)
		assert.Zero(t, *unex)
	})
}

func TestService_SetTags(t *testing.T) {
	t.Run("replaces and de-duplicates", func(t *testing.T) {
		svc, unex := newSvc(t, fakes.NewBlog())
		ctx, _ := tenantCtx(t)
		a, b := uuid.New(), uuid.New()

		created, err := svc.Create(ctx, uuid.New(), "Hello", "", "body")
		require.NoError(t, err)

		got, err := svc.SetTags(ctx, created.ID, []uuid.UUID{a, b, a})
		require.NoError(t, err)
		assert.Equal(t, []uuid.UUID{a, b}, got.TagIDs)

		got, err = svc.SetTags(ctx, created.ID, []uuid.UUID{b})
		require.NoError(t, err)
		assert.Equal(t, []uuid.UUID{b}, got.TagIDs)

		got, err = svc.SetTags(ctx, created.ID, nil)
		require.NoError(t, err)
		assert.Empty(t, got.TagIDs)
		assert.Zero(t, *unex)
	})

	t.Run("unknown id", func(t *testing.T) {
		svc, unex := newSvc(t, fakes.NewBlog())
		ctx, _ := tenantCtx(t)

		_, err := svc.SetTags(ctx, uuid.New(), []uuid.UUID{uuid.New()})
		assert.True(t, blog.IsNotFoundError(err), "got %T: %v", err, err)
		assert.Zero(t, *unex)
	})
}

func TestService_PublishAndUnpublish(t *testing.T) {
	t.Run("records the first publication only once", func(t *testing.T) {
		svc, unex := newSvc(t, fakes.NewBlog())
		ctx, _ := tenantCtx(t)

		created, err := svc.Create(ctx, uuid.New(), "Hello", "", "body")
		require.NoError(t, err)
		require.Nil(t, created.FirstPublishedAt)

		first, err := svc.Publish(ctx, created.ID, 0)
		require.NoError(t, err)
		assert.Equal(t, blog.StatusPublished, first.Status)
		require.NotNil(t, first.FirstPublishedAt)
		firstAt := *first.FirstPublishedAt

		drafted, err := svc.Unpublish(ctx, created.ID, 0)
		require.NoError(t, err)
		assert.Equal(t, blog.StatusDraft, drafted.Status)
		require.NotNil(t, drafted.FirstPublishedAt, "unpublishing retains the first publication")

		again, err := svc.Publish(ctx, created.ID, 0)
		require.NoError(t, err)
		assert.True(t, again.FirstPublishedAt.Equal(firstAt), "republishing must not move FirstPublishedAt")
		assert.Zero(t, *unex)
	})

	t.Run("unknown id", func(t *testing.T) {
		svc, unex := newSvc(t, fakes.NewBlog())
		ctx, _ := tenantCtx(t)

		_, err := svc.Publish(ctx, uuid.New(), 0)
		assert.True(t, blog.IsNotFoundError(err), "got %T: %v", err, err)

		_, err = svc.Unpublish(ctx, uuid.New(), 0)
		assert.True(t, blog.IsNotFoundError(err), "got %T: %v", err, err)
		assert.Zero(t, *unex)
	})
}

func TestService_ListAndByID(t *testing.T) {
	store := fakes.NewBlog()
	svc, unex := newSvc(t, store)
	ctx, tc := tenantCtx(t)
	cat, otherCat := uuid.New(), uuid.New()

	draft, err := svc.Create(ctx, cat, "Draft", "", "body")
	require.NoError(t, err)
	pub, err := svc.Create(ctx, cat, "Published", "", "body")
	require.NoError(t, err)
	_, err = svc.Publish(ctx, pub.ID, 0)
	require.NoError(t, err)
	elsewhere, err := svc.Create(ctx, otherCat, "Elsewhere", "", "body")
	require.NoError(t, err)

	foreign, err := blog.New(uuid.New(), uuid.New(), cat, "Foreign", "", "body")
	require.NoError(t, err)
	store.Seed(foreign)

	all, err := svc.List(ctx, blog.ListOpts{})
	require.NoError(t, err)
	assert.Len(t, all, 3, "List must not leak another tenant's posts")

	wantPublished := blog.StatusPublished
	byStatus, err := svc.List(ctx, blog.ListOpts{Status: &wantPublished})
	require.NoError(t, err)
	require.Len(t, byStatus, 1)
	assert.Equal(t, pub.ID, byStatus[0].ID)

	byCategory, err := svc.List(ctx, blog.ListOpts{CategoryID: &otherCat})
	require.NoError(t, err)
	require.Len(t, byCategory, 1)
	assert.Equal(t, elsewhere.ID, byCategory[0].ID)

	one, err := svc.ByID(ctx, draft.ID)
	require.NoError(t, err)
	assert.Equal(t, draft.ID, one.ID)

	_, err = svc.ByID(ctx, uuid.New())
	assert.True(t, blog.IsNotFoundError(err), "got %T: %v", err, err)

	_ = tc
	assert.Zero(t, *unex)
}

func TestService_List_StoreFailureIsUnexpected(t *testing.T) {
	store := fakes.NewBlog()
	store.ListFn = func(context.Context, uuid.UUID, uuid.UUID, blog.ListOpts) ([]*blog.Post, error) {
		return nil, errors.New("boom")
	}
	svc, unex := newSvc(t, store)
	ctx, _ := tenantCtx(t)

	_, err := svc.List(ctx, blog.ListOpts{})
	require.Error(t, err)
	assert.Equal(t, 1, *unex)
}

func TestService_Delete(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		store := fakes.NewBlog()
		svc, unex := newSvc(t, store)
		ctx, _ := tenantCtx(t)

		created, err := svc.Create(ctx, uuid.New(), "Gone", "", "body")
		require.NoError(t, err)
		require.NoError(t, svc.Delete(ctx, created.ID, 0))
		assert.Zero(t, store.Len())
		assert.Zero(t, *unex)
	})

	t.Run("unknown id", func(t *testing.T) {
		svc, unex := newSvc(t, fakes.NewBlog())
		ctx, _ := tenantCtx(t)

		assert.True(t, blog.IsNotFoundError(svc.Delete(ctx, uuid.New(), 0)), "unknown id must be NotFoundError")
		assert.Zero(t, *unex)
	})

	t.Run("store failure is reported as unexpected", func(t *testing.T) {
		store := fakes.NewBlog()
		store.DeleteFn = func(context.Context, uuid.UUID, int) error { return errors.New("boom") }
		svc, unex := newSvc(t, store)
		ctx, _ := tenantCtx(t)

		created, err := svc.Create(ctx, uuid.New(), "Gone", "", "body")
		require.NoError(t, err)
		require.Error(t, svc.Delete(ctx, created.ID, 0))
		assert.Equal(t, 1, *unex)
	})
}

func TestService_Counts(t *testing.T) {
	svc, unex := newSvc(t, fakes.NewBlog())
	ctx, _ := tenantCtx(t)
	cat, unusedCat := uuid.New(), uuid.New()
	a, b, unusedTag := uuid.New(), uuid.New(), uuid.New()

	first, err := svc.Create(ctx, cat, "First", "", "body")
	require.NoError(t, err)
	_, err = svc.SetTags(ctx, first.ID, []uuid.UUID{a, b})
	require.NoError(t, err)

	second, err := svc.Create(ctx, cat, "Second", "", "body")
	require.NoError(t, err)
	_, err = svc.SetTags(ctx, second.ID, []uuid.UUID{a})
	require.NoError(t, err)

	byCategory, err := svc.CountByCategory(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, byCategory[cat])
	_, present := byCategory[unusedCat]
	assert.False(t, present, "a category with no posts is absent, not zero-valued")

	byTag, err := svc.CountByTag(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, byTag[a])
	assert.Equal(t, 1, byTag[b])
	_, present = byTag[unusedTag]
	assert.False(t, present, "a tag with no posts is absent, not zero-valued")

	assert.Zero(t, *unex)
}

func TestService_MissingTenant(t *testing.T) {
	svc, unex := newSvc(t, fakes.NewBlog())
	bare := context.Background()

	_, err := svc.List(bare, blog.ListOpts{})
	assert.True(t, tenant.IsMissingError(err), "got %T: %v", err, err)

	_, err = svc.CountByCategory(bare)
	assert.True(t, tenant.IsMissingError(err), "got %T: %v", err, err)

	_, err = svc.CountByTag(bare)
	assert.True(t, tenant.IsMissingError(err), "got %T: %v", err, err)

	assert.Zero(t, *unex)
}

func TestErrors_AppErrorCodes(t *testing.T) {
	assert.Equal(t, apperror.CodePostNotFound, (&blog.NotFoundError{}).ToAppError().Code())
	assert.Equal(t, codes.NotFound, (&blog.NotFoundError{}).ToAppError().GRPCCode())
	assert.Equal(t, apperror.CodePostAlreadyExists, (&blog.AlreadyExistsError{}).ToAppError().Code())
	assert.Equal(t, codes.AlreadyExists, (&blog.AlreadyExistsError{}).ToAppError().GRPCCode())
	assert.Equal(t, apperror.CodePostInvalidTitle, (&blog.InvalidTitleError{}).ToAppError().Code())
	assert.Equal(t, apperror.CodePostInvalidSlug, (&blog.InvalidSlugError{}).ToAppError().Code())
	assert.Equal(t, apperror.CodePostInvalidBody, (&blog.InvalidBodyError{}).ToAppError().Code())
	assert.Equal(t, apperror.CodePostCategoryRequired, (&blog.CategoryRequiredError{}).ToAppError().Code())
	assert.Equal(t, codes.InvalidArgument, (&blog.CategoryRequiredError{}).ToAppError().GRPCCode())
}

type recordingStore struct {
	*fakes.Blog
	mu        sync.Mutex
	saves     []int
	raceFirst bool
}

func (r *recordingStore) Save(ctx context.Context, p *blog.Post, ifVersion int) error {
	r.mu.Lock()
	r.saves = append(r.saves, ifVersion)
	race := r.raceFirst
	r.raceFirst = false
	r.mu.Unlock()
	if !race {
		return r.Blog.Save(ctx, p, ifVersion)
	}
	winner := *p
	if err := r.Blog.Save(ctx, &winner, 0); err != nil {
		return err
	}
	return &blog.StaleVersionError{Want: ifVersion, Got: ifVersion + 1}
}

func (r *recordingStore) savedVersions() []int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.saves)
}

func seedDraft(t *testing.T, store *fakes.Blog, tc tenant.Context) *blog.Post {
	t.Helper()
	p, err := blog.New(tc.OrgID, tc.ProjectID, uuid.New(), "Hello", "", "body")
	require.NoError(t, err)
	store.Seed(p)
	return p
}

func seedPublished(t *testing.T, store *fakes.Blog, tc tenant.Context) *blog.Post {
	t.Helper()
	p, err := blog.New(tc.OrgID, tc.ProjectID, uuid.New(), "Live", "", "body")
	require.NoError(t, err)
	p.Publish()
	p.Version = 3
	store.Seed(p)
	return p
}

func TestService_TransitionsEmitInTheUnitOfWork(t *testing.T) {
	t.Run("publish on a draft emits one PostPublished carrying the stored version", func(t *testing.T) {
		store := fakes.NewBlog()
		svc, unex, hooks := newHooked(t, store, fakes.UnitOfWork)
		ctx, tc := tenantCtx(t)
		p := seedDraft(t, store, tc)

		got, err := svc.Publish(ctx, p.ID, 0)
		require.NoError(t, err)

		stored, err := store.ByID(ctx, p.ID)
		require.NoError(t, err)
		calls := hooks.Recorded()
		require.Len(t, calls, 1)
		assert.Equal(t, events.PostPublished, calls[0].Type)
		assert.True(t, calls[0].InTx, "Enqueue must run inside the unit of work")
		data, ok := calls[0].Data.(events.PostPublishedV1)
		require.True(t, ok, "got %T", calls[0].Data)
		assert.Equal(t, p.ID, data.ID)
		assert.Equal(t, p.Slug, data.Slug)
		assert.Equal(t, stored.Version, data.Version)
		assert.Equal(t, stored.Version, got.Version)
		assert.Zero(t, *unex)
	})

	t.Run("publish on a published post writes nothing and emits nothing", func(t *testing.T) {
		store := fakes.NewBlog()
		rec := &recordingStore{Blog: store}
		svc, unex, hooks := newHooked(t, rec, fakes.UnitOfWork)
		ctx, tc := tenantCtx(t)
		p := seedPublished(t, store, tc)

		got, err := svc.Publish(ctx, p.ID, 0)
		require.NoError(t, err)
		assert.Equal(t, 3, got.Version)
		assert.Empty(t, rec.savedVersions(), "a no-op publish must not save")
		assert.Empty(t, hooks.Recorded())
		assert.Zero(t, *unex)
	})

	t.Run("unpublish emits one PostUnpublished, only on a transition", func(t *testing.T) {
		store := fakes.NewBlog()
		svc, unex, hooks := newHooked(t, store, fakes.UnitOfWork)
		ctx, tc := tenantCtx(t)
		draft := seedDraft(t, store, tc)
		live := seedPublished(t, store, tc)

		_, err := svc.Unpublish(ctx, draft.ID, 0)
		require.NoError(t, err)
		require.Empty(t, hooks.Recorded(), "unpublishing a draft is a no-op")

		_, err = svc.Unpublish(ctx, live.ID, 0)
		require.NoError(t, err)
		calls := hooks.Recorded()
		require.Len(t, calls, 1)
		assert.Equal(t, events.PostUnpublished, calls[0].Type)
		assert.True(t, calls[0].InTx)
		data, ok := calls[0].Data.(events.PostUnpublishedV1)
		require.True(t, ok, "got %T", calls[0].Data)
		assert.Equal(t, live.ID, data.ID)
		assert.Equal(t, 4, data.Version)
		assert.Zero(t, *unex)
	})

	t.Run("delete emits PostDeleted with WasPublished", func(t *testing.T) {
		store := fakes.NewBlog()
		svc, unex, hooks := newHooked(t, store, fakes.UnitOfWork)
		ctx, tc := tenantCtx(t)
		live := seedPublished(t, store, tc)
		draft := seedDraft(t, store, tc)

		require.NoError(t, svc.Delete(ctx, live.ID, 0))
		require.NoError(t, svc.Delete(ctx, draft.ID, 0))

		calls := hooks.Recorded()
		require.Len(t, calls, 2)
		for _, c := range calls {
			assert.Equal(t, events.PostDeleted, c.Type)
			assert.True(t, c.InTx)
		}
		assert.Equal(t, events.PostDeletedV1{ID: live.ID, Slug: live.Slug, WasPublished: true}, calls[0].Data)
		assert.Equal(t, events.PostDeletedV1{ID: draft.ID, Slug: draft.Slug, WasPublished: false}, calls[1].Data)
		assert.Zero(t, store.Len())
		assert.Zero(t, *unex)
	})

	t.Run("update emits nothing", func(t *testing.T) {
		store := fakes.NewBlog()
		svc, unex, hooks := newHooked(t, store, fakes.UnitOfWork)
		ctx, tc := tenantCtx(t)
		p := seedPublished(t, store, tc)

		_, err := svc.Update(ctx, p.ID, "Edited", "", "body", p.CategoryID, 0)
		require.NoError(t, err)
		_, err = svc.UpdateWithTags(ctx, p.ID, "Edited", "", "body", p.CategoryID, []uuid.UUID{uuid.New()}, 0)
		require.NoError(t, err)
		_, err = svc.SetTags(ctx, p.ID, nil)
		require.NoError(t, err)
		assert.Empty(t, hooks.Recorded())
		assert.Zero(t, *unex)
	})

	t.Run("a post with no tags marshals an empty tag_ids array", func(t *testing.T) {
		store := fakes.NewBlog()
		svc, _, hooks := newHooked(t, store, fakes.UnitOfWork)
		ctx, tc := tenantCtx(t)
		p := seedDraft(t, store, tc)
		require.Nil(t, p.TagIDs)

		_, err := svc.Publish(ctx, p.ID, 0)
		require.NoError(t, err)
		calls := hooks.Recorded()
		require.Len(t, calls, 1)
		raw, err := json.Marshal(calls[0].Data)
		require.NoError(t, err)
		assert.Contains(t, string(raw), `"tag_ids":[]`)
	})

	t.Run("an Enqueue failure fails the transition as unexpected", func(t *testing.T) {
		store := fakes.NewBlog()
		svc, unex, hooks := newHooked(t, store, fakes.UnitOfWork)
		hooks.Err = errors.New("outbox down")
		ctx, tc := tenantCtx(t)
		p := seedDraft(t, store, tc)

		_, err := svc.Publish(ctx, p.ID, 0)
		require.Error(t, err)
		assert.Equal(t, 1, *unex)
	})
}

func TestService_SnapshotTimesAreWholeSeconds(t *testing.T) {
	store := fakes.NewBlog()
	svc, _, hooks := newHooked(t, store, fakes.UnitOfWork)
	ctx, tc := tenantCtx(t)
	p := seedDraft(t, store, tc)

	_, err := svc.Publish(ctx, p.ID, 0)
	require.NoError(t, err)
	calls := hooks.Recorded()
	require.Len(t, calls, 1)
	data, ok := calls[0].Data.(events.PostPublishedV1)
	require.True(t, ok, "got %T", calls[0].Data)

	assert.Zero(t, data.UpdatedAt.Nanosecond(), "UpdatedAt must be whole seconds, matching the envelope's created_at precision")
	require.NotNil(t, data.FirstPublishedAt)
	assert.Zero(t, data.FirstPublishedAt.Nanosecond(), "FirstPublishedAt must be whole seconds, matching the envelope's created_at precision")
}

func TestService_TransitionVersionGuard(t *testing.T) {
	t.Run("a stale caller version fails before the no-op check", func(t *testing.T) {
		store := fakes.NewBlog()
		svc, unex, hooks := newHooked(t, store, fakes.UnitOfWork)
		ctx, tc := tenantCtx(t)
		p := seedDraft(t, store, tc)

		_, err := svc.Unpublish(ctx, p.ID, p.Version+1)
		assert.True(t, blog.IsStaleVersionError(err), "got %T: %v", err, err)
		assert.Empty(t, hooks.Recorded())
		assert.Zero(t, *unex)
	})

	t.Run("ifVersion 0 saves against the loaded version", func(t *testing.T) {
		store := fakes.NewBlog()
		rec := &recordingStore{Blog: store}
		svc, _, _ := newHooked(t, rec, fakes.UnitOfWork)
		ctx, tc := tenantCtx(t)
		p := seedDraft(t, store, tc)

		_, err := svc.Publish(ctx, p.ID, 0)
		require.NoError(t, err)
		assert.Equal(t, []int{p.Version}, rec.savedVersions())

		live := seedPublished(t, store, tc)
		require.NoError(t, svc.Delete(ctx, live.ID, 0))
		_, err = store.ByID(ctx, live.ID)
		assert.True(t, blog.IsNotFoundError(err))
	})

	t.Run("a lost race with ifVersion 0 reloads once and emits nothing", func(t *testing.T) {
		store := fakes.NewBlog()
		rec := &recordingStore{Blog: store, raceFirst: true}
		svc, unex, hooks := newHooked(t, rec, fakes.UnitOfWork)
		ctx, tc := tenantCtx(t)
		p := seedDraft(t, store, tc)

		got, err := svc.Publish(ctx, p.ID, 0)
		require.NoError(t, err)
		assert.Equal(t, blog.StatusPublished, got.Status)
		assert.Equal(t, p.Version+1, got.Version)
		assert.Equal(t, []int{p.Version}, rec.savedVersions(), "the retry must see the post published and not save")
		assert.Empty(t, hooks.Recorded())
		assert.Zero(t, *unex)
	})

	t.Run("a lost race with a caller version is stale", func(t *testing.T) {
		store := fakes.NewBlog()
		rec := &recordingStore{Blog: store, raceFirst: true}
		svc, unex, hooks := newHooked(t, rec, fakes.UnitOfWork)
		ctx, tc := tenantCtx(t)
		p := seedDraft(t, store, tc)

		_, err := svc.Publish(ctx, p.ID, p.Version)
		assert.True(t, blog.IsStaleVersionError(err), "got %T: %v", err, err)
		assert.Len(t, rec.savedVersions(), 1, "a caller version is never retried")
		assert.Empty(t, hooks.Recorded())
		assert.Zero(t, *unex)
	})
}

func TestService_BareIDChecksProject(t *testing.T) {
	store := fakes.NewBlog()
	svc, unex, hooks := newHooked(t, store, fakes.UnitOfWork)
	ctx, tc := tenantCtx(t)
	sibling := tc
	sibling.ProjectID = uuid.New()
	p := seedPublished(t, store, sibling)

	_, err := store.ByID(ctx, p.ID)
	require.NoError(t, err, "the fake must not filter by project, or this test cannot fail")

	_, err = svc.ByID(ctx, p.ID)
	assert.True(t, blog.IsNotFoundError(err), "ByID: got %T: %v", err, err)
	_, err = svc.Publish(ctx, p.ID, 0)
	assert.True(t, blog.IsNotFoundError(err), "Publish: got %T: %v", err, err)
	_, err = svc.Unpublish(ctx, p.ID, 0)
	assert.True(t, blog.IsNotFoundError(err), "Unpublish: got %T: %v", err, err)
	err = svc.Delete(ctx, p.ID, 0)
	assert.True(t, blog.IsNotFoundError(err), "Delete: got %T: %v", err, err)

	assert.Equal(t, 1, store.Len())
	assert.Empty(t, hooks.Recorded())
	assert.Zero(t, *unex)
}

func TestService_Locate(t *testing.T) {
	store := fakes.NewBlog()
	svc, unex := newSvc(t, store)
	_, tc := tenantCtx(t)
	p := seedDraft(t, store, tc)

	orgOnly := tenant.Into(context.Background(), tenant.Context{OrgID: tc.OrgID, UserID: tc.UserID})
	got, err := svc.Locate(orgOnly, p.ID)
	require.NoError(t, err)
	assert.Equal(t, p.ID, got.ID)

	_, err = svc.ByID(orgOnly, p.ID)
	assert.True(t, blog.IsNotFoundError(err), "ByID keeps the project check: got %T: %v", err, err)

	otherOrg := tenant.Into(context.Background(), tenant.Context{OrgID: uuid.New(), UserID: tc.UserID})
	_, err = svc.Locate(otherOrg, p.ID)
	assert.True(t, blog.IsNotFoundError(err), "got %T: %v", err, err)
	assert.Zero(t, *unex)
}
