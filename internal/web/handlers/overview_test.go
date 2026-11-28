package handlers_test

import (
	"context"
	"errors"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/category"
	"altalune.id/yasaku/internal/ledger"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/period"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/report"
	"altalune.id/yasaku/internal/testutil/fakes"
	"altalune.id/yasaku/internal/transaction"
	"altalune.id/yasaku/money"
)

func (f *txFixture) home(t *testing.T, sub string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	f.overviewHandler().Register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, f.request(t, http.MethodGet, f.path(sub), nil, false))
	return rec
}

func (f *txFixture) record(t *testing.T, kind transaction.Kind, minor int64, cat *uuid.UUID, note string) {
	t.Helper()
	_, err := f.Transactions.Record(f.projectCtx(t), transaction.RecordInput{
		WalletID: f.Cash, Kind: kind, Amount: money.New(minor, money.IDR),
		CategoryID: cat, Note: note, OccurredAt: time.Now(),
	})
	require.NoError(t, err)
}

func (f *txFixture) withClock(at time.Time) *period.Service {
	store := fakes.NewPeriod()
	uow := period.UnitOfWork(txPassthroughUoW)
	f.Periods = period.NewService(store, discardLogger(), passthroughUnexpected(), f.Ledgers, f.Snapshots, f.Snapshots, uow, time.Now)
	f.Transactions = transaction.NewService(fakes.NewTransaction(), discardLogger(), passthroughUnexpected(),
		txWalletReader(f.Wallets), txCategoryReader(f.TxCategories),
		txPeriodResolver{periods: f.Periods}, transaction.UnitOfWork(txPassthroughUoW))
	return period.NewService(store, discardLogger(), passthroughUnexpected(), f.Ledgers, f.Snapshots, f.Snapshots, uow, func() time.Time { return at })
}

var dayTotalRe = regexp.MustCompile(`data-day-total[^>]*>([^<]+)<`)

func dayTotals(body string) []string {
	var out []string
	for _, m := range dayTotalRe.FindAllStringSubmatch(body, -1) {
		out = append(out, m[1])
	}
	return out
}

func TestHome_RendersPeriodStatsAndDayGroupsWithoutTheInlineForm(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	_, err := f.Periods.EnsureCurrent(f.projectCtx(t))
	require.NoError(t, err)
	f.record(t, transaction.KindExpense, 4000000, &f.Food, "Kopi pagi")
	f.record(t, transaction.KindExpense, 1000000, &f.Food, "Roti")
	f.ReportReader.Summaries = report.PeriodSummary{
		Currency: money.IDR,
		Income:   money.New(90000000, money.IDR),
		Expense:  money.New(5000000, money.IDR),
		Net:      money.New(85000000, money.IDR),
	}

	rec := f.home(t, "/overview")

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, `class="stats`)
	assert.Contains(t, body, "Rp\u00a0900.000")
	assert.Contains(t, body, "Rp\u00a0850.000")
	assert.Contains(t, body, "Kopi pagi")
	assert.Contains(t, body, "Roti")
	assert.Equal(t, 1, strings.Count(body, "data-day-group"), "two entries on one day make one group")
	assert.Equal(t, []string{"−Rp\u00a050.000"}, dayTotals(body), "the day total sums both expenses")
	assert.NotContains(t, body, `id="tx-quick-add"`, "Home renders no add form; the sheet loads on open")
}

func TestHome_QuickAddLinksTopExpenseCategoriesToTheAddSheet(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	ctx := f.projectCtx(t)
	_, err := f.Periods.EnsureCurrent(ctx)
	require.NoError(t, err)
	salary, err := f.TxCategories.Create(ctx, "Gaji", category.KindIncome, "banknote", "chart-2")
	require.NoError(t, err)
	f.record(t, transaction.KindExpense, 4000000, &f.Food, "Kopi")
	f.record(t, transaction.KindIncome, 90000000, &salary.ID, "")

	body := f.home(t, "/overview").Body.String()

	assert.Contains(t, body, `href="`+f.path("/transactions/new?kind=expense&amp;category="+f.Food.String())+`"`)
	assert.NotContains(t, body, "category="+salary.ID.String(), "Quick add offers expense categories only")
}

