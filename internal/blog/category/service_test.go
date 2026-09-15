package category_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	apperrorv1 "altalune.id/yasaku/gen/go/apperror/v1"
	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/blog/category"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/testutil/fakes"
)

func newSvc(t *testing.T, store category.Store) (*category.Service, *int) {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	calls := 0
	unexpected := func(_ context.Context, _ string, err error, _ ...any) *apperror.AppError {
		calls++
		return apperror.New("yasaku.unexpected", err.Error(), codes.Internal,
			&apperrorv1.ErrorDetail{Code: "yasaku.unexpected"}).WithCause(err)
	}
	return category.NewService(store, log, unexpected), &calls
}

func svcCtx(t *testing.T) (context.Context, tenant.Context) {
	t.Helper()
	tc := tenant.Context{OrgID: uuid.New(), ProjectID: uuid.New(), UserID: uuid.New()}
	return tenant.Into(t.Context(), tc), tc
}

func TestService_Create(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		svc, unex := newSvc(t, fakes.NewCategory())
		ctx, tc := svcCtx(t)

		got, err := svc.Create(ctx, "  Release Notes  ", "")
		require.NoError(t, err)
		assert.Equal(t, "Release Notes", got.Name)
		assert.Equal(t, "release-notes", got.Slug)
		assert.Equal(t, tc.OrgID, got.OrgID)
		assert.Equal(t, tc.ProjectID, got.ProjectID)
		assert.Zero(t, *unex)
	})

	t.Run("invalid name bubbles the typed error", func(t *testing.T) {
		svc, unex := newSvc(t, fakes.NewCategory())
		ctx, _ := svcCtx(t)

		_, err := svc.Create(ctx, "   ", "")
		assert.True(t, category.IsInvalidNameError(err), "got %T: %v", err, err)
		assert.Zero(t, *unex, "a validation failure is expected, not unexpected")
	})

	t.Run("duplicate slug bubbles AlreadyExistsError", func(t *testing.T) {
		svc, unex := newSvc(t, fakes.NewCategory())
		ctx, _ := svcCtx(t)

		_, err := svc.Create(ctx, "Release Notes", "")
		require.NoError(t, err)
		_, err = svc.Create(ctx, "Release Notes", "")
		assert.True(t, category.IsAlreadyExistsError(err), "got %T: %v", err, err)
		assert.Zero(t, *unex)
	})

	t.Run("store failure is reported as unexpected", func(t *testing.T) {
		store := fakes.NewCategory()
		store.SaveFn = func(context.Context, *category.Category) error { return errors.New("boom") }
		svc, unex := newSvc(t, store)
		ctx, _ := svcCtx(t)

		_, err := svc.Create(ctx, "Release Notes", "")
		require.Error(t, err)
		assert.Equal(t, 1, *unex)
	})

	t.Run("missing tenant scope is refused", func(t *testing.T) {
		svc, unex := newSvc(t, fakes.NewCategory())
		_, err := svc.Create(context.Background(), "Release Notes", "")
		require.Error(t, err)
		assert.Zero(t, *unex)
	})
}

func TestService_Rename(t *testing.T) {
	t.Run("keeps the slug stable", func(t *testing.T) {
		svc, unex := newSvc(t, fakes.NewCategory())
		ctx, _ := svcCtx(t)

		c, err := svc.Create(ctx, "Release Notes", "")
		require.NoError(t, err)

		got, err := svc.Rename(ctx, c.ID, "Changelog")
		require.NoError(t, err)
		assert.Equal(t, "Changelog", got.Name)
		assert.Equal(t, "release-notes", got.Slug)
		assert.Zero(t, *unex)

		reread, err := svc.ByID(ctx, c.ID)
		require.NoError(t, err)
		assert.Equal(t, "Changelog", reread.Name)
	})

	t.Run("unknown id is NotFoundError", func(t *testing.T) {
		svc, unex := newSvc(t, fakes.NewCategory())
		ctx, _ := svcCtx(t)

		_, err := svc.Rename(ctx, uuid.New(), "Changelog")
		assert.True(t, category.IsNotFoundError(err), "got %T: %v", err, err)
		assert.Zero(t, *unex)
	})

	t.Run("invalid name bubbles the typed error", func(t *testing.T) {
		svc, unex := newSvc(t, fakes.NewCategory())
		ctx, _ := svcCtx(t)

		c, err := svc.Create(ctx, "Release Notes", "")
		require.NoError(t, err)
		_, err = svc.Rename(ctx, c.ID, "  ")
		assert.True(t, category.IsInvalidNameError(err), "got %T: %v", err, err)
		assert.Zero(t, *unex)
	})

	t.Run("another tenant's category is invisible", func(t *testing.T) {
		store := fakes.NewCategory()
		svc, unex := newSvc(t, store)
		ownerCtx, _ := svcCtx(t)
		c, err := svc.Create(ownerCtx, "Release Notes", "")
		require.NoError(t, err)

		otherCtx, _ := svcCtx(t)
		_, err = svc.Rename(otherCtx, c.ID, "Changelog")
		assert.True(t, category.IsNotFoundError(err), "got %T: %v", err, err)
		assert.Zero(t, *unex)
	})
}

