package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/civil"
	"altalune.id/yasaku/internal/i18n"
	"altalune.id/yasaku/internal/ledger"
	"altalune.id/yasaku/internal/transaction"
	"altalune.id/yasaku/internal/wallet"
	"altalune.id/yasaku/money"
)

func TestTransactionHandler_PostCreate_IdIDAmounts(t *testing.T) {
	t.Parallel()
	post := func(t *testing.T, f *txFixture, amount string) *httptest.ResponseRecorder {
		t.Helper()
		mux := http.NewServeMux()
		f.txHandler().Register(mux)
		form := url.Values{
			"kind":      {"expense"},
			"amount":    {amount},
			"wallet_id": {f.Cash.String()},
			"date":      {civil.DateOf(time.Now(), time.UTC).String()},
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, f.request(t, http.MethodPost, f.path("/transactions"), form, true))
		return rec
	}

	t.Run("decimal part on IDR is refused in the user's language", func(t *testing.T) {
		t.Parallel()
		f := newTxFixture(t)
		rec := post(t, f, "12.500,50")
		require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
		assert.Contains(t, rec.Body.String(), f.Bundle.For("id-ID").T("tx.amount_error.no_decimals"))
		assert.NotContains(t, rec.Body.String(), "currency has no decimals")
		items, _, err := f.Transactions.List(f.projectCtx(t), transaction.ListOpts{})
		require.NoError(t, err)
		assert.Empty(t, items)
	})

	t.Run("grouped thousands save", func(t *testing.T) {
		t.Parallel()
		f := newTxFixture(t)
		rec := post(t, f, "12.500")
		require.Equal(t, http.StatusOK, rec.Code)
		items, _, err := f.Transactions.List(f.projectCtx(t), transaction.ListOpts{})
		require.NoError(t, err)
		require.Len(t, items, 1)
		assert.Equal(t, int64(1250000), items[0].Amount.Minor)
	})
}

func TestTransactionHandler_PostCreate_ZeroIsRefusedInTheUsersLanguage(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	mux := http.NewServeMux()
	f.txHandler().Register(mux)
	form := url.Values{
		"kind": {"expense"}, "amount": {"0"}, "wallet_id": {f.Cash.String()},
		"date": {civil.DateOf(time.Now(), time.UTC).String()},
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, f.request(t, http.MethodPost, f.path("/transactions"), form, true))
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), f.Bundle.For(i18n.IdID).T("tx.amount_error.not_positive"))
	assert.NotContains(t, rec.Body.String(), "must be greater than zero")
}

func TestAmount_RequestLocaleReachesEveryParser(t *testing.T) {
	t.Parallel()
	usd := money.Currency("USD")
	cases := []struct {
		loc  i18n.Locale
		save bool
	}{{i18n.IdID, true}, {i18n.EnUS, false}}

	for _, tc := range cases {
		t.Run("transaction create "+tc.loc.String(), func(t *testing.T) {
			t.Parallel()
			f := newTxFixture(t)
			f.Loc = tc.loc
			w, err := f.Wallets.Create(f.projectCtx(t), wallet.Params{Name: "Wise", Kind: wallet.KindBank, Currency: usd})
			require.NoError(t, err)
			mux := http.NewServeMux()
			f.txHandler().Register(mux)
			form := url.Values{
				"kind": {"expense"}, "amount": {"12.500"}, "wallet_id": {w.ID.String()},
				"date": {civil.DateOf(time.Now(), time.UTC).String()},
			}
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, f.request(t, http.MethodPost, f.path("/transactions"), form, true))
			items, _, err := f.Transactions.List(f.projectCtx(t), transaction.ListOpts{})
			require.NoError(t, err)
			if tc.save {
				require.Equal(t, http.StatusOK, rec.Code)
				require.Len(t, items, 1)
				assert.Equal(t, money.New(1250000, usd), items[0].Amount)
				return
			}
			require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
			assert.Contains(t, rec.Body.String(), f.Bundle.For(tc.loc).T("tx.amount_error.too_many_decimals"))
			assert.Empty(t, items)
		})

		t.Run("wallet opening balance "+tc.loc.String(), func(t *testing.T) {
			t.Parallel()
			f := newWalletFixture(t)
			f.Loc = tc.loc
			_, err := f.Ledgers.Update(f.scoped(t), ledger.Patch{Currency: &usd})
			require.NoError(t, err)
			rec := f.do(t, f.walletMux(t), http.MethodPost, "/wallets", url.Values{
				"name": {"Wise"}, "kind": {"bank"}, "opening_balance": {"12.500"},
			})
			txs, _, err := f.Transactions.List(f.scoped(t), transaction.ListOpts{})
			require.NoError(t, err)
			if tc.save {
				require.Equal(t, http.StatusSeeOther, rec.Code)
				require.Len(t, txs, 1)
				assert.Equal(t, money.New(1250000, usd), txs[0].Amount)
				return
			}
			require.Equal(t, http.StatusOK, rec.Code)
			assert.Contains(t, rec.Body.String(), f.Deps.I18n.For(tc.loc).T("tx.amount_error.too_many_decimals"))
			assert.Empty(t, txs)
		})

		t.Run("wallet reconcile "+tc.loc.String(), func(t *testing.T) {
			t.Parallel()
			f := newWalletFixture(t)
			f.Loc = tc.loc
			w, err := f.Wallets.Create(f.scoped(t), wallet.Params{Name: "Wise", Kind: wallet.KindBank, Currency: usd})
			require.NoError(t, err)
			rec := f.do(t, f.walletMux(t), http.MethodPost, "/wallets/"+w.ID.String()+"/adjust", url.Values{"target": {"12.500"}})
			txs, _, err := f.Transactions.List(f.scoped(t), transaction.ListOpts{WalletID: &w.ID})
			require.NoError(t, err)
			if tc.save {
				require.Equal(t, http.StatusSeeOther, rec.Code)
				require.Len(t, txs, 1)
				assert.Equal(t, money.New(1250000, usd), txs[0].Amount)
				return
			}
			require.Equal(t, http.StatusOK, rec.Code)
			assert.Contains(t, rec.Body.String(), f.Deps.I18n.For(tc.loc).T("tx.amount_error.too_many_decimals"))
			assert.Empty(t, txs)
		})
	}
}
