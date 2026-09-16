package category_test

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
	"altalune.id/yasaku/internal/category"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/testutil/fakes"
)

type stubNamer struct {
	names map[string]string
	blank bool
	calls int
}

func (n *stubNamer) DefaultName(_ context.Context, key string) string {
	n.calls++
	if n.blank {
		return ""
	}
	if v, ok := n.names[key]; ok {
		return v
	}
	return strings.TrimPrefix(key, "category.default.")
}

func newSvc(t *testing.T, store category.Store, namer category.Namer) (*category.Service, *int) {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	calls := 0
	unexpected := func(_ context.Context, _ string, err error, _ ...any) *apperror.AppError {
		calls++
		return apperror.New("yasaku.unexpected", err.Error(), codes.Internal,
			&apperrorv1.ErrorDetail{Code: "yasaku.unexpected"}).WithCause(err)
	}
	if namer == nil {
		namer = &stubNamer{}
	}
	return category.NewService(store, log, unexpected, namer), &calls
}

func svcCtx(t *testing.T) (context.Context, tenant.Context) {
	t.Helper()
	tc := tenant.Context{OrgID: uuid.New(), ProjectID: uuid.New(), UserID: uuid.New()}
	return tenant.Into(t.Context(), tc), tc
}

func TestService_Create(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		svc, unex := newSvc(t, fakes.NewTxCategory(), nil)
		ctx, tc := svcCtx(t)

		got, err := svc.Create(ctx, "  Food & Drinks  ", category.KindExpense, "utensils", "chart-1")
		require.NoError(t, err)
		assert.Equal(t, "Food & Drinks", got.Name)
		assert.Equal(t, category.KindExpense, got.Kind)
		assert.Equal(t, tc.OrgID, got.OrgID)
		assert.Equal(t, tc.ProjectID, got.ProjectID)
		assert.Zero(t, *unex)
	})

	t.Run("sort order continues within the kind", func(t *testing.T) {
		svc, _ := newSvc(t, fakes.NewTxCategory(), nil)
		ctx, _ := svcCtx(t)

		a, err := svc.Create(ctx, "Food", category.KindExpense, "", "")
		require.NoError(t, err)
		b, err := svc.Create(ctx, "Transport", category.KindExpense, "", "")
		require.NoError(t, err)
		income, err := svc.Create(ctx, "Salary", category.KindIncome, "", "")
		require.NoError(t, err)

		assert.Less(t, a.SortOrder, b.SortOrder, "a later category in the same kind sorts after")
		assert.Equal(t, a.SortOrder, income.SortOrder, "sort order restarts per kind")
	})

	t.Run("invalid name bubbles the typed error", func(t *testing.T) {
		svc, unex := newSvc(t, fakes.NewTxCategory(), nil)
		ctx, _ := svcCtx(t)

		_, err := svc.Create(ctx, "  ", category.KindExpense, "", "")
		assert.True(t, category.IsInvalidNameError(err), "got %T: %v", err, err)
		assert.Zero(t, *unex)
	})

	t.Run("invalid kind bubbles the typed error", func(t *testing.T) {
		svc, unex := newSvc(t, fakes.NewTxCategory(), nil)
		ctx, _ := svcCtx(t)

		_, err := svc.Create(ctx, "Food", category.Kind("transfer"), "", "")
		assert.True(t, category.IsInvalidKindError(err), "got %T: %v", err, err)
		assert.Zero(t, *unex)
	})

	t.Run("a duplicate active name in the same kind bubbles AlreadyExistsError", func(t *testing.T) {
		svc, unex := newSvc(t, fakes.NewTxCategory(), nil)
		ctx, _ := svcCtx(t)

		_, err := svc.Create(ctx, "Food", category.KindExpense, "", "")
		require.NoError(t, err)
		_, err = svc.Create(ctx, "  food  ", category.KindExpense, "", "")
		assert.True(t, category.IsAlreadyExistsError(err), "got %T: %v", err, err)
		assert.Zero(t, *unex)
	})

	t.Run("the same name under the other kind is allowed", func(t *testing.T) {
		svc, _ := newSvc(t, fakes.NewTxCategory(), nil)
		ctx, _ := svcCtx(t)

		_, err := svc.Create(ctx, "Other", category.KindExpense, "", "")
		require.NoError(t, err)
		_, err = svc.Create(ctx, "Other", category.KindIncome, "", "")
		require.NoError(t, err, "the unique index is (project_id, kind, lower(name))")
	})

	t.Run("a store failure is reported as unexpected", func(t *testing.T) {
		store := fakes.NewTxCategory()
		store.SaveFn = func(context.Context, *category.Category) error { return errors.New("boom") }
		svc, unex := newSvc(t, store, nil)
		ctx, _ := svcCtx(t)

		_, err := svc.Create(ctx, "Food", category.KindExpense, "", "")
		require.Error(t, err)
		assert.Equal(t, 1, *unex)
	})

	t.Run("missing tenant scope is refused", func(t *testing.T) {
		svc, unex := newSvc(t, fakes.NewTxCategory(), nil)
		_, err := svc.Create(context.Background(), "Food", category.KindExpense, "", "")
		require.Error(t, err)
		assert.Zero(t, *unex)
	})
}

