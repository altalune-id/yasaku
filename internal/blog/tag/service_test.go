package tag_test

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
	"altalune.id/yasaku/internal/blog/tag"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/testutil/fakes"
)

func newSvc(t *testing.T, store tag.Store) (*tag.Service, *int) {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	calls := 0
	unexpected := func(_ context.Context, _ string, err error, _ ...any) *apperror.AppError {
		calls++
		return apperror.New("yasaku.unexpected", err.Error(), codes.Internal,
			&apperrorv1.ErrorDetail{Code: "yasaku.unexpected"}).WithCause(err)
	}
	return tag.NewService(store, log, unexpected), &calls
}

func tenantCtx(t *testing.T) (context.Context, tenant.Context) {
	t.Helper()
	tc := tenant.Context{OrgID: uuid.New(), ProjectID: uuid.New(), UserID: uuid.New()}
	return tenant.Into(context.Background(), tc), tc
}

func TestService_Create(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		store := fakes.NewTag()
		svc, unex := newSvc(t, store)
		ctx, tc := tenantCtx(t)

		got, err := svc.Create(ctx, "  Go Lang  ", "")
		require.NoError(t, err)
		assert.Equal(t, "Go Lang", got.Name)
		assert.Equal(t, "go-lang", got.Slug)
		assert.Equal(t, tc.OrgID, got.OrgID)
		assert.Equal(t, tc.ProjectID, got.ProjectID)
		assert.Zero(t, *unex)
	})

	t.Run("invalid name is a typed error, not unexpected", func(t *testing.T) {
		svc, unex := newSvc(t, fakes.NewTag())
		ctx, _ := tenantCtx(t)

		_, err := svc.Create(ctx, "   ", "")
		assert.True(t, tag.IsInvalidNameError(err), "got %T: %v", err, err)
		assert.Zero(t, *unex)
	})

	t.Run("duplicate slug surfaces AlreadyExists, not unexpected", func(t *testing.T) {
		store := fakes.NewTag()
		svc, unex := newSvc(t, store)
		ctx, _ := tenantCtx(t)

		_, err := svc.Create(ctx, "Go Lang", "")
		require.NoError(t, err)

		_, err = svc.Create(ctx, "go lang", "")
		assert.True(t, tag.IsAlreadyExistsError(err), "got %T: %v", err, err)
		assert.Zero(t, *unex)
	})

	t.Run("store failure is reported as unexpected", func(t *testing.T) {
		store := fakes.NewTag()
		boom := errors.New("boom")
		store.SaveFn = func(context.Context, *tag.Tag) error { return boom }
		svc, unex := newSvc(t, store)
		ctx, _ := tenantCtx(t)

		_, err := svc.Create(ctx, "Go", "")
		require.Error(t, err)
		assert.Equal(t, 1, *unex)
	})

	t.Run("missing tenant", func(t *testing.T) {
		svc, unex := newSvc(t, fakes.NewTag())
		_, err := svc.Create(context.Background(), "Go", "")
		assert.True(t, tenant.IsMissingError(err), "got %T: %v", err, err)
		assert.Zero(t, *unex)
	})
}