func TestHome_EmptyPeriodOffersTheFirstExpense(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	_, err := f.Periods.EnsureCurrent(f.projectCtx(t))
	require.NoError(t, err)

	body := f.home(t, "/overview").Body.String()

	assert.Contains(t, body, "data-home-empty")
	assert.Regexp(t, `data-dialog-open="tx-add-dialog" data-dialog-src="[^"]*/transactions/new\?kind=expense`, body, "every opener can load the sheet itself, so + never navigates with JS on")
	assert.Contains(t, body, `href="`+f.path("/transactions/new?kind=expense")+`"`)
	assert.NotContains(t, body, "data-day-group")
}

func TestHome_EmptyCurrentPeriodPulsesTheAddButton(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	_, err := f.Periods.EnsureCurrent(f.projectCtx(t))
	require.NoError(t, err)

	assert.Contains(t, f.home(t, "/overview").Body.String(), `data-tip-pulse="add-first"`)

	f.record(t, transaction.KindExpense, 2500000, nil, "kopi")
	assert.NotContains(t, f.home(t, "/overview").Body.String(), `data-tip-pulse="add-first"`, "the pulse stops once the first entry exists")
}

func TestHome_NewLedgerWithoutAPeriodStillPulsesTheAddButton(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)

	assert.Contains(t, f.home(t, "/overview").Body.String(), `data-tip-pulse="add-first"`, "the first period starts with the first entry, so the add tip must not wait for it")
}

var addOpenerRE = regexp.MustCompile(`<[a-z]+\b[^>]*data-dialog-open="tx-add-dialog"[^>]*>`)
var dialogSrcRE = regexp.MustCompile(`data-dialog-src="([^"]+)"`)

func TestHome_LoadsTheAddSheetLazilyAndHostsRowActions(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	f.record(t, transaction.KindExpense, 2500000, &f.Food, "kopi")

	body := f.home(t, "/overview").Body.String()

	assert.NotContains(t, body, `id="tx-add-dialog"`, "Home builds no add form; the openers fetch it")
	assert.Contains(t, body, `id="tx-row-actions"`)
	assert.Contains(t, body, "swipe.js")
	openers := addOpenerRE.FindAllString(body, -1)
	require.NotEmpty(t, openers)
	for _, o := range openers {
		m := dialogSrcRE.FindStringSubmatch(o)
		require.NotNil(t, m, "an opener without data-dialog-src cannot open the sheet: %s", o)
		rec := f.serveTx(t, f.request(t, http.MethodGet, html.UnescapeString(m[1]), nil, true))
		require.Equal(t, http.StatusOK, rec.Code, m[1])
		assert.Contains(t, rec.Body.String(), `id="tx-add-dialog"`, "the lazy GET returns the sheet: %s", m[1])
	}
}

func TestHome_ShellRendersNoRawLocaleKeys(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)

	body := f.home(t, "/overview").Body.String()

	assert.NotRegexp(t, `>\s*(nav|more|overview|tx|wallet)\.[a-z_.]+\s*<`, body, "every label in the shell and Home resolves to a translation")
}

func TestHome_NoActiveWalletOffersCreateAWallet(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	ctx := f.projectCtx(t)
	for _, id := range []uuid.UUID{f.Cash, f.Bank} {
		_, err := f.Wallets.Archive(ctx, id)
		require.NoError(t, err)
	}

	rec := f.home(t, "/overview")

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "data-home-no-wallet")
	assert.Contains(t, body, `href="`+f.path("/wallets/new")+`"`)
	assert.NotContains(t, body, `class="stats`)
}

