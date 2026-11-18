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
	"altalune.id/yasaku/internal/transaction"
)

func TestTransactionHandler_PostCreate_TransferDestinationIsAFieldError(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		to   func(f *txFixture) string
		key  string
		htmx bool
	}{
		{"missing, sheet", func(*txFixture) string { return "" }, "tx.to_wallet_error.required", true},
		{"missing, full page", func(*txFixture) string { return "" }, "tx.to_wallet_error.required", false},
		{"same as source, sheet", func(f *txFixture) string { return f.Cash.String() }, "tx.to_wallet_error.same", true},
		{"same as source, full page", func(f *txFixture) string { return f.Cash.String() }, "tx.to_wallet_error.same", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newTxFixture(t)
			mux := http.NewServeMux()
			f.txHandler().Register(mux)

			date := civil.DateOf(time.Now(), time.UTC).AddDays(-1).String()
			form := url.Values{
				"kind":         {"transfer"},
				"amount":       {"12.500"},
				"wallet_id":    {f.Cash.String()},
				"to_wallet_id": {tc.to(f)},
				"note":         {"Isi dompet"},
				"date":         {date},
			}
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, f.request(t, http.MethodPost, f.path("/transactions"), form, tc.htmx))

			body := rec.Body.String()
			if tc.htmx {
				require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
			} else {
				require.Equal(t, http.StatusOK, rec.Code)
				assert.Contains(t, body, "<html")
			}
			assert.Contains(t, body, f.Bundle.For(i18n.IdID).T(tc.key))
			assert.Contains(t, body, `aria-describedby="tx-to-wallet-hint"`)
			assert.Contains(t, body, `value="12.500"`)
			assert.Contains(t, body, "Isi dompet")
			assert.Contains(t, body, date)

			items, _, err := f.Transactions.List(f.projectCtx(t), transaction.ListOpts{})
			require.NoError(t, err)
			assert.Empty(t, items)
		})
	}
}
