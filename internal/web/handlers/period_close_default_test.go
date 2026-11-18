package handlers_test

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/civil"
	"altalune.id/yasaku/internal/transaction"
	"altalune.id/yasaku/internal/wallet"
	"altalune.id/yasaku/money"
)

func TestPeriods_CloseDialogDefaultsToYesterdayUnlessTodayHasEntries(t *testing.T) {
	f := newPeriodFixture(t, defaultCaps())
	f.setNow(time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC))
	ctx := f.scoped(t)
	w, err := f.Wallets.Create(ctx, wallet.Params{Name: "BCA", Kind: wallet.KindBank, Currency: money.IDR})
	require.NoError(t, err)
	record := func(at time.Time) {
		t.Helper()
		_, err := f.Txs.Record(ctx, transaction.RecordInput{
			WalletID: w.ID, Kind: transaction.KindExpense, Amount: money.New(10_000, money.IDR), OccurredAt: at,
		})
		require.NoError(t, err)
	}
	record(time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC))
	yesterday, today := civil.Date{Year: 2026, Month: 10, Day: 13}, civil.Date{Year: 2026, Month: 10, Day: 14}

	page := f.do(t, http.MethodGet, f.path("/periods"), nil)
	require.Equal(t, http.StatusOK, page.Code)
	assert.Contains(t, page.Body.String(), `value="`+yesterday.String()+`"`, "no entry today: the dialog defaults to yesterday")
	assert.NotContains(t, page.Body.String(), "data-end-today")

	record(time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC))
	page = f.do(t, http.MethodGet, f.path("/periods"), nil)
	require.Equal(t, http.StatusOK, page.Code)
	assert.Contains(t, page.Body.String(), `value="`+today.String()+`"`, "an entry dated today keeps today in the period")
	assert.Contains(t, page.Body.String(), "data-end-today", "closing today too is called out")
}

// SECURITY: money correctness: the console refuses a close that would freeze entries dated after the end, with the latest date in plain words.
func TestPeriods_PostClose_AnEndBeforeTheLatestEntryIsAFieldError(t *testing.T) {
	f := newPeriodFixture(t, defaultCaps())
	f.setNow(time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC))
	ctx := f.scoped(t)
	w, err := f.Wallets.Create(ctx, wallet.Params{Name: "BCA", Kind: wallet.KindBank, Currency: money.IDR})
	require.NoError(t, err)
	_, err = f.Txs.Record(ctx, transaction.RecordInput{
		WalletID: w.ID, Kind: transaction.KindExpense, Amount: money.New(1_234_500, money.IDR),
		OccurredAt: time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	cur, err := f.Periods.Current(ctx)
	require.NoError(t, err)

	rec := f.do(t, http.MethodPost, f.path("/periods/"+cur.ID.String()+"/close"), url.Values{"end_date": {"2026-10-13"}})
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, `value="2026-10-13"`, "the chosen end date is kept")
	assert.Contains(t, body, `data-end-error role="alert" class="text-sm text-error">period.end_before_latest`, "an end-date error, not a banner")
	assert.Contains(t, body, `aria-invalid="true" id="close-end-date"`)
	assert.NotContains(t, body, "report.no_data", "the refusal still previews the period's totals")
	assert.Contains(t, body, "12,345", "the period's expense shows in the preview")
	got, err := f.Periods.ByID(ctx, cur.ID)
	require.NoError(t, err)
	assert.False(t, got.IsLocked())
}

func TestPeriods_CloseWithAFutureEntryNamesItAndNeverDefaultsPastToday(t *testing.T) {
	f := newPeriodFixture(t, defaultCaps())
	f.setNow(time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC))
	ctx := f.scoped(t)
	w, err := f.Wallets.Create(ctx, wallet.Params{Name: "BCA", Kind: wallet.KindBank, Currency: money.IDR})
	require.NoError(t, err)
	_, err = f.Txs.Record(ctx, transaction.RecordInput{
		WalletID: w.ID, Kind: transaction.KindExpense, Amount: money.New(10_000, money.IDR),
		OccurredAt: time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	cur, err := f.Periods.Current(ctx)
	require.NoError(t, err)
	closePath := f.path("/periods/" + cur.ID.String() + "/close")

	page := f.do(t, http.MethodGet, closePath, nil)
	require.Equal(t, http.StatusOK, page.Code)
	assert.Contains(t, page.Body.String(), `value="2026-10-14" max="2026-10-14"`, "the default end is today, never past max")

	rec := f.do(t, http.MethodPost, closePath, url.Values{"end_date": {"2026-10-14"}})
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), "period.end_future_entry", "a future entry gets its own message")
	assert.NotContains(t, rec.Body.String(), "period.end_before_latest")
}

func TestPeriods_ClosePreviewForAnEarlyEndShowsTotalsAndTheFieldError(t *testing.T) {
	f := newPeriodFixture(t, defaultCaps())
	f.setNow(time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC))
	ctx := f.scoped(t)
	w, err := f.Wallets.Create(ctx, wallet.Params{Name: "BCA", Kind: wallet.KindBank, Currency: money.IDR})
	require.NoError(t, err)
	_, err = f.Txs.Record(ctx, transaction.RecordInput{
		WalletID: w.ID, Kind: transaction.KindExpense, Amount: money.New(1_234_500, money.IDR),
		OccurredAt: time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	cur, err := f.Periods.Current(ctx)
	require.NoError(t, err)

	rec := f.doHTMX(t, http.MethodGet, f.path("/periods/"+cur.ID.String()+"/close?end_date=2026-10-13"), nil)
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, `id="close-preview"`, "an htmx date change gets only the preview fragment")
	assert.NotContains(t, body, "report.no_data", "the preview still shows the period's totals")
	assert.Contains(t, body, "12,345")
	assert.Contains(t, body, `id="close-end-error" data-end-error`, "the preview shows the same end-date error the submit would")
	assert.Contains(t, body, "period.end_before_latest")
}
