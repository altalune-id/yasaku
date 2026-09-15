package wallet_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	apperrorv1 "altalune.id/yasaku/gen/go/apperror/v1"
	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/testutil/fakes"
	"altalune.id/yasaku/internal/wallet"
	"altalune.id/yasaku/money"
)

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

type counter struct{ n int }

func countingUnexpected(c *counter) apperror.UnexpectedFunc {
	return func(_ context.Context, _ string, err error, _ ...any) *apperror.AppError {
		c.n++
		return apperror.New("yasaku.unexpected", err.Error(), codes.Internal,
			&apperrorv1.ErrorDetail{Code: "yasaku.unexpected"}).WithCause(err)
	}
}

func newSvc(t *testing.T, store wallet.Store) (*wallet.Service, *counter) {
	t.Helper()
	c := &counter{}
	return wallet.NewService(store, testLogger(), countingUnexpected(c)), c
}

func svcCtx(t *testing.T) (context.Context, tenant.Context) {
	t.Helper()
	tc := tenant.Context{OrgID: uuid.New(), ProjectID: uuid.New(), UserID: uuid.New()}
	return tenant.Into(t.Context(), tc), tc
}

func mustCreate(ctx context.Context, t *testing.T, svc *wallet.Service, name string) *wallet.Wallet {
	t.Helper()
	w, err := svc.Create(ctx, wallet.Params{Name: name, Kind: wallet.KindBank, Currency: money.IDR})
	require.NoError(t, err)
	return w
}

func TestService_Create(t *testing.T) {
	t.Run("stamps the caller's tenant scope", func(t *testing.T) {
		svc, unex := newSvc(t, fakes.NewWallet())
		ctx, tc := svcCtx(t)

		w, err := svc.Create(ctx, wallet.Params{Name: "  BCA  ", Kind: wallet.KindBank, Currency: money.IDR})
		require.NoError(t, err)
		assert.Equal(t, "BCA", w.Name)
		assert.Equal(t, tc.OrgID, w.OrgID)
		assert.Equal(t, tc.ProjectID, w.ProjectID)
		assert.Zero(t, unex.n)
	})

	t.Run("normalises the currency code", func(t *testing.T) {
		svc, _ := newSvc(t, fakes.NewWallet())
		ctx, _ := svcCtx(t)

		w, err := svc.Create(ctx, wallet.Params{Name: "Revolut", Kind: wallet.KindBank, Currency: money.Currency("usd")})
		require.NoError(t, err)
		assert.Equal(t, money.Currency("USD"), w.Currency)
	})

	t.Run("an unknown currency is a typed money error", func(t *testing.T) {
		svc, unex := newSvc(t, fakes.NewWallet())
		ctx, _ := svcCtx(t)

		_, err := svc.Create(ctx, wallet.Params{Name: "Doge", Kind: wallet.KindBank, Currency: money.Currency("DOGE")})
		assert.True(t, money.IsUnknownCurrencyError(err), "got %T: %v", err, err)
		assert.Zero(t, unex.n)
	})

	t.Run("a duplicate active name bubbles AlreadyExistsError", func(t *testing.T) {
		svc, unex := newSvc(t, fakes.NewWallet())
		ctx, _ := svcCtx(t)

		mustCreate(ctx, t, svc, "BCA")
		_, err := svc.Create(ctx, wallet.Params{Name: "bca", Kind: wallet.KindBank, Currency: money.IDR})
		assert.True(t, wallet.IsAlreadyExistsError(err), "got %T: %v", err, err)
		assert.Zero(t, unex.n)
	})

	t.Run("a store failure is reported as unexpected", func(t *testing.T) {
		store := fakes.NewWallet()
		store.SaveFn = func(context.Context, *wallet.Wallet) error { return errors.New("boom") }
		svc, unex := newSvc(t, store)
		ctx, _ := svcCtx(t)

		_, err := svc.Create(ctx, wallet.Params{Name: "BCA", Kind: wallet.KindBank, Currency: money.IDR})
		require.Error(t, err)
		assert.Equal(t, 1, unex.n)
	})

	t.Run("without a tenant context it refuses", func(t *testing.T) {
		svc, _ := newSvc(t, fakes.NewWallet())
		_, err := svc.Create(t.Context(), wallet.Params{Name: "BCA", Kind: wallet.KindBank, Currency: money.IDR})
		require.Error(t, err)
	})
}