func TestHome_NewProjectRedirectsAManagerToFirstRun(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	octx := fakes.Owner(context.Background(), tenant.Context{OrgID: f.OrgID, UserID: f.Principal.UserID})
	fresh, err := f.Projects.Create(octx, f.OrgID, "fresh", "Fresh")
	require.NoError(t, err)
	f.ProjSlug = fresh.Slug

	rec := f.home(t, "/overview")

	require.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, f.path("/first-run"), rec.Header().Get("Location"))
}

func TestHome_ConfiguredProjectWithoutWalletsSendsAManagerToFirstRun(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	octx := fakes.Owner(context.Background(), tenant.Context{OrgID: f.OrgID, UserID: f.Principal.UserID})
	fresh, err := f.Projects.Create(octx, f.OrgID, "fresh", "Fresh")
	require.NoError(t, err)
	cur := money.IDR
	_, err = f.Ledgers.Update(f.scoped(f.OrgID, fresh.ID, f.Principal.UserID), ledger.Patch{Currency: &cur})
	require.NoError(t, err)
	f.ProjSlug = fresh.Slug

	rec := f.home(t, "/overview")

	require.Equal(t, http.StatusSeeOther, rec.Code, "saved ledger settings alone don't finish first run: a wallet does")
	assert.Equal(t, f.path("/first-run"), rec.Header().Get("Location"))
}

func TestHome_ClosedPeriodShowsItsStoredTotals(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	past := f.withClock(time.Now().AddDate(0, 0, -45))
	ctx := f.projectCtx(t)
	old, err := past.EnsureCurrent(ctx)
	require.NoError(t, err)
	end, err := f.Periods.SuggestedEnd(ctx, old.ID)
	require.NoError(t, err)
	_, err = f.Periods.Close(ctx, old.ID, end, f.Principal.UserID)
	require.NoError(t, err)
	f.ReportReader.Summaries = report.PeriodSummary{Currency: money.IDR, Income: money.New(1000000, money.IDR), Expense: money.Zero(money.IDR), Net: money.New(1000000, money.IDR)}

	body := f.home(t, "/overview?period="+old.ID.String()).Body.String()

	assert.NotContains(t, body, "Rp\u00a010.000", "a closed period reads the totals stored when it closed (zero here), like Periods and the API, not today's live sums")
}

func TestHome_ShowsBalanceFixesOnlyWhenThereAreAny(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	_, err := f.Periods.EnsureCurrent(f.projectCtx(t))
	require.NoError(t, err)
	f.record(t, transaction.KindExpense, 2500000, nil, "kopi")
	assert.NotContains(t, f.home(t, "/overview").Body.String(), "data-balance-fixes")

	f.ReportReader.Summaries = report.PeriodSummary{Currency: money.IDR, Income: money.Zero(money.IDR), Expense: money.Zero(money.IDR), Net: money.Zero(money.IDR), Adjustments: money.New(-5000000, money.IDR)}
	body := f.home(t, "/overview").Body.String()
	assert.Contains(t, body, "data-balance-fixes")
	assert.Contains(t, body, "−Rp\u00a050.000")
}

func TestHome_ShowsStartingBalancesOnlyWhenThereAreAny(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	_, err := f.Periods.EnsureCurrent(f.projectCtx(t))
	require.NoError(t, err)
	f.record(t, transaction.KindExpense, 2500000, nil, "kopi")
	assert.NotContains(t, f.home(t, "/overview").Body.String(), "data-starting-balances")

	f.ReportReader.Summaries = report.PeriodSummary{Currency: money.IDR, Income: money.Zero(money.IDR), Expense: money.Zero(money.IDR), Net: money.Zero(money.IDR), Adjustments: money.New(-5000000, money.IDR), Openings: money.New(1397276900, money.IDR)}
	body := f.home(t, "/overview").Body.String()
	require.Contains(t, body, "data-starting-balances")
	assert.Contains(t, body, "+Rp\u00a013.972.769")
	assert.Less(t, strings.Index(body, "data-starting-balances"), strings.Index(body, "data-balance-fixes"), "starting balances sit just above balance fixes")
}