// TestService_ByID_RefusesASiblingProject uses a deliberately non-filtering fake: the store
// hands back any row by id, so the service's own scope check is the only thing standing
// between the caller and another project's row inside the same org.
func TestService_ByID_RefusesASiblingProject(t *testing.T) {
	store := fakes.NewTxCategory()
	svc, unex := newSvc(t, store, nil)
	ctx, tc := svcCtx(t)

	sibling, err := category.New(tc.OrgID, uuid.New(), "Sibling", category.KindExpense, "", "", 0)
	require.NoError(t, err)
	require.NoError(t, store.Save(ctx, sibling))

	raw, err := store.ByID(ctx, sibling.ID)
	require.NoError(t, err, "the fake must not filter, or this test proves nothing")
	require.Equal(t, sibling.ID, raw.ID)

	_, err = svc.ByID(ctx, sibling.ID)
	assert.True(t, category.IsNotFoundError(err), "got %T: %v", err, err)
	assert.Zero(t, *unex)

	otherOrg, err := category.New(uuid.New(), uuid.New(), "Other Org", category.KindExpense, "", "", 0)
	require.NoError(t, err)
	require.NoError(t, store.Save(ctx, otherOrg))
	_, err = svc.ByID(ctx, otherOrg.ID)
	assert.True(t, category.IsNotFoundError(err), "another org's row must read as absent too")
}

func TestService_MutatorsRefuseASiblingProject(t *testing.T) {
	seed := func(t *testing.T) (*category.Service, *fakes.TxCategory, context.Context, *category.Category) {
		t.Helper()
		store := fakes.NewTxCategory()
		svc, _ := newSvc(t, store, nil)
		ctx, tc := svcCtx(t)
		sibling, err := category.New(tc.OrgID, uuid.New(), "Sibling", category.KindExpense, "", "", 0)
		require.NoError(t, err)
		require.NoError(t, store.Save(ctx, sibling))
		return svc, store, ctx, sibling
	}

	t.Run("Rename", func(t *testing.T) {
		svc, _, ctx, sibling := seed(t)
		_, err := svc.Rename(ctx, sibling.ID, "Renamed")
		assert.True(t, category.IsNotFoundError(err), "got %T: %v", err, err)
	})
	t.Run("Update", func(t *testing.T) {
		svc, _, ctx, sibling := seed(t)
		_, err := svc.Update(ctx, sibling.ID, "bus", "chart-2")
		assert.True(t, category.IsNotFoundError(err), "got %T: %v", err, err)
	})
	t.Run("Archive", func(t *testing.T) {
		svc, _, ctx, sibling := seed(t)
		_, err := svc.Archive(ctx, sibling.ID)
		assert.True(t, category.IsNotFoundError(err), "got %T: %v", err, err)
	})
	t.Run("Unarchive", func(t *testing.T) {
		svc, _, ctx, sibling := seed(t)
		_, err := svc.Unarchive(ctx, sibling.ID)
		assert.True(t, category.IsNotFoundError(err), "got %T: %v", err, err)
	})
	t.Run("Delete leaves the row in place", func(t *testing.T) {
		svc, store, ctx, sibling := seed(t)
		assert.True(t, category.IsNotFoundError(svc.Delete(ctx, sibling.ID)))
		_, err := store.ByID(ctx, sibling.ID)
		require.NoError(t, err, "a refused delete must not touch the row")
	})
}

