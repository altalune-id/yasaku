package handlers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/civil"
	"altalune.id/yasaku/internal/period"
	"altalune.id/yasaku/internal/transaction"
	"altalune.id/yasaku/internal/wallet"
	"altalune.id/yasaku/internal/web/handlers"
)

func openingDater(periods *period.Service) wallet.OpeningDaterFunc {
	return func(ctx context.Context, at time.Time) (wallet.OpeningDate, error) {
		d, err := periods.OpeningDate(ctx, at)
		if err != nil {
			return wallet.OpeningDate{}, err
		}
		return wallet.OpeningDate{At: d.At, Date: d.Date, Moved: d.Moved}, nil
	}
}

func (f *txFixture) walletMux() *http.ServeMux {
	open := wallet.NewOpenWorkflow(f.Wallets, f.Transactions, openingDater(f.Periods), wallet.UnitOfWork(txPassthroughUoW),
		discardLogger(), passthroughUnexpected())
	h := handlers.NewWalletHandler(f.Deps, f.Projects, f.Wallets, open, f.Transactions, f.Ledgers)
	mux := http.NewServeMux()
	h.Register(mux)
	return mux
}

func (f *txFixture) closeTodaysPeriod(t *testing.T) civil.Date {
	t.Helper()
	ctx := f.projectCtx(t)
	cur, err := f.Periods.EnsureCurrent(ctx)
	require.NoError(t, err)
	loc, err := f.Ledgers.Location(ctx, f.OrgID, f.ProjID)
	require.NoError(t, err)
	today := civil.DateOf(time.Now(), loc)
	_, err = f.Periods.Close(ctx, cur.ID, today, f.Principal.UserID)
	require.NoError(t, err)
	return today
}

func (f *txFixture) openingOf(t *testing.T, name string) *transaction.Transaction {
	t.Helper()
	ctx := f.projectCtx(t)
	items, _, err := f.Transactions.List(ctx, transaction.ListOpts{})
	require.NoError(t, err)
	ws, err := f.Wallets.List(ctx, wallet.ListOpts{})
	require.NoError(t, err)
	for _, w := range ws {
		if w.Name != name {
			continue
		}
		for _, it := range items {
			if it.WalletID == w.ID && it.Kind == transaction.KindOpening {
				return it
			}
		}
	}
	t.Fatalf("no opening balance for %s", name)
	return nil
}

func (f *txFixture) postNewWallet(t *testing.T, mux *http.ServeMux, name string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, f.request(t, http.MethodPost, f.path("/wallets"), url.Values{
		"name": {name}, "kind": {"bank"}, "opening_balance": {"250000"},
	}, false))
	return rec
}

func TestWallet_CreateAfterClosingToday_DatesTheOpeningInTheNextPeriodAndSaysSo(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	today := f.closeTodaysPeriod(t)
	next := today.AddDays(1)
	mux := f.walletMux()

	rec := f.postNewWallet(t, mux, "Jago")
	require.Equal(t, http.StatusSeeOther, rec.Code, "the wallet is still created: %s", rec.Body.String())
	loc := rec.Header().Get("Location")
	assert.Contains(t, loc, "opening="+next.String())

	opening := f.openingOf(t, "Jago")
	tz, err := f.Ledgers.Location(f.projectCtx(t), f.OrgID, f.ProjID)
	require.NoError(t, err)
	assert.True(t, next.In(tz).Equal(opening.OccurredAt),
		"dated 00:00 project time on the next period's start; got %s", opening.OccurredAt)

	page := httptest.NewRecorder()
	mux.ServeHTTP(page, f.request(t, http.MethodGet, loc, nil, false))
	require.Equal(t, http.StatusOK, page.Code)
	body := page.Body.String()
	assert.Contains(t, body, `data-wallet-notice="opening-moved"`)
	assert.Contains(t, body, next.String())
	assert.NotContains(t, body, "wallet.opening_moved", "the notice must be translated")
}

func TestWallet_CreateInAnOpenPeriod_KeepsTodayAndShowsNoNotice(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	mux := f.walletMux()

	rec := f.postNewWallet(t, mux, "Jago")
	require.Equal(t, http.StatusSeeOther, rec.Code)
	loc := rec.Header().Get("Location")
	assert.NotContains(t, loc, "opening=")

	opening := f.openingOf(t, "Jago")
	assert.WithinDuration(t, time.Now(), opening.OccurredAt, time.Minute)

	page := httptest.NewRecorder()
	mux.ServeHTTP(page, f.request(t, http.MethodGet, loc, nil, false))
	require.Equal(t, http.StatusOK, page.Code)
	assert.NotContains(t, page.Body.String(), "data-wallet-notice")
}

func TestWallet_GetWallets_ShowsTheOpeningNoticeOnlyWhenTheOpeningReallyMoved(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	mux := f.walletMux()
	require.Equal(t, http.StatusSeeOther, f.postNewWallet(t, mux, "Jago").Code)
	jago := f.openingOf(t, "Jago")
	tz, err := f.Ledgers.Location(f.projectCtx(t), f.OrgID, f.ProjID)
	require.NoError(t, err)
	onDay := civil.DateOf(jago.OccurredAt, tz)

	for name, q := range map[string]string{
		"malformed date":       "?opening=<script>&wallet=" + jago.WalletID.String(),
		"no wallet":            "?opening=" + onDay.String(),
		"a date it is not on":  "?opening=" + onDay.AddDays(5).String() + "&wallet=" + jago.WalletID.String(),
		"an unknown wallet id": "?opening=" + onDay.String() + "&wallet=" + uuid.NewString(),
	} {
		t.Run(name, func(t *testing.T) {
			page := httptest.NewRecorder()
			mux.ServeHTTP(page, f.request(t, http.MethodGet, f.path("/wallets"+q), nil, false))
			require.Equal(t, http.StatusOK, page.Code)
			assert.NotContains(t, page.Body.String(), "data-wallet-notice", "a crafted URL must not show the notice")
		})
	}
}