func TestHome_ClosedPeriodShowsItsStoredStartingBalances(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	past := f.withClock(time.Now().AddDate(0, 0, -45))
	ctx := f.projectCtx(t)
	old, err := past.EnsureCurrent(ctx)
	require.NoError(t, err)
	_, err = f.Transactions.Record(ctx, transaction.RecordInput{
		WalletID: f.Cash, Kind: transaction.KindExpense, Amount: money.New(2500000, money.IDR),
		OccurredAt: time.Now().AddDate(0, 0, -44),
	})
	require.NoError(t, err)
	end, err := f.Periods.SuggestedEnd(ctx, old.ID)
	require.NoError(t, err)
	f.Snapshots.openings = 1397276900
	_, err = f.Periods.Close(ctx, old.ID, end, f.Principal.UserID)
	require.NoError(t, err)
	f.ReportReader.Summaries = report.PeriodSummary{Currency: money.IDR, Income: money.Zero(money.IDR), Expense: money.Zero(money.IDR), Net: money.Zero(money.IDR), Openings: money.New(100000000, money.IDR)}

	body := f.home(t, "/overview?period="+old.ID.String()).Body.String()

	require.Contains(t, body, "data-starting-balances")
	assert.Contains(t, body, "+Rp\u00a013.972.769", "a closed period reads the starting balances stored when it closed")
	assert.NotContains(t, body, "Rp\u00a01.000.000", "not today's live sum")
}

func TestHome_PeriodSwitcherAndClosedPeriod(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	past := f.withClock(time.Now().AddDate(0, 0, -45))
	ctx := f.projectCtx(t)
	old, err := past.EnsureCurrent(ctx)
	require.NoError(t, err)
	end, err := f.Periods.SuggestedEnd(ctx, old.ID)
	require.NoError(t, err)
	_, err = f.Periods.Close(ctx, old.ID, end, f.Principal.UserID)
	require.NoError(t, err)
	cur, err := f.Periods.Current(ctx)
	require.NoError(t, err)

	body := f.home(t, "/overview").Body.String()
	assert.Contains(t, body, `href="`+f.path("/overview?period="+old.ID.String())+`"`, "the current period links back")
	assert.NotContains(t, body, "data-home-closed")

	closed := f.home(t, "/overview?period="+old.ID.String()).Body.String()
	assert.Contains(t, closed, "data-home-closed")
	assert.Contains(t, closed, `href="`+f.path("/periods")+`"`)
	assert.Contains(t, closed, `href="`+f.path("/overview?period="+cur.ID.String())+`"`, "a past period links forward")
	assert.NotContains(t, closed, "data-home-ended", "the end-of-period banner shows on the current period only")
}

func TestHome_UnknownPeriodFallsBackToCurrent(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	cur, err := f.Periods.EnsureCurrent(f.projectCtx(t))
	require.NoError(t, err)

	rec := f.home(t, "/overview?period="+uuid.NewString())

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), cur.StartDate.In(time.UTC).Format("January 2006"))
}

func TestHome_EndedPeriodShowsTheCloseBanner(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	past := f.withClock(time.Now().AddDate(0, 0, -45))
	old, err := past.EnsureCurrent(f.projectCtx(t))
	require.NoError(t, err)
	f.ReportReader.Summaries = report.PeriodSummary{Currency: money.IDR, Expense: money.New(12300000, money.IDR)}

	body := f.home(t, "/overview").Body.String()

	assert.Contains(t, body, "data-home-ended")
	assert.Contains(t, body, "Rp\u00a0123.000")
	assert.Contains(t, body, `href="`+f.path("/periods/"+old.ID.String()+"/close")+`"`)
}

