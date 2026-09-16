package report_test

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

	"altalune.id/yasaku/civil"
	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/report"
	"altalune.id/yasaku/internal/testutil/fakes"
	"altalune.id/yasaku/money"
)

type fakeSettings struct {
	currency money.Currency
	loc      *time.Location
	curErr   error
	locErr   error
}

func (f *fakeSettings) DefaultCurrency(_ context.Context, _, _ uuid.UUID) (money.Currency, error) {
	return f.currency, f.curErr
}

func (f *fakeSettings) Location(_ context.Context, _, _ uuid.UUID) (*time.Location, error) {
	return f.loc, f.locErr
}

type fixture struct {
	svc      *report.Service
	reader   *fakes.ReportReader
	settings *fakeSettings
	tc       tenant.Context
}

func (f fixture) ctx() context.Context { return tenant.Into(context.Background(), f.tc) }

func newFixture(t *testing.T) fixture {
	t.Helper()
	jakarta, err := time.LoadLocation("Asia/Jakarta")
	require.NoError(t, err)

	reader := fakes.NewReportReader()
	settings := &fakeSettings{currency: money.IDR, loc: jakarta}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	reporter := apperror.NewReporter(log, false)

	return fixture{
		svc:      report.NewService(reader, log, reporter.Unexpected, settings),
		reader:   reader,
		settings: settings,
		tc:       tenant.Context{OrgID: uuid.New(), ProjectID: uuid.New(), UserID: uuid.New()},
	}
}

func TestService_Summary_PassesTenantScopeCurrencyAndLocalStart(t *testing.T) {
	f := newFixture(t)
	periodID := uuid.New()
	start := civil.Date{Year: 2026, Month: time.September, Day: 1}
	f.reader.Ref = report.PeriodRef{ID: periodID, Name: "Sep 2026", Start: start}
	f.reader.Summaries = report.PeriodSummary{
		Period:   report.PeriodRef{ID: periodID, Name: "Sep 2026", Start: start},
		Currency: money.IDR,
		Income:   money.New(500, money.IDR),
		Expense:  money.New(200, money.IDR),
		Net:      money.New(300, money.IDR),
		TxCount:  4,
	}

	got, err := f.svc.Summary(f.ctx(), periodID)
	require.NoError(t, err)
	assert.Equal(t, 4, got.TxCount)
	assert.Equal(t, money.New(300, money.IDR), got.Net)

	call, ok := f.reader.Last("Summary")
	require.True(t, ok)
	assert.Equal(t, f.tc.OrgID, call.OrgID)
	assert.Equal(t, f.tc.ProjectID, call.ProjectID)
	assert.Equal(t, periodID, call.PeriodID)
	assert.Equal(t, money.IDR, call.Currency)

	// Local midnight in Asia/Jakarta is 17:00 UTC the day before.
	assert.True(t, call.StartUTC.Equal(start.In(f.settings.loc)), "startUTC = %s", call.StartUTC)
	assert.Equal(t, time.Date(2026, time.August, 31, 17, 0, 0, 0, time.UTC), call.StartUTC.UTC())
}

func TestService_Summary_FallsBackToUTCWhenSettingsHaveNoLocation(t *testing.T) {
	f := newFixture(t)
	f.settings.loc = nil
	periodID := uuid.New()
	start := civil.Date{Year: 2026, Month: time.September, Day: 1}
	f.reader.Ref = report.PeriodRef{ID: periodID, Start: start}

	_, err := f.svc.Summary(f.ctx(), periodID)
	require.NoError(t, err)

	call, ok := f.reader.Last("Summary")
	require.True(t, ok)
	assert.Equal(t, time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC), call.StartUTC.UTC())
}

func TestService_Summary_RequiresTenantScope(t *testing.T) {
	f := newFixture(t)
	_, err := f.svc.Summary(context.Background(), uuid.New())
	require.Error(t, err)
	assert.Empty(t, f.reader.Calls())
}

func TestService_Summary_WrapsReaderFailure(t *testing.T) {
	f := newFixture(t)
	f.reader.SummaryErr = errors.New("boom")
	f.reader.Ref = report.PeriodRef{Start: civil.Date{Year: 2026, Month: time.September, Day: 1}}

	_, err := f.svc.Summary(f.ctx(), uuid.New())
	require.Error(t, err)
	_, ok := apperror.AsAppError(err)
	assert.True(t, ok, "reader failures surface as an AppError")
}