func TestService_EnsureByName(t *testing.T) {
	t.Run("creates when absent", func(t *testing.T) {
		store := fakes.NewTag()
		svc, unex := newSvc(t, store)
		ctx, tc := tenantCtx(t)

		got, err := svc.EnsureByName(ctx, tc.OrgID, tc.ProjectID, "Go Lang")
		require.NoError(t, err)
		assert.Equal(t, "go-lang", got.Slug)
		assert.Equal(t, 1, store.Len())
		assert.Zero(t, *unex)
	})

	t.Run("is idempotent across name casing and spacing", func(t *testing.T) {
		store := fakes.NewTag()
		svc, unex := newSvc(t, store)
		ctx, tc := tenantCtx(t)

		first, err := svc.EnsureByName(ctx, tc.OrgID, tc.ProjectID, "Go Lang")
		require.NoError(t, err)

		second, err := svc.EnsureByName(ctx, tc.OrgID, tc.ProjectID, "  go   lang  ")
		require.NoError(t, err)

		assert.Equal(t, first.ID, second.ID, "a second EnsureByName must reuse the existing tag")
		assert.Equal(t, 1, store.Len(), "EnsureByName created a duplicate row")
		assert.Equal(t, "Go Lang", second.Name, "the existing display name must win")
		assert.Zero(t, *unex)
	})

	t.Run("scopes by project", func(t *testing.T) {
		store := fakes.NewTag()
		svc, _ := newSvc(t, store)
		ctx, tc := tenantCtx(t)
		otherProject := uuid.New()

		a, err := svc.EnsureByName(ctx, tc.OrgID, tc.ProjectID, "Go")
		require.NoError(t, err)
		b, err := svc.EnsureByName(ctx, tc.OrgID, otherProject, "Go")
		require.NoError(t, err)

		assert.NotEqual(t, a.ID, b.ID, "the same name in another project is a different tag")
		assert.Equal(t, 2, store.Len())
	})

	t.Run("invalid name is rejected before any store call", func(t *testing.T) {
		store := fakes.NewTag()
		store.BySlugFn = func(context.Context, uuid.UUID, uuid.UUID, string) (*tag.Tag, error) {
			t.Fatal("BySlug must not be reached for an invalid name")
			return nil, nil
		}
		svc, unex := newSvc(t, store)
		ctx, tc := tenantCtx(t)

		_, err := svc.EnsureByName(ctx, tc.OrgID, tc.ProjectID, strings.Repeat("a", 101))
		assert.True(t, tag.IsInvalidNameError(err), "got %T: %v", err, err)
		assert.Zero(t, *unex)
	})

	t.Run("a losing create race re-reads instead of surfacing AlreadyExists", func(t *testing.T) {
		store := fakes.NewTag()
		winner, err := tag.New(uuid.New(), uuid.New(), "Go Lang", "")
		require.NoError(t, err)

		svc, unex := newSvc(t, store)
		ctx, _ := tenantCtx(t)

		store.SaveFn = func(_ context.Context, t *tag.Tag) error {
			store.Seed(winner)
			return &tag.AlreadyExistsError{Slug: t.Slug}
		}
		store.BySlugFn = nil

		got, err := svc.EnsureByName(ctx, winner.OrgID, winner.ProjectID, "go lang")
		require.NoError(t, err, "a lost race must resolve to the winning row")
		assert.Equal(t, winner.ID, got.ID)
		assert.Zero(t, *unex)
	})

	t.Run("a non-NotFound lookup failure is unexpected", func(t *testing.T) {
		store := fakes.NewTag()
		store.BySlugFn = func(context.Context, uuid.UUID, uuid.UUID, string) (*tag.Tag, error) {
			return nil, errors.New("boom")
		}
		svc, unex := newSvc(t, store)
		ctx, tc := tenantCtx(t)

		_, err := svc.EnsureByName(ctx, tc.OrgID, tc.ProjectID, "Go")
		require.Error(t, err)
		assert.Equal(t, 1, *unex)
	})
}

func TestService_Rename(t *testing.T) {
	t.Run("keeps the slug stable", func(t *testing.T) {
		store := fakes.NewTag()
		svc, unex := newSvc(t, store)
		ctx, _ := tenantCtx(t)

		created, err := svc.Create(ctx, "Go Lang", "")
		require.NoError(t, err)

		got, err := svc.Rename(ctx, created.ID, "Golang")
		require.NoError(t, err)
		assert.Equal(t, "Golang", got.Name)
		assert.Equal(t, created.Slug, got.Slug)
		assert.Zero(t, *unex)
	})

	t.Run("unknown id", func(t *testing.T) {
		svc, unex := newSvc(t, fakes.NewTag())
		ctx, _ := tenantCtx(t)

		_, err := svc.Rename(ctx, uuid.New(), "x")
		assert.True(t, tag.IsNotFoundError(err), "got %T: %v", err, err)
		assert.Zero(t, *unex)
	})

	t.Run("invalid name", func(t *testing.T) {
		store := fakes.NewTag()
		svc, unex := newSvc(t, store)
		ctx, _ := tenantCtx(t)

		created, err := svc.Create(ctx, "Go", "")
		require.NoError(t, err)

		_, err = svc.Rename(ctx, created.ID, "  ")
		assert.True(t, tag.IsInvalidNameError(err), "got %T: %v", err, err)
		assert.Zero(t, *unex)
	})
}