func TestHome_PaydayBannerAsksBeforeClosing(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	past := f.withClock(time.Now().AddDate(0, 0, -45))
	_, err := past.EnsureCurrent(f.projectCtx(t))
	require.NoError(t, err)

	body := f.home(t, "/overview").Body.String()

	assert.Contains(t, body, "Gaji sudah masuk?", "the banner asks instead of announcing an end")
	assert.Contains(t, body, `class="btn btn-ghost">Belum</button>`)
}

func TestHome_NotYetHidesThePaydayBannerForThreeDays(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	past := f.withClock(time.Now().AddDate(0, 0, -45))
	old, err := past.EnsureCurrent(f.projectCtx(t))
	require.NoError(t, err)
	mux := http.NewServeMux()
	f.overviewHandler().Register(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, f.request(t, http.MethodPost, f.path("/periods/"+old.ID.String()+"/snooze"), url.Values{}, false))
	require.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, f.path("/overview"), rec.Header().Get("Location"))
	cookies := rec.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.Equal(t, "payday_snooze_"+old.ID.String(), cookies[0].Name)
	assert.Equal(t, 3*24*60*60, cookies[0].MaxAge)

	r := f.request(t, http.MethodGet, f.path("/overview"), nil, false)
	r.AddCookie(cookies[0])
	home := httptest.NewRecorder()
	mux.ServeHTTP(home, r)
	assert.NotContains(t, home.Body.String(), "data-home-ended")

	other := f.request(t, http.MethodGet, f.path("/overview"), nil, false)
	other.AddCookie(&http.Cookie{Name: "payday_snooze_" + uuid.NewString(), Value: "1"})
	again := httptest.NewRecorder()
	mux.ServeHTTP(again, other)
	assert.Contains(t, again.Body.String(), "data-home-ended", "a snooze covers only its own period")
}

func TestHome_PaydayBannerForAMemberHasNoClose(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	past := f.withClock(time.Now().AddDate(0, 0, -45))
	old, err := past.EnsureCurrent(f.projectCtx(t))
	require.NoError(t, err)
	octx := fakes.Owner(context.Background(), tenant.Context{OrgID: f.OrgID, UserID: f.Principal.UserID})
	member := uuid.New()
	_, err = f.Orgs.AddMember(octx, f.OrgID, member, org.RoleMember)
	require.NoError(t, err)
	f.Principal.UserID = member

	body := f.home(t, "/overview").Body.String()

	assert.Contains(t, body, "data-home-ended")
	assert.Contains(t, body, "Pengeluaran Anda di ")
	assert.Contains(t, body, `class="btn btn-ghost">Belum</button>`)
	assert.NotContains(t, body, `href="`+f.path("/periods/"+old.ID.String()+"/close")+`"`, "a member cannot close the period")
}

func TestHome_NotYetRejectsABadPeriodID(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	mux := http.NewServeMux()
	f.overviewHandler().Register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, f.request(t, http.MethodPost, f.path("/periods/not-a-uuid/snooze"), url.Values{}, false))
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Empty(t, rec.Result().Cookies())
}

func TestHome_RunningPeriodHasNoCloseBanner(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	_, err := f.Periods.EnsureCurrent(f.projectCtx(t))
	require.NoError(t, err)

	assert.NotContains(t, f.home(t, "/overview").Body.String(), "data-home-ended")
}

func TestHome_SwipeTipAfterTheThirdEntry(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	_, err := f.Periods.EnsureCurrent(f.projectCtx(t))
	require.NoError(t, err)
	f.record(t, transaction.KindExpense, 100000, &f.Food, "Satu")
	f.record(t, transaction.KindExpense, 100000, &f.Food, "Dua")
	assert.NotContains(t, f.home(t, "/overview").Body.String(), `data-tip="row-actions"`)

	f.record(t, transaction.KindExpense, 100000, &f.Food, "Tiga")
	assert.Contains(t, f.home(t, "/overview").Body.String(), `data-tip="row-actions"`)
}

