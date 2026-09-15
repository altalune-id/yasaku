package blog_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	apperrorv1 "altalune.id/yasaku/gen/go/apperror/v1"
	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/blog"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/testutil/fakes"
)

func newSvc(t *testing.T, store blog.Store) (*blog.Service, *int) {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	calls := 0
	unexpected := func(_ context.Context, _ string, err error, _ ...any) *apperror.AppError {
		calls++
		return apperror.New("yasaku.unexpected", err.Error(), codes.Internal,
			&apperrorv1.ErrorDetail{Code: "yasaku.unexpected"}).WithCause(err)
	}
	return blog.NewService(store, log, unexpected), &calls
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
		store.SaveFn = func(context.Context, *blog.Post) error { return errors.New("boom") }
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

		got, err := svc.Update(ctx, created.ID, "Goodbye", "custom-slug", "new body", newCat)
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
		published, err := svc.Publish(ctx, created.ID)
		require.NoError(t, err)

		got, err := svc.Update(ctx, created.ID, "Edited", "", "body", cat)
		require.NoError(t, err)
		assert.Equal(t, blog.StatusPublished, got.Status)
		require.NotNil(t, got.FirstPublishedAt)
		assert.True(t, got.FirstPublishedAt.Equal(*published.FirstPublishedAt))
		assert.Zero(t, *unex)
	})

	t.Run("unknown id", func(t *testing.T) {
		svc, unex := newSvc(t, fakes.NewBlog())
		ctx, _ := tenantCtx(t)

		_, err := svc.Update(ctx, uuid.New(), "Title", "", "body", uuid.New())
		assert.True(t, blog.IsNotFoundError(err), "got %T: %v", err, err)
		assert.Zero(t, *unex)
	})

	t.Run("invalid title", func(t *testing.T) {
		svc, unex := newSvc(t, fakes.NewBlog())
		ctx, _ := tenantCtx(t)

		created, err := svc.Create(ctx, uuid.New(), "Hello", "", "body")
		require.NoError(t, err)

		_, err = svc.Update(ctx, created.ID, "  ", "", "body", uuid.New())
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

		_, err = svc.Update(ctx, second.ID, "Free", first.Slug, "body", cat)
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

		first, err := svc.Publish(ctx, created.ID)
		require.NoError(t, err)
		assert.Equal(t, blog.StatusPublished, first.Status)
		require.NotNil(t, first.FirstPublishedAt)
		firstAt := *first.FirstPublishedAt

		drafted, err := svc.Unpublish(ctx, created.ID)
		require.NoError(t, err)
		assert.Equal(t, blog.StatusDraft, drafted.Status)
		require.NotNil(t, drafted.FirstPublishedAt, "unpublishing retains the first publication")

		again, err := svc.Publish(ctx, created.ID)
		require.NoError(t, err)
		assert.True(t, again.FirstPublishedAt.Equal(firstAt), "republishing must not move FirstPublishedAt")
		assert.Zero(t, *unex)
	})

	t.Run("unknown id", func(t *testing.T) {
		svc, unex := newSvc(t, fakes.NewBlog())
		ctx, _ := tenantCtx(t)

		_, err := svc.Publish(ctx, uuid.New())
		assert.True(t, blog.IsNotFoundError(err), "got %T: %v", err, err)

		_, err = svc.Unpublish(ctx, uuid.New())
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
	_, err = svc.Publish(ctx, pub.ID)
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
		require.NoError(t, svc.Delete(ctx, created.ID))
		assert.Zero(t, store.Len())
		assert.Zero(t, *unex)
	})

	t.Run("unknown id", func(t *testing.T) {
		svc, unex := newSvc(t, fakes.NewBlog())
		ctx, _ := tenantCtx(t)

		assert.True(t, blog.IsNotFoundError(svc.Delete(ctx, uuid.New())), "unknown id must be NotFoundError")
		assert.Zero(t, *unex)
	})

	t.Run("store failure is reported as unexpected", func(t *testing.T) {
		store := fakes.NewBlog()
		store.DeleteFn = func(context.Context, uuid.UUID) error { return errors.New("boom") }
		svc, unex := newSvc(t, store)
		ctx, _ := tenantCtx(t)

		require.Error(t, svc.Delete(ctx, uuid.New()))
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