func TestService_CategoryReports_PassScopeAndCurrency(t *testing.T) {
	f := newFixture(t)
	periodID := uuid.New()
	catID := uuid.New()
	f.reader.Spend = []report.CategorySlice{{CategoryID: &catID, Name: "Food", Amount: money.New(100, money.IDR)}}
	f.reader.Income = []report.CategorySlice{{CategoryID: &catID, Name: "Salary", Amount: money.New(900, money.IDR)}}

	spend, err := f.svc.SpendByCategory(f.ctx(), periodID)
	require.NoError(t, err)
	require.Len(t, spend, 1)

	income, err := f.svc.IncomeByCategory(f.ctx(), periodID)
	require.NoError(t, err)
	require.Len(t, income, 1)

	for _, method := range []string{"SpendByCategory", "IncomeByCategory"} {
		call, ok := f.reader.Last(method)
		require.True(t, ok, method)
		assert.Equal(t, f.tc.OrgID, call.OrgID, method)
		assert.Equal(t, f.tc.ProjectID, call.ProjectID, method)
		assert.Equal(t, periodID, call.PeriodID, method)
		assert.Equal(t, money.IDR, call.Currency, method)
	}
}

func TestService_Flows_PassesScopeAndCurrency(t *testing.T) {
	f := newFixture(t)
	periodID := uuid.New()
	_, err := f.svc.Flows(f.ctx(), periodID)
	require.NoError(t, err)

	call, ok := f.reader.Last("Flows")
	require.True(t, ok)
	assert.Equal(t, f.tc.ProjectID, call.ProjectID)
	assert.Equal(t, periodID, call.PeriodID)
	assert.Equal(t, money.IDR, call.Currency)
}

func TestService_Cashflow_PassesThePeriodIDsInOrder(t *testing.T) {
	f := newFixture(t)
	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	f.reader.Points = []report.CashflowPoint{{Income: money.New(1, money.IDR)}}

	points, err := f.svc.Cashflow(f.ctx(), ids)
	require.NoError(t, err)
	require.Len(t, points, 1)

	call, ok := f.reader.Last("Cashflow")
	require.True(t, ok)
	assert.Equal(t, ids, call.PeriodIDs)
	assert.Equal(t, money.IDR, call.Currency)
}

func TestService_WalletBalances_PassesScope(t *testing.T) {
	f := newFixture(t)
	f.reader.Balances = []report.WalletLine{{Name: "Cash", Closing: money.New(10, money.IDR)}}

	lines, err := f.svc.WalletBalances(f.ctx())
	require.NoError(t, err)
	require.Len(t, lines, 1)

	call, ok := f.reader.Last("WalletBalances")
	require.True(t, ok)
	assert.Equal(t, f.tc.OrgID, call.OrgID)
	assert.Equal(t, f.tc.ProjectID, call.ProjectID)
}

func TestService_SnapshotFor_ForwardsItsArgumentScopeToTheReader(t *testing.T) {
	f := newFixture(t)
	otherOrg, otherProject, periodID := uuid.New(), uuid.New(), uuid.New()
	start := civil.Date{Year: 2026, Month: time.September, Day: 1}
	f.reader.Ref = report.PeriodRef{ID: periodID, Start: start}
	f.reader.Summaries = report.PeriodSummary{
		Currency: money.IDR,
		Income:   money.New(700, money.IDR),
		Expense:  money.New(250, money.IDR),
		Net:      money.New(450, money.IDR),
		TxCount:  6,
		Wallets: []report.WalletLine{
			{WalletID: uuid.New(), Name: "Cash", Closing: money.New(1_000, money.IDR)},
		},
	}

	snap, err := f.svc.SnapshotFor(f.ctx(), otherOrg, otherProject, periodID)
	require.NoError(t, err)
	assert.Equal(t, money.IDR, snap.Currency)
	assert.Equal(t, 6, snap.TxCount)
	require.Len(t, snap.Wallets, 1)

	call, ok := f.reader.Last("Summary")
	require.True(t, ok)
	assert.Equal(t, otherOrg, call.OrgID)
	assert.Equal(t, otherProject, call.ProjectID)
}

func TestService_Summary_UsesTheSettingsCurrency(t *testing.T) {
	f := newFixture(t)
	f.settings.currency = "USD"
	f.reader.Ref = report.PeriodRef{Start: civil.Date{Year: 2026, Month: time.September, Day: 1}}

	_, err := f.svc.Summary(f.ctx(), uuid.New())
	require.NoError(t, err)

	call, ok := f.reader.Last("Summary")
	require.True(t, ok)
	assert.Equal(t, money.Currency("USD"), call.Currency)
}

func TestService_Summary_SurfacesSettingsFailure(t *testing.T) {
	f := newFixture(t)
	f.settings.curErr = errors.New("settings down")

	_, err := f.svc.Summary(f.ctx(), uuid.New())
	require.Error(t, err)
	_, ok := apperror.AsAppError(err)
	assert.True(t, ok)
}