func TestService_ListAndByID(t *testing.T) {
	store := fakes.NewTag()
	svc, unex := newSvc(t, store)
	ctx, tc := tenantCtx(t)

	for _, name := range []string{"a", "b", "c"} {
		_, err := svc.Create(ctx, name, "")
		require.NoError(t, err)
	}
	other, err := tag.New(uuid.New(), uuid.New(), "elsewhere", "")
	require.NoError(t, err)
	store.Seed(other)

	got, err := svc.List(ctx)
	require.NoError(t, err)
	assert.Len(t, got, 3, "List must not leak another tenant's tags")

	one, err := svc.ByID(ctx, got[0].ID)
	require.NoError(t, err)
	assert.Equal(t, got[0].ID, one.ID)

	_, err = svc.ByID(ctx, uuid.New())
	assert.True(t, tag.IsNotFoundError(err), "got %T: %v", err, err)

	_ = tc
	assert.Zero(t, *unex)
}

func TestService_Delete(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		store := fakes.NewTag()
		svc, unex := newSvc(t, store)
		ctx, _ := tenantCtx(t)

		created, err := svc.Create(ctx, "Go", "")
		require.NoError(t, err)
		require.NoError(t, svc.Delete(ctx, created.ID))
		assert.Zero(t, store.Len())
		assert.Zero(t, *unex)
	})

	t.Run("unknown id", func(t *testing.T) {
		svc, unex := newSvc(t, fakes.NewTag())
		ctx, _ := tenantCtx(t)
		err := svc.Delete(ctx, uuid.New())
		assert.True(t, tag.IsNotFoundError(err), "got %T: %v", err, err)
		assert.Zero(t, *unex)
	})

	t.Run("in use surfaces InUseError, not unexpected", func(t *testing.T) {
		store := fakes.NewTag()
		svc, unex := newSvc(t, store)
		ctx, tc := tenantCtx(t)

		attached, err := tag.New(tc.OrgID, tc.ProjectID, "attached", "")
		require.NoError(t, err)
		store.Seed(attached)
		store.DeleteFn = func(context.Context, uuid.UUID) error {
			return &tag.InUseError{ID: attached.ID.String()}
		}

		err = svc.Delete(ctx, attached.ID)
		assert.True(t, tag.IsInUseError(err), "got %T: %v", err, err)
		assert.Zero(t, *unex)
	})
}

func TestErrors_AppErrorCodes(t *testing.T) {
	assert.Equal(t, apperror.CodeTagNotFound, (&tag.NotFoundError{}).ToAppError().Code())
	assert.Equal(t, codes.NotFound, (&tag.NotFoundError{}).ToAppError().GRPCCode())
	assert.Equal(t, apperror.CodeTagAlreadyExists, (&tag.AlreadyExistsError{}).ToAppError().Code())
	assert.Equal(t, codes.AlreadyExists, (&tag.AlreadyExistsError{}).ToAppError().GRPCCode())
	assert.Equal(t, apperror.CodeTagInvalidName, (&tag.InvalidNameError{}).ToAppError().Code())
	assert.Equal(t, codes.InvalidArgument, (&tag.InvalidNameError{}).ToAppError().GRPCCode())
	assert.Equal(t, apperror.CodeTagInUse, (&tag.InUseError{}).ToAppError().Code())
	assert.Equal(t, codes.FailedPrecondition, (&tag.InUseError{}).ToAppError().GRPCCode())
}

