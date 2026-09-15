package boot

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/civil"
	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/category"
	"altalune.id/yasaku/internal/i18n"
	"altalune.id/yasaku/internal/period"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/report"
	"altalune.id/yasaku/internal/testutil/fakes"
	"altalune.id/yasaku/money"
)

type stubSettings struct{ loc *time.Location }

func (s stubSettings) Location(_ context.Context, _, _ uuid.UUID) (*time.Location, error) {
	return s.loc, nil
}

func (s stubSettings) StartDay(_ context.Context, _, _ uuid.UUID) (int, error) { return 1, nil }

type stubSnapshotter struct{}

func (stubSnapshotter) Snapshot(_ context.Context, _, _, _ uuid.UUID) (period.Snapshot, error) {
	return period.Snapshot{}, nil
}

type resolverFixture struct {
	adapter periodResolverAdapter
	tc      tenant.Context

	july   uuid.UUID
	august uuid.UUID
	sept   uuid.UUID
}

func (f resolverFixture) ctx() context.Context { return tenant.Into(context.Background(), f.tc) }

func newResolverFixture(t *testing.T) resolverFixture {
	t.Helper()
	store := fakes.NewPeriod()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	reporter := apperror.NewReporter(log, false)
	passthrough := func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }

	svc := period.NewService(store, log, reporter.Unexpected,
		stubSettings{loc: time.UTC}, stubSnapshotter{}, period.UnitOfWork(passthrough), time.Now)

	f := resolverFixture{
		adapter: periodResolverAdapter{periods: svc},
		tc:      tenant.Context{OrgID: uuid.New(), ProjectID: uuid.New(), UserID: uuid.New()},
	}
	f.july = seedFakePeriod(t, store, f.tc, "Jul 2026", "2026-07-01", "2026-07-31", period.StatusClosed)
	f.august = seedFakePeriod(t, store, f.tc, "Aug 2026", "2026-08-01", "2026-08-31", period.StatusClosed)
	f.sept = seedFakePeriod(t, store, f.tc, "Sep 2026", "2026-09-01", "", period.StatusOpen)
	return f
}

func seedFakePeriod(t *testing.T, store *fakes.Period, tc tenant.Context, name, start, end string, status period.Status) uuid.UUID {
	t.Helper()
	startDate, err := civil.ParseDate(start)
	require.NoError(t, err)
	p, err := period.New(tc.OrgID, tc.ProjectID, startDate, name)
	require.NoError(t, err)
	if end != "" {
		endDate, eErr := civil.ParseDate(end)
		require.NoError(t, eErr)
		p.EndDate = &endDate
	}
	p.Status = status
	require.NoError(t, store.Save(context.Background(), p))
	return p.ID
}

func TestPeriodResolverAdapter_Containing_ReportsLockedAndNeighbours(t *testing.T) {
	f := newResolverFixture(t)
	at := time.Date(2026, time.August, 15, 9, 0, 0, 0, time.UTC)

	info, ok, err := f.adapter.Containing(f.ctx(), f.tc.OrgID, f.tc.ProjectID, at)
	require.NoError(t, err)
	require.True(t, ok)

	assert.Equal(t, f.august, info.ID)
	assert.True(t, info.Locked, "a closed period is locked")
	require.NotNil(t, info.PrevID)
	assert.Equal(t, f.july, *info.PrevID)
	require.NotNil(t, info.NextID)
	assert.Equal(t, f.sept, *info.NextID)
}

func TestPeriodResolverAdapter_Containing_OpenPeriodIsNotLocked(t *testing.T) {
	f := newResolverFixture(t)
	at := time.Date(2026, time.September, 10, 9, 0, 0, 0, time.UTC)

	info, ok, err := f.adapter.Containing(f.ctx(), f.tc.OrgID, f.tc.ProjectID, at)
	require.NoError(t, err)
	require.True(t, ok)

	assert.Equal(t, f.sept, info.ID)
	assert.False(t, info.Locked)
	require.NotNil(t, info.PrevID)
	assert.Equal(t, f.august, *info.PrevID)
	assert.Nil(t, info.NextID, "the current period has no successor")
}

func TestPeriodResolverAdapter_Containing_ReportsAbsenceAsNotOk(t *testing.T) {
	f := newResolverFixture(t)
	at := time.Date(2026, time.May, 1, 9, 0, 0, 0, time.UTC)

	info, ok, err := f.adapter.Containing(f.ctx(), f.tc.OrgID, f.tc.ProjectID, at)
	require.NoError(t, err, "an instant before every period is not an error")
	assert.False(t, ok)
	assert.Equal(t, uuid.Nil, info.ID)
}

func TestPeriodResolverAdapter_ByID_ReportsLocked(t *testing.T) {
	f := newResolverFixture(t)

	info, err := f.adapter.ByID(f.ctx(), f.tc.OrgID, f.tc.ProjectID, f.july)
	require.NoError(t, err)
	assert.Equal(t, f.july, info.ID)
	assert.True(t, info.Locked)
	assert.Nil(t, info.PrevID)
	require.NotNil(t, info.NextID)
	assert.Equal(t, f.august, *info.NextID)
}