func TestService_ArchiveUnarchive(t *testing.T) {
	svc, _ := newSvc(t, fakes.NewTxCategory(), nil)
	ctx, _ := svcCtx(t)

	c, err := svc.Create(ctx, "Food", category.KindExpense, "", "")
	require.NoError(t, err)

	archived, err := svc.Archive(ctx, c.ID)
	require.NoError(t, err)
	assert.True(t, archived.IsArchived())

	active, err := svc.List(ctx, category.ListOpts{})
	require.NoError(t, err)
	assert.Empty(t, active, "an archived category is out of the default list")

	all, err := svc.List(ctx, category.ListOpts{IncludeArchived: true})
	require.NoError(t, err)
	assert.Len(t, all, 1)

	back, err := svc.Unarchive(ctx, c.ID)
	require.NoError(t, err)
	assert.False(t, back.IsArchived())
}

func TestService_List_FiltersByKind(t *testing.T) {
	svc, _ := newSvc(t, fakes.NewTxCategory(), nil)
	ctx, _ := svcCtx(t)

	_, err := svc.Create(ctx, "Food", category.KindExpense, "", "")
	require.NoError(t, err)
	_, err = svc.Create(ctx, "Salary", category.KindIncome, "", "")
	require.NoError(t, err)

	expense, err := svc.List(ctx, category.ListOpts{Kind: category.KindExpense})
	require.NoError(t, err)
	require.Len(t, expense, 1)
	assert.Equal(t, "Food", expense[0].Name)

	all, err := svc.List(ctx, category.ListOpts{})
	require.NoError(t, err)
	assert.Len(t, all, 2)
}

func TestService_ResolveByName(t *testing.T) {
	seed := func(t *testing.T) (*category.Service, context.Context) {
		t.Helper()
		svc, _ := newSvc(t, fakes.NewTxCategory(), nil)
		ctx, _ := svcCtx(t)
		for _, n := range []string{"Food & Drinks", "Transport", "Transport Fees"} {
			_, err := svc.Create(ctx, n, category.KindExpense, "", "")
			require.NoError(t, err)
		}
		_, err := svc.Create(ctx, "Transport", category.KindIncome, "", "")
		require.NoError(t, err)
		return svc, ctx
	}

	t.Run("exact match is case insensitive", func(t *testing.T) {
		svc, ctx := seed(t)
		got, err := svc.ResolveByName(ctx, category.KindExpense, "  food & drinks ")
		require.NoError(t, err)
		assert.Equal(t, "Food & Drinks", got.Name)
	})

	t.Run("an exact match wins over a longer partial", func(t *testing.T) {
		svc, ctx := seed(t)
		got, err := svc.ResolveByName(ctx, category.KindExpense, "Transport")
		require.NoError(t, err)
		assert.Equal(t, "Transport", got.Name)
		assert.Equal(t, category.KindExpense, got.Kind)
	})

	t.Run("kind scopes the lookup", func(t *testing.T) {
		svc, ctx := seed(t)
		got, err := svc.ResolveByName(ctx, category.KindIncome, "transport")
		require.NoError(t, err)
		assert.Equal(t, category.KindIncome, got.Kind)
	})

	t.Run("a unique partial match resolves", func(t *testing.T) {
		svc, ctx := seed(t)
		got, err := svc.ResolveByName(ctx, category.KindExpense, "drinks")
		require.NoError(t, err)
		assert.Equal(t, "Food & Drinks", got.Name)
	})

	t.Run("an ambiguous partial is refused", func(t *testing.T) {
		svc, ctx := seed(t)
		_, err := svc.ResolveByName(ctx, category.KindExpense, "transpo")
		assert.True(t, category.IsAmbiguousNameError(err), "got %T: %v", err, err)
	})

	t.Run("no match is NotFoundError", func(t *testing.T) {
		svc, ctx := seed(t)
		_, err := svc.ResolveByName(ctx, category.KindExpense, "zzz")
		assert.True(t, category.IsNotFoundError(err), "got %T: %v", err, err)
	})

	t.Run("a blank query is an invalid name", func(t *testing.T) {
		svc, ctx := seed(t)
		_, err := svc.ResolveByName(ctx, category.KindExpense, "   ")
		assert.True(t, category.IsInvalidNameError(err), "got %T: %v", err, err)
	})

	t.Run("an invalid kind is refused", func(t *testing.T) {
		svc, ctx := seed(t)
		_, err := svc.ResolveByName(ctx, category.Kind("transfer"), "Food")
		assert.True(t, category.IsInvalidKindError(err), "got %T: %v", err, err)
	})

	t.Run("archived categories are not resolvable", func(t *testing.T) {
		svc, ctx := seed(t)
		hit, err := svc.ResolveByName(ctx, category.KindExpense, "Transport Fees")
		require.NoError(t, err)
		_, err = svc.Archive(ctx, hit.ID)
		require.NoError(t, err)

		got, err := svc.ResolveByName(ctx, category.KindExpense, "transpo")
		require.NoError(t, err, "with the archived sibling gone the partial is unique again")
		assert.Equal(t, "Transport", got.Name)
	})
}