func TestService_ByID_RefusesSiblingProject(t *testing.T) {
	store := fakes.NewWallet()
	svc, _ := newSvc(t, store)
	ctxA, tcA := svcCtx(t)

	tcB := tenant.Context{OrgID: tcA.OrgID, ProjectID: uuid.New(), UserID: tcA.UserID}
	ctxB := tenant.Into(t.Context(), tcB)

	seeded := mustCreate(ctxB, t, svc, "Project B wallet")

	raw, err := store.ByID(t.Context(), seeded.ID)
	require.NoError(t, err, "the fake must not filter by project, or this test proves nothing")
	require.Equal(t, seeded.ID, raw.ID)

	_, err = svc.ByID(ctxA, seeded.ID)
	assert.True(t, wallet.IsNotFoundError(err), "got %T: %v", err, err)
}

func TestService_List(t *testing.T) {
	svc, _ := newSvc(t, fakes.NewWallet())
	ctx, _ := svcCtx(t)

	mustCreate(ctx, t, svc, "Mandiri")
	mustCreate(ctx, t, svc, "BCA")
	old := mustCreate(ctx, t, svc, "Jenius")
	_, err := svc.Archive(ctx, old.ID)
	require.NoError(t, err)

	active, err := svc.List(ctx, wallet.ListOpts{})
	require.NoError(t, err)
	names := make([]string, 0, len(active))
	for _, w := range active {
		names = append(names, w.Name)
	}
	assert.Equal(t, []string{"BCA", "Mandiri"}, names)

	all, err := svc.List(ctx, wallet.ListOpts{IncludeArchived: true})
	require.NoError(t, err)
	assert.Len(t, all, 3)
}

func TestService_RenameAndUpdate(t *testing.T) {
	t.Run("rename", func(t *testing.T) {
		svc, _ := newSvc(t, fakes.NewWallet())
		ctx, _ := svcCtx(t)
		w := mustCreate(ctx, t, svc, "BCA")

		got, err := svc.Rename(ctx, w.ID, "  Mandiri ")
		require.NoError(t, err)
		assert.Equal(t, "Mandiri", got.Name)
	})

	t.Run("rename onto a taken name bubbles AlreadyExistsError", func(t *testing.T) {
		svc, unex := newSvc(t, fakes.NewWallet())
		ctx, _ := svcCtx(t)
		mustCreate(ctx, t, svc, "BCA")
		w := mustCreate(ctx, t, svc, "Mandiri")

		_, err := svc.Rename(ctx, w.ID, "bca")
		assert.True(t, wallet.IsAlreadyExistsError(err), "got %T: %v", err, err)
		assert.Zero(t, unex.n)
	})

	t.Run("update", func(t *testing.T) {
		svc, _ := newSvc(t, fakes.NewWallet())
		ctx, _ := svcCtx(t)
		w := mustCreate(ctx, t, svc, "BCA")

		got, err := svc.Update(ctx, w.ID, wallet.KindSavings, "BCA Tahapan", true)
		require.NoError(t, err)
		assert.Equal(t, wallet.KindSavings, got.Kind)
		assert.True(t, got.ExcludeFromTotal)
	})

	t.Run("update on an archived wallet is refused", func(t *testing.T) {
		svc, unex := newSvc(t, fakes.NewWallet())
		ctx, _ := svcCtx(t)
		w := mustCreate(ctx, t, svc, "BCA")
		_, err := svc.Archive(ctx, w.ID)
		require.NoError(t, err)

		_, err = svc.Update(ctx, w.ID, wallet.KindCash, "", false)
		assert.True(t, wallet.IsArchivedError(err), "got %T: %v", err, err)
		assert.Zero(t, unex.n)
	})

	t.Run("a sibling project's wallet is not found", func(t *testing.T) {
		store := fakes.NewWallet()
		svc, _ := newSvc(t, store)
		ctxA, tcA := svcCtx(t)
		ctxB := tenant.Into(t.Context(), tenant.Context{OrgID: tcA.OrgID, ProjectID: uuid.New(), UserID: tcA.UserID})
		w := mustCreate(ctxB, t, svc, "Project B wallet")

		_, err := svc.Rename(ctxA, w.ID, "Hijacked")
		assert.True(t, wallet.IsNotFoundError(err), "got %T: %v", err, err)
	})
}