func TestHome_LoadFailureRendersTheErrorState(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	_, err := f.Periods.EnsureCurrent(f.projectCtx(t))
	require.NoError(t, err)
	f.ReportReader.SummaryErr = errors.New("db down")

	rec := f.home(t, "/overview")

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, `role="alert"`)
	assert.Contains(t, body, `href="`+f.path("/overview")+`"`, "Try again reloads Home")
}

func TestHome_DayTotalsIgnoreTransfers(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	ctx := f.projectCtx(t)
	_, err := f.Periods.EnsureCurrent(ctx)
	require.NoError(t, err)
	f.record(t, transaction.KindExpense, 1000000, &f.Food, "Makan")
	_, err = f.Transactions.Record(ctx, transaction.RecordInput{
		WalletID: f.Cash, ToWalletID: &f.Bank, Kind: transaction.KindTransfer,
		Amount: money.New(50000000, money.IDR), OccurredAt: time.Now(),
	})
	require.NoError(t, err)

	body := f.home(t, "/overview").Body.String()

	assert.Equal(t, []string{"−Rp\u00a010.000"}, dayTotals(body))
}

func TestHome_ReopenedPastPeriodHasNoCloseBanner(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	past := f.withClock(time.Now().AddDate(0, 0, -45))
	ctx := f.projectCtx(t)
	old, err := past.EnsureCurrent(ctx)
	require.NoError(t, err)
	end, err := f.Periods.SuggestedEnd(ctx, old.ID)
	require.NoError(t, err)
	_, err = f.Periods.Close(ctx, old.ID, end, f.Principal.UserID)
	require.NoError(t, err)
	_, err = f.Periods.Reopen(ctx, old.ID)
	require.NoError(t, err)

	body := f.home(t, "/overview?period="+old.ID.String()).Body.String()

	assert.NotContains(t, body, "data-home-closed")
	assert.NotContains(t, body, "data-home-ended")
}

func TestHome_QuickAddSkipsArchivedCategories(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	ctx := f.projectCtx(t)
	_, err := f.Periods.EnsureCurrent(ctx)
	require.NoError(t, err)
	f.record(t, transaction.KindExpense, 4000000, &f.Food, "Kopi")
	_, err = f.TxCategories.Archive(ctx, f.Food)
	require.NoError(t, err)

	assert.NotContains(t, f.home(t, "/overview").Body.String(), "category="+f.Food.String())
}

func TestHome_NewProjectShowsAMemberTheSetupHint(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	octx := fakes.Owner(context.Background(), tenant.Context{OrgID: f.OrgID, UserID: f.Principal.UserID})
	fresh, err := f.Projects.Create(octx, f.OrgID, "fresh", "Fresh")
	require.NoError(t, err)
	member := uuid.New()
	_, err = f.Orgs.AddMember(octx, f.OrgID, member, org.RoleMember)
	require.NoError(t, err)
	f.Principal.UserID = member
	f.ProjSlug = fresh.Slug

	rec := f.home(t, "/overview")

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "data-home-setup-pending")
	assert.NotContains(t, body, `href="`+f.path("/wallets/new")+`"`, "a member cannot set the project up")
	assert.NotContains(t, body, f.path("/first-run"))
}

func TestHome_SiblingProjectPeriodFallsBackToCurrent(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	cur, err := f.Periods.EnsureCurrent(f.projectCtx(t))
	require.NoError(t, err)
	other, _ := f.siblingProject(t)
	sctx := f.scoped(f.OrgID, other, f.Principal.UserID)
	theirs, err := f.Periods.EnsureCurrent(sctx)
	require.NoError(t, err)
	_, err = f.Periods.Rename(sctx, theirs.ID, "Sibling period")
	require.NoError(t, err)

	rec := f.home(t, "/overview?period="+theirs.ID.String())

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, cur.StartDate.In(time.UTC).Format("January 2006"))
	assert.NotContains(t, body, "Sibling period")
}