func TestService_SeedDefaults(t *testing.T) {
	t.Run("seeds every default once and is idempotent", func(t *testing.T) {
		namer := &stubNamer{}
		svc, unex := newSvc(t, fakes.NewTxCategory(), namer)
		ctx, tc := svcCtx(t)

		n, err := svc.SeedDefaults(ctx)
		require.NoError(t, err)
		assert.Equal(t, len(category.Defaults), n)
		assert.Equal(t, 20, n)
		assert.Zero(t, *unex)

		rows, err := svc.List(ctx, category.ListOpts{})
		require.NoError(t, err)
		require.Len(t, rows, 20)
		for _, r := range rows {
			assert.Equal(t, tc.OrgID, r.OrgID)
			assert.Equal(t, tc.ProjectID, r.ProjectID)
		}

		again, err := svc.SeedDefaults(ctx)
		require.NoError(t, err)
		assert.Zero(t, again, "a second call must insert nothing")

		rows, err = svc.List(ctx, category.ListOpts{IncludeArchived: true})
		require.NoError(t, err)
		assert.Len(t, rows, 20)
	})

	// An archived default must neither be resurrected nor duplicated: idempotency is by
	// lower(name) per kind across ALL rows, archived included.
	t.Run("an archived default is neither duplicated nor resurrected", func(t *testing.T) {
		svc, _ := newSvc(t, fakes.NewTxCategory(), &stubNamer{})
		ctx, _ := svcCtx(t)

		n, err := svc.SeedDefaults(ctx)
		require.NoError(t, err)
		require.Equal(t, 20, n)

		rows, err := svc.List(ctx, category.ListOpts{})
		require.NoError(t, err)
		victim := rows[0]
		_, err = svc.Archive(ctx, victim.ID)
		require.NoError(t, err)

		third, err := svc.SeedDefaults(ctx)
		require.NoError(t, err)
		assert.Zero(t, third, "an archived default must not be re-inserted")

		all, err := svc.List(ctx, category.ListOpts{IncludeArchived: true})
		require.NoError(t, err)
		assert.Len(t, all, 20, "no duplicate row for the archived default")

		reread, err := svc.ByID(ctx, victim.ID)
		require.NoError(t, err)
		assert.True(t, reread.IsArchived(), "seeding must not resurrect an archived default")
	})

	// Sort order is assigned PER KIND, the same scheme Create uses. A global 0..19 would put
	// the income defaults at 13..19 and collide with the first new expense category.
	t.Run("sort order is per kind and appearance comes from the Defaults table", func(t *testing.T) {
		svc, _ := newSvc(t, fakes.NewTxCategory(), &stubNamer{})
		ctx, _ := svcCtx(t)

		_, err := svc.SeedDefaults(ctx)
		require.NoError(t, err)

		rows, err := svc.List(ctx, category.ListOpts{})
		require.NoError(t, err)
		require.Len(t, rows, len(category.Defaults))

		bySort := map[category.Kind]map[int]*category.Category{}
		for _, r := range rows {
			if bySort[r.Kind] == nil {
				bySort[r.Kind] = map[int]*category.Category{}
			}
			assert.NotContains(t, bySort[r.Kind], r.SortOrder,
				"two %s categories share sort order %d", r.Kind, r.SortOrder)
			bySort[r.Kind][r.SortOrder] = r
		}

		seen := map[category.Kind]int{}
		for _, d := range category.Defaults {
			idx := seen[d.Kind]
			seen[d.Kind]++
			row, ok := bySort[d.Kind][idx]
			require.True(t, ok, "no %s category at sort order %d (%s)", d.Kind, idx, d.Key)
			assert.Equal(t, d.Icon, row.Icon)
			assert.Equal(t, d.Color, row.Color)
		}
		assert.Equal(t, 13, seen[category.KindExpense])
		assert.Equal(t, 7, seen[category.KindIncome])
	})

	t.Run("a category created after seeding continues its kind rather than interleaving", func(t *testing.T) {
		svc, _ := newSvc(t, fakes.NewTxCategory(), &stubNamer{})
		ctx, _ := svcCtx(t)

		_, err := svc.SeedDefaults(ctx)
		require.NoError(t, err)

		mine, err := svc.Create(ctx, "Pets", category.KindExpense, "", "")
		require.NoError(t, err)
		assert.Equal(t, 13, mine.SortOrder, "the 14th expense category sorts after the 13 defaults")

		expense, err := svc.List(ctx, category.ListOpts{Kind: category.KindExpense})
		require.NoError(t, err)
		require.Len(t, expense, 14)
		assert.Equal(t, "Pets", expense[13].Name, "a new category lands last, not in the middle")
	})

	t.Run("a concurrent seeder's AlreadyExistsError is skipped, not reported as unexpected", func(t *testing.T) {
		store := fakes.NewTxCategory()
		svc, unex := newSvc(t, store, &stubNamer{})
		ctx, _ := svcCtx(t)

		// The hook nils itself for the duration of the passthrough, or store.Save would
		// re-enter it. SeedDefaults is single-goroutine, so the swap is safe.
		var hook func(context.Context, *category.Category) error
		hook = func(sctx context.Context, c *category.Category) error {
			if category.FoldName(c.Name) == "food" {
				return &category.AlreadyExistsError{Name: c.Name, Kind: c.Kind}
			}
			store.SaveFn = nil
			defer func() { store.SaveFn = hook }()
			return store.Save(sctx, c)
		}
		store.SaveFn = hook

		n, err := svc.SeedDefaults(ctx)
		require.NoError(t, err, "a racing seeder is the idempotent outcome, not an internal error")
		assert.Equal(t, 19, n, "the contended default is skipped, the other 19 still land")
		assert.Zero(t, *unex)

		rows, err := svc.List(ctx, category.ListOpts{})
		require.NoError(t, err)
		assert.Len(t, rows, 19, "no partial set: seeding continued past the contended row")
	})

	t.Run("a blank namer falls back to the title-cased key", func(t *testing.T) {
		svc, _ := newSvc(t, fakes.NewTxCategory(), &stubNamer{blank: true})
		ctx, _ := svcCtx(t)

		n, err := svc.SeedDefaults(ctx)
		require.NoError(t, err)
		require.Equal(t, 20, n)

		rows, err := svc.List(ctx, category.ListOpts{})
		require.NoError(t, err)
		names := map[string]bool{}
		for _, r := range rows {
			names[r.Name] = true
		}
		assert.True(t, names["Phone Internet"], "got %v", names)
		assert.True(t, names["Other Expense"])
		assert.True(t, names["Other Income"])
		assert.True(t, names["Food"])
	})

	t.Run("the namer is asked for the category.default.<key> message id", func(t *testing.T) {
		namer := &stubNamer{names: map[string]string{"category.default.food": "Makan & Minum"}}
		svc, _ := newSvc(t, fakes.NewTxCategory(), namer)
		ctx, _ := svcCtx(t)

		_, err := svc.SeedDefaults(ctx)
		require.NoError(t, err)
		assert.Equal(t, len(category.Defaults), namer.calls)

		got, err := svc.ResolveByName(ctx, category.KindExpense, "Makan & Minum")
		require.NoError(t, err)
		assert.Equal(t, "Makan & Minum", got.Name)
	})

	t.Run("a pre-existing hand-made category with a default's name is left alone", func(t *testing.T) {
		svc, _ := newSvc(t, fakes.NewTxCategory(), &stubNamer{})
		ctx, _ := svcCtx(t)

		mine, err := svc.Create(ctx, "  FOOD  ", category.KindExpense, "wallet", "chart-5")
		require.NoError(t, err)

		n, err := svc.SeedDefaults(ctx)
		require.NoError(t, err)
		assert.Equal(t, 19, n, "the colliding default is skipped by lower(name)")

		reread, err := svc.ByID(ctx, mine.ID)
		require.NoError(t, err)
		assert.Equal(t, "FOOD", reread.Name)
		assert.Equal(t, "wallet", reread.Icon)
	})

	t.Run("a store failure is reported as unexpected", func(t *testing.T) {
		store := fakes.NewTxCategory()
		store.ListFn = func(context.Context, uuid.UUID, uuid.UUID, category.ListOpts) ([]*category.Category, error) {
			return nil, errors.New("boom")
		}
		svc, unex := newSvc(t, store, &stubNamer{})
		ctx, _ := svcCtx(t)

		_, err := svc.SeedDefaults(ctx)
		require.Error(t, err)
		assert.Equal(t, 1, *unex)
	})

	t.Run("missing tenant scope is refused", func(t *testing.T) {
		svc, unex := newSvc(t, fakes.NewTxCategory(), &stubNamer{})
		_, err := svc.SeedDefaults(context.Background())
		require.Error(t, err)
		assert.Zero(t, *unex)
	})
}

