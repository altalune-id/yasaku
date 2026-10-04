package category_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	apperrorv1 "altalune.id/yasaku/gen/go/apperror/v1"
	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/category"
	"altalune.id/yasaku/internal/testutil/fakes"
)

func newMirroredSvc(t *testing.T) (*category.Service, *fakes.Mirror[category.MirrorRef]) {
	t.Helper()
	m := &fakes.Mirror[category.MirrorRef]{}
	unexpected := func(_ context.Context, _ string, err error, _ ...any) *apperror.AppError {
		return apperror.New("yasaku.unexpected", err.Error(), codes.Internal, &apperrorv1.ErrorDetail{Code: "yasaku.unexpected"}).WithCause(err)
	}
	return category.NewService(fakes.NewTxCategory(), slog.New(slog.NewTextHandler(io.Discard, nil)), unexpected, &stubNamer{},
		category.WithMirror(m, fakes.UnitOfWork)), m
}

func TestCategoryWrites_MarkInsideTheUnitOfWorkAndKickAfter(t *testing.T) {
	svc, m := newMirroredSvc(t)
	ctx, _ := svcCtx(t)
	c, err := svc.Create(ctx, "Food", category.KindExpense, "", "")
	require.NoError(t, err)
	ref := category.MirrorRef{Entity: category.MirrorCategory, ID: c.ID}
	require.Equal(t, []fakes.MirrorCall[category.MirrorRef]{{Refs: []category.MirrorRef{ref}, InTx: true}}, m.Marks())
	require.Equal(t, []fakes.MirrorCall[category.MirrorRef]{{Refs: []category.MirrorRef{ref}, InTx: false}}, m.Kicks())

	_, err = svc.Rename(ctx, c.ID, "Meals")
	require.NoError(t, err)
	require.True(t, m.Kicks()[1].Refs[0].Cascade, "a rename re-marks the transactions showing the name")
	_, err = svc.Update(ctx, c.ID, "utensils", "chart-1")
	require.NoError(t, err)
	require.False(t, m.Kicks()[2].Refs[0].Cascade)
	require.NoError(t, svc.Delete(ctx, c.ID))
	require.True(t, m.Kicks()[3].Refs[0].Deleted)
}

func TestSeedDefaults_MarksEachRowAndKicksOnce(t *testing.T) {
	svc, m := newMirroredSvc(t)
	ctx, _ := svcCtx(t)
	added, err := svc.SeedDefaults(ctx)
	require.NoError(t, err)
	require.Len(t, m.Marks(), added, "one unit of work per seeded row")
	require.Len(t, m.Kicks(), 1)
	require.Len(t, m.Kicks()[0].Refs, added)
}

func TestWithMirror_PanicsAtWiringWithoutAUnitOfWork(t *testing.T) {
	require.PanicsWithValue(t, "category: WithMirror needs a unit of work, or a mark would not commit with its write", func() {
		category.WithMirror(&fakes.Mirror[category.MirrorRef]{}, nil)
	})
	require.NotPanics(t, func() { category.WithMirror(nil, nil) })
}