func TestService_ArchiveUnarchive(t *testing.T) {
	t.Run("archiving frees the name and unarchiving can collide", func(t *testing.T) {
		svc, unex := newSvc(t, fakes.NewWallet())
		ctx, _ := svcCtx(t)

		first := mustCreate(ctx, t, svc, "BCA")
		archived, err := svc.Archive(ctx, first.ID)
		require.NoError(t, err)
		assert.True(t, archived.IsArchived())

		second := mustCreate(ctx, t, svc, "BCA")
		require.NotEqual(t, first.ID, second.ID)

		_, err = svc.Unarchive(ctx, first.ID)
		assert.True(t, wallet.IsAlreadyExistsError(err), "got %T: %v", err, err)
		assert.Zero(t, unex.n)
	})

	t.Run("renaming an archived wallet escapes the collision", func(t *testing.T) {
		svc, unex := newSvc(t, fakes.NewWallet())
		ctx, _ := svcCtx(t)

		first := mustCreate(ctx, t, svc, "BCA")
		_, err := svc.Archive(ctx, first.ID)
		require.NoError(t, err)
		mustCreate(ctx, t, svc, "BCA")

		_, err = svc.Unarchive(ctx, first.ID)
		require.True(t, wallet.IsAlreadyExistsError(err), "fixture: the collision must exist first")

		renamed, err := svc.Rename(ctx, first.ID, "BCA (closed)")
		require.NoError(t, err, "an archived wallet must be renameable, or the collision is a dead end")
		assert.True(t, renamed.IsArchived())

		back, err := svc.Unarchive(ctx, first.ID)
		require.NoError(t, err)
		assert.False(t, back.IsArchived())
		assert.Equal(t, "BCA (closed)", back.Name)
		assert.Zero(t, unex.n)
	})

	t.Run("unarchive restores an uncontested name", func(t *testing.T) {
		svc, _ := newSvc(t, fakes.NewWallet())
		ctx, _ := svcCtx(t)
		w := mustCreate(ctx, t, svc, "BCA")
		_, err := svc.Archive(ctx, w.ID)
		require.NoError(t, err)

		got, err := svc.Unarchive(ctx, w.ID)
		require.NoError(t, err)
		assert.False(t, got.IsArchived())
	})

	t.Run("archive is idempotent", func(t *testing.T) {
		svc, _ := newSvc(t, fakes.NewWallet())
		ctx, _ := svcCtx(t)
		w := mustCreate(ctx, t, svc, "BCA")
		a, err := svc.Archive(ctx, w.ID)
		require.NoError(t, err)
		b, err := svc.Archive(ctx, w.ID)
		require.NoError(t, err)
		assert.Equal(t, *a.ArchivedAt, *b.ArchivedAt)
	})
}