func TestService_List(t *testing.T) {
	t.Run("scoped to the caller's project, newest first", func(t *testing.T) {
		store := fakes.NewCategory()
		svc, unex := newSvc(t, store)
		ctx, tc := svcCtx(t)

		base := time.Now().UTC().Add(-time.Hour)
		for i, name := range []string{"oldest", "middle", "newest"} {
			c, err := category.New(tc.OrgID, tc.ProjectID, name, "")
			require.NoError(t, err)
			c.CreatedAt = base.Add(time.Duration(i) * time.Minute)
			require.NoError(t, store.Save(ctx, c))
		}
		otherCtx, _ := svcCtx(t)
		_, err := svc.Create(otherCtx, "not mine", "")
		require.NoError(t, err)

		got, err := svc.List(ctx)
		require.NoError(t, err)
		require.Len(t, got, 3)
		assert.Equal(t, []string{"newest", "middle", "oldest"},
			[]string{got[0].Name, got[1].Name, got[2].Name})
		assert.Zero(t, *unex)
	})

	t.Run("store failure is reported as unexpected", func(t *testing.T) {
		store := fakes.NewCategory()
		store.ListFn = func(context.Context, uuid.UUID, uuid.UUID) ([]*category.Category, error) {
			return nil, errors.New("boom")
		}
		svc, unex := newSvc(t, store)
		ctx, _ := svcCtx(t)

		_, err := svc.List(ctx)
		require.Error(t, err)
		assert.Equal(t, 1, *unex)
	})
}

func TestService_ByID(t *testing.T) {
	t.Run("unknown id is NotFoundError", func(t *testing.T) {
		svc, unex := newSvc(t, fakes.NewCategory())
		ctx, _ := svcCtx(t)

		_, err := svc.ByID(ctx, uuid.New())
		assert.True(t, category.IsNotFoundError(err), "got %T: %v", err, err)
		assert.Zero(t, *unex)
	})
}

func TestService_Delete(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		svc, unex := newSvc(t, fakes.NewCategory())
		ctx, _ := svcCtx(t)

		c, err := svc.Create(ctx, "Gone", "")
		require.NoError(t, err)
		require.NoError(t, svc.Delete(ctx, c.ID))

		_, err = svc.ByID(ctx, c.ID)
		assert.True(t, category.IsNotFoundError(err), "got %T: %v", err, err)
		assert.Zero(t, *unex)
	})

	t.Run("in use bubbles InUseError", func(t *testing.T) {
		store := fakes.NewCategory()
		svc, unex := newSvc(t, store)
		ctx, _ := svcCtx(t)

		c, err := svc.Create(ctx, "Has Posts", "")
		require.NoError(t, err)
		store.DeleteFn = func(_ context.Context, id uuid.UUID) error {
			return &category.InUseError{ID: id.String()}
		}

		err = svc.Delete(ctx, c.ID)
		assert.True(t, category.IsInUseError(err), "got %T: %v", err, err)
		assert.Zero(t, *unex, "a refused delete is expected, not unexpected")
	})

	t.Run("unknown id is NotFoundError", func(t *testing.T) {
		svc, unex := newSvc(t, fakes.NewCategory())
		ctx, _ := svcCtx(t)

		assert.True(t, category.IsNotFoundError(svc.Delete(ctx, uuid.New())))
		assert.Zero(t, *unex)
	})

	t.Run("store failure is reported as unexpected", func(t *testing.T) {
		store := fakes.NewCategory()
		svc, unex := newSvc(t, store)
		ctx, _ := svcCtx(t)

		c, err := svc.Create(ctx, "Boom", "")
		require.NoError(t, err)
		store.DeleteFn = func(context.Context, uuid.UUID) error { return errors.New("boom") }

		require.Error(t, svc.Delete(ctx, c.ID))
		assert.Equal(t, 1, *unex)
	})
}

func TestErrors_ToAppError(t *testing.T) {
	cases := []struct {
		name     string
		err      interface{ ToAppError() *apperror.AppError }
		code     string
		grpcCode codes.Code
	}{
		{"not found", &category.NotFoundError{ID: "x"}, apperror.CodeCategoryNotFound, codes.NotFound},
		{"already exists", &category.AlreadyExistsError{Slug: "s"}, apperror.CodeCategoryAlreadyExists, codes.AlreadyExists},
		{"invalid name", &category.InvalidNameError{Reason: "empty"}, apperror.CodeCategoryInvalidName, codes.InvalidArgument},
		{"in use", &category.InUseError{ID: "x"}, apperror.CodeCategoryInUse, codes.FailedPrecondition},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := tc.err.ToAppError()
			assert.Equal(t, tc.code, app.Code())
			assert.Equal(t, tc.grpcCode, app.GRPCCode())
		})
	}
}