func TestService_Rename_DuplicateBubblesAlreadyExists(t *testing.T) {
	svc, unex := newSvc(t, fakes.NewTxCategory(), nil)
	ctx, _ := svcCtx(t)

	_, err := svc.Create(ctx, "Food", category.KindExpense, "", "")
	require.NoError(t, err)
	other, err := svc.Create(ctx, "Transport", category.KindExpense, "", "")
	require.NoError(t, err)

	_, err = svc.Rename(ctx, other.ID, "food")
	assert.True(t, category.IsAlreadyExistsError(err), "got %T: %v", err, err)
	assert.Zero(t, *unex)
}

func TestService_Delete_InUseBubbles(t *testing.T) {
	store := fakes.NewTxCategory()
	svc, unex := newSvc(t, store, nil)
	ctx, _ := svcCtx(t)

	c, err := svc.Create(ctx, "Food", category.KindExpense, "", "")
	require.NoError(t, err)

	store.DeleteFn = func(_ context.Context, id uuid.UUID) error { return &category.InUseError{ID: id.String()} }
	err = svc.Delete(ctx, c.ID)
	assert.True(t, category.IsInUseError(err), "got %T: %v", err, err)
	assert.Zero(t, *unex)
}

func TestErrorsCarryTheirCodes(t *testing.T) {
	tests := []struct {
		err  error
		code string
	}{
		{&category.NotFoundError{ID: uuid.NewString()}, apperror.CodeTxCategoryNotFound},
		{&category.InvalidNameError{Reason: "empty"}, apperror.CodeTxCategoryInvalidName},
		{&category.InvalidIconError{Icon: "skull"}, apperror.CodeTxCategoryInvalidName},
		{&category.InvalidColorError{Color: "puce"}, apperror.CodeTxCategoryInvalidName},
		{&category.AlreadyExistsError{Name: "Food", Kind: category.KindExpense}, apperror.CodeTxCategoryAlreadyExists},
		{&category.InvalidKindError{Value: "transfer"}, apperror.CodeTxCategoryInvalidKind},
		{&category.InUseError{ID: uuid.NewString()}, apperror.CodeTxCategoryInUse},
		{&category.AmbiguousNameError{Name: "trans", Matches: 2}, apperror.CodeTxCategoryAmbiguousName},
	}
	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			assert.NotEmpty(t, tt.err.Error())
			ae, ok := apperror.AsAppError(tt.err)
			require.True(t, ok, "%T does not convert to an AppError", tt.err)
			assert.Equal(t, tt.code, ae.Code())
		})
	}
}