func TestService_Delete(t *testing.T) {
	t.Run("removes the wallet", func(t *testing.T) {
		svc, _ := newSvc(t, fakes.NewWallet())
		ctx, _ := svcCtx(t)
		w := mustCreate(ctx, t, svc, "BCA")

		require.NoError(t, svc.Delete(ctx, w.ID))
		_, err := svc.ByID(ctx, w.ID)
		assert.True(t, wallet.IsNotFoundError(err))
	})

	t.Run("passes the store's InUseError through unchanged", func(t *testing.T) {
		store := fakes.NewWallet()
		svc, unex := newSvc(t, store)
		ctx, _ := svcCtx(t)
		w := mustCreate(ctx, t, svc, "BCA")
		store.DeleteFn = func(_ context.Context, id uuid.UUID) error { return &wallet.InUseError{ID: id.String()} }

		err := svc.Delete(ctx, w.ID)
		assert.True(t, wallet.IsInUseError(err), "got %T: %v", err, err)
		var inUse *wallet.InUseError
		require.ErrorAs(t, err, &inUse)
		assert.Equal(t, w.ID.String(), inUse.ID)
		assert.Zero(t, unex.n)
	})

	t.Run("a sibling project's wallet is not found and is never deleted", func(t *testing.T) {
		store := fakes.NewWallet()
		svc, _ := newSvc(t, store)
		ctxA, tcA := svcCtx(t)
		ctxB := tenant.Into(t.Context(), tenant.Context{OrgID: tcA.OrgID, ProjectID: uuid.New(), UserID: tcA.UserID})
		w := mustCreate(ctxB, t, svc, "Project B wallet")

		assert.True(t, wallet.IsNotFoundError(svc.Delete(ctxA, w.ID)))
		_, err := store.ByID(t.Context(), w.ID)
		require.NoError(t, err, "Delete must load the row before deleting, or the scope check is absent")
	})
}

func TestService_ResolveByName(t *testing.T) {
	setup := func(t *testing.T) (*wallet.Service, context.Context) {
		t.Helper()
		svc, _ := newSvc(t, fakes.NewWallet())
		ctx, _ := svcCtx(t)
		return svc, ctx
	}

	t.Run("exact case-insensitive match wins over a substring match", func(t *testing.T) {
		svc, ctx := setup(t)
		exact := mustCreate(ctx, t, svc, "BCA")
		mustCreate(ctx, t, svc, "BCA Syariah")

		got, err := svc.ResolveByName(ctx, "bca")
		require.NoError(t, err)
		assert.Equal(t, exact.ID, got.ID)
	})

	t.Run("a unique substring resolves", func(t *testing.T) {
		svc, ctx := setup(t)
		want := mustCreate(ctx, t, svc, "Bank Mandiri")
		mustCreate(ctx, t, svc, "BCA")

		got, err := svc.ResolveByName(ctx, "mandir")
		require.NoError(t, err)
		assert.Equal(t, want.ID, got.ID)
	})

	t.Run("two substring matches are ambiguous with sorted candidates", func(t *testing.T) {
		svc, ctx := setup(t)
		mustCreate(ctx, t, svc, "Mandiri Syariah")
		mustCreate(ctx, t, svc, "Bank Mandiri")

		_, err := svc.ResolveByName(ctx, "mandiri")
		require.True(t, wallet.IsAmbiguousNameError(err), "got %T: %v", err, err)
		var amb *wallet.AmbiguousNameError
		require.ErrorAs(t, err, &amb)
		assert.Equal(t, "mandiri", amb.Query)
		assert.Equal(t, []string{"Bank Mandiri", "Mandiri Syariah"}, amb.Candidates)
	})

	t.Run("archived wallets are excluded", func(t *testing.T) {
		svc, ctx := setup(t)
		w := mustCreate(ctx, t, svc, "Jenius")
		_, err := svc.Archive(ctx, w.ID)
		require.NoError(t, err)

		_, err = svc.ResolveByName(ctx, "jenius")
		assert.True(t, wallet.IsNotFoundError(err), "got %T: %v", err, err)
	})

	t.Run("no match is not found", func(t *testing.T) {
		svc, ctx := setup(t)
		mustCreate(ctx, t, svc, "BCA")

		_, err := svc.ResolveByName(ctx, "gopay")
		assert.True(t, wallet.IsNotFoundError(err), "got %T: %v", err, err)
	})

	t.Run("a blank query is not found", func(t *testing.T) {
		svc, ctx := setup(t)
		mustCreate(ctx, t, svc, "BCA")

		_, err := svc.ResolveByName(ctx, "   ")
		assert.True(t, wallet.IsNotFoundError(err), "got %T: %v", err, err)
	})
}