// TestService_CrossProjectIsInvisible pins the service-level scope check. The Store filters by
// org, not by project, so within one org a guessed or leaked tag id from a sibling project would
// otherwise resolve. The fake deliberately does NOT filter ByID by project either — it is a plain
// map lookup — so what these assertions exercise is the service's own check and nothing else.
func TestService_CrossProjectIsInvisible(t *testing.T) {
	newPair := func(t *testing.T) (*tag.Service, *fakes.Tag, context.Context, *tag.Tag) {
		t.Helper()
		store := fakes.NewTag()
		svc, unex := newSvc(t, store)

		orgID := uuid.New()
		projectA := tenant.Context{OrgID: orgID, ProjectID: uuid.New(), UserID: uuid.New()}
		projectB := tenant.Context{OrgID: orgID, ProjectID: uuid.New(), UserID: uuid.New()}

		owned, err := tag.New(projectA.OrgID, projectA.ProjectID, "Project A Only", "")
		require.NoError(t, err)
		store.Seed(owned)

		// Guard the guard: without this the test could pass because the fake filtered.
		raw, err := store.ByID(t.Context(), owned.ID)
		require.NoError(t, err, "the fake must hand the row to any caller, or this test proves nothing")
		require.Equal(t, owned.ID, raw.ID)

		t.Cleanup(func() { assert.Zero(t, *unex, "a scope miss is expected, not unexpected") })
		return svc, store, tenant.Into(t.Context(), projectB), owned
	}

	t.Run("ByID from a sibling project is NotFound", func(t *testing.T) {
		svc, _, ctxB, owned := newPair(t)

		_, err := svc.ByID(ctxB, owned.ID)
		assert.True(t, tag.IsNotFoundError(err),
			"a sibling project's tag must be NotFound, got %T: %v", err, err)
	})

	t.Run("Delete from a sibling project does not delete it", func(t *testing.T) {
		svc, store, ctxB, owned := newPair(t)

		err := svc.Delete(ctxB, owned.ID)
		assert.True(t, tag.IsNotFoundError(err),
			"a sibling project must not delete the tag, got %T: %v", err, err)

		still, err := store.ByID(t.Context(), owned.ID)
		require.NoError(t, err, "the tag must survive the refused delete")
		assert.Equal(t, owned.ID, still.ID)
		assert.Equal(t, 1, store.Len())
	})

	t.Run("Rename from a sibling project does not rename it", func(t *testing.T) {
		svc, store, ctxB, owned := newPair(t)

		_, err := svc.Rename(ctxB, owned.ID, "Hijacked")
		assert.True(t, tag.IsNotFoundError(err),
			"a sibling project must not rename the tag, got %T: %v", err, err)

		still, err := store.ByID(t.Context(), owned.ID)
		require.NoError(t, err)
		assert.Equal(t, "Project A Only", still.Name, "the tag's name must be untouched")
	})

	t.Run("the owning project still reaches its own tag", func(t *testing.T) {
		store := fakes.NewTag()
		svc, unex := newSvc(t, store)
		ctx, tc := tenantCtx(t)

		owned, err := tag.New(tc.OrgID, tc.ProjectID, "Mine", "")
		require.NoError(t, err)
		store.Seed(owned)

		got, err := svc.ByID(ctx, owned.ID)
		require.NoError(t, err, "the scope check must not refuse the owner")
		assert.Equal(t, owned.ID, got.ID)

		renamed, err := svc.Rename(ctx, owned.ID, "Still Mine")
		require.NoError(t, err)
		assert.Equal(t, "Still Mine", renamed.Name)

		require.NoError(t, svc.Delete(ctx, owned.ID))
		assert.Zero(t, store.Len())
		assert.Zero(t, *unex)
	})
}