func TestPeriodResolverAdapter_ByID_RefusesAnotherProject(t *testing.T) {
	f := newResolverFixture(t)
	other := tenant.Context{OrgID: f.tc.OrgID, ProjectID: uuid.New(), UserID: f.tc.UserID}

	_, err := f.adapter.ByID(tenant.Into(context.Background(), other), other.OrgID, other.ProjectID, f.august)
	require.Error(t, err)
	assert.True(t, period.IsNotFoundError(err), "a sibling project's period is reported absent")
}

func TestNamerAdapter_ReportsNothingWithoutATranslator(t *testing.T) {
	got := namerAdapter{}.DefaultName(context.Background(), "category.default.food")
	assert.Empty(t, got, "an empty name lets category fall back to its title-cased key, not the raw message id")
}

func TestNamerAdapter_WithoutATranslatorCategoryNamesTheDefaultItself(t *testing.T) {
	store := fakes.NewTxCategory()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	reporter := apperror.NewReporter(log, false)
	svc := category.NewService(store, log, reporter.Unexpected, namerAdapter{})

	tc := tenant.Context{OrgID: uuid.New(), ProjectID: uuid.New(), UserID: uuid.New()}
	n, err := svc.SeedDefaults(tenant.Into(context.Background(), tc))
	require.NoError(t, err)
	require.Positive(t, n)

	got, err := svc.List(tenant.Into(context.Background(), tc), category.ListOpts{})
	require.NoError(t, err)
	for _, c := range got {
		assert.NotContains(t, c.Name, category.DefaultKeyPrefix,
			"a seeded category must never be named after its raw i18n message id")
	}
}

func TestSnapshotterAdapter_StampsComputedAt(t *testing.T) {
	reader := fakes.NewReportReader()
	reader.Ref = report.PeriodRef{ID: uuid.New()}
	reader.Summaries = report.PeriodSummary{
		Currency: money.IDR,
		Income:   money.New(700, money.IDR),
		Expense:  money.New(250, money.IDR),
		Net:      money.New(450, money.IDR),
		TxCount:  6,
		Wallets: []report.WalletLine{
			{WalletID: uuid.New(), Name: "Cash", Closing: money.New(1_000, money.IDR)},
		},
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	reporter := apperror.NewReporter(log, false)
	reports := report.NewService(reader, log, reporter.Unexpected,
		stubReportSettings{currency: money.IDR, loc: time.UTC})

	frozen := time.Date(2026, time.September, 15, 4, 30, 0, 0, time.UTC)
	adapter := snapshotterAdapter{reports: reports, now: func() time.Time { return frozen }}

	tc := tenant.Context{OrgID: uuid.New(), ProjectID: uuid.New(), UserID: uuid.New()}
	snap, err := adapter.Snapshot(tenant.Into(context.Background(), tc), tc.OrgID, tc.ProjectID, uuid.New())
	require.NoError(t, err)

	assert.False(t, snap.ComputedAt.IsZero(), "a persisted snapshot must carry when it was computed")
	assert.True(t, snap.ComputedAt.Equal(frozen), "ComputedAt = %s", snap.ComputedAt)
	assert.Equal(t, money.IDR, snap.Currency)
	assert.Equal(t, int64(700), snap.Income)
	assert.Equal(t, int64(250), snap.Expense)
	assert.Equal(t, int64(450), snap.Net)
	assert.Equal(t, 6, snap.TxCount)
	require.Len(t, snap.Wallets, 1)
	assert.Equal(t, int64(1_000), snap.Wallets[0].Closing)
	assert.Equal(t, "Cash", snap.Wallets[0].Name)
}

func TestSnapshotterAdapter_StampsComputedAtWithoutAnInjectedClock(t *testing.T) {
	reader := fakes.NewReportReader()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	reporter := apperror.NewReporter(log, false)
	reports := report.NewService(reader, log, reporter.Unexpected,
		stubReportSettings{currency: money.IDR, loc: time.UTC})
	adapter := snapshotterAdapter{reports: reports}

	tc := tenant.Context{OrgID: uuid.New(), ProjectID: uuid.New(), UserID: uuid.New()}
	snap, err := adapter.Snapshot(tenant.Into(context.Background(), tc), tc.OrgID, tc.ProjectID, uuid.New())
	require.NoError(t, err)
	assert.False(t, snap.ComputedAt.IsZero(), "the zero clock must not leave ComputedAt unset")
}

type stubReportSettings struct {
	currency money.Currency
	loc      *time.Location
}

func (s stubReportSettings) DefaultCurrency(_ context.Context, _, _ uuid.UUID) (money.Currency, error) {
	return s.currency, nil
}

func (s stubReportSettings) Location(_ context.Context, _, _ uuid.UUID) (*time.Location, error) {
	return s.loc, nil
}

func TestNamerAdapter_UsesTheTranslatorOnTheContext(t *testing.T) {
	bundle := i18n.NewEmbeddedBundle(i18n.EnUS)
	ctx := i18n.TranslatorInto(context.Background(), bundle.For(i18n.EnUS))

	got := namerAdapter{}.DefaultName(ctx, "category.default.food")
	assert.Equal(t, "Food & Drinks", got)
}
