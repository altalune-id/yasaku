package handlers_test

import (
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

	"altalune.id/yasaku/civil"
	"altalune.id/yasaku/internal/category"
	"altalune.id/yasaku/internal/i18n"
	"altalune.id/yasaku/internal/transaction"
	"altalune.id/yasaku/internal/wallet"
	"altalune.id/yasaku/internal/web/handlers"
	"altalune.id/yasaku/money"
)

func (f *txFixture) requestIn(t *testing.T, loc i18n.Locale, method, target string, form url.Values, htmx bool) *http.Request {
	t.Helper()
	r := f.request(t, method, target, form, htmx)
	ctx := i18n.LocaleInto(r.Context(), loc)
	ctx = i18n.TranslatorInto(ctx, f.Bundle.For(loc))
	return r.WithContext(ctx)
}

func (f *txFixture) serveTx(t *testing.T, r *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	f.txHandler().Register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, r)
	return rec
}

func (f *txFixture) recordTx(t *testing.T, in transaction.RecordInput) *transaction.Transaction {
	t.Helper()
	if in.Kind == "" {
		in.Kind = transaction.KindExpense
	}
	if in.WalletID == uuid.Nil {
		in.WalletID = f.Cash
	}
	if in.OccurredAt.IsZero() {
		in.OccurredAt = time.Now()
	}
	tx, err := f.Transactions.Record(f.projectCtx(t), in)
	require.NoError(t, err)
	return tx
}

func (f *txFixture) archiveAllWallets(t *testing.T) {
	t.Helper()
	ctx := f.projectCtx(t)
	for _, id := range []uuid.UUID{f.Cash, f.Bank} {
		_, err := f.Wallets.Archive(ctx, id)
		require.NoError(t, err)
	}
}

func (f *txFixture) today() string { return civil.DateOf(time.Now(), time.UTC).String() }

func (f *txFixture) savedMinor(t *testing.T) []int64 {
	t.Helper()
	items, _, err := f.Transactions.List(f.projectCtx(t), transaction.ListOpts{})
	require.NoError(t, err)
	out := make([]int64, 0, len(items))
	for _, it := range items {
		out = append(out, it.Amount.Minor)
	}
	return out
}

var amountHintRe = regexp.MustCompile(`(?s)<input aria-describedby="tx-amount-hint" aria-invalid="true" id="tx-amount"[^>]*>.*?id="tx-amount-hint"`)

// TestTransactionAmountWithoutJS is the review-focus table: a no-JS POST is parsed in the user's locale and never folds a decimal into the whole number.
func TestTransactionAmountWithoutJS(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		loc       i18n.Locale
		usd       bool
		amount    string
		wantMinor int64
		errKey    string
	}{
		{name: "id-ID IDR with decimals is refused", loc: i18n.IdID, amount: "12.500,50", errKey: "tx.amount_error.no_decimals"},
		{name: "id-ID IDR grouped saves", loc: i18n.IdID, amount: "12.500", wantMinor: 1250000},
		{name: "en-US USD grouped with decimals saves", loc: i18n.EnUS, usd: true, amount: "12,500.50", wantMinor: 1250050},
		{name: "expression is refused", loc: i18n.IdID, amount: "12+5", errKey: "tx.amount_error.expression"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newTxFixture(t)
			walletID := f.Cash
			if tc.usd {
				w, err := f.Wallets.Create(f.projectCtx(t), wallet.Params{Name: "Chase", Kind: wallet.KindBank, Currency: money.Currency("USD")})
				require.NoError(t, err)
				walletID = w.ID
			}
			form := url.Values{"kind": {"expense"}, "amount": {tc.amount}, "wallet_id": {walletID.String()}, "date": {f.today()}}
			rec := f.serveTx(t, f.requestIn(t, tc.loc, http.MethodPost, f.path("/transactions"), form, false))

			if tc.errKey == "" {
				require.Equal(t, http.StatusSeeOther, rec.Code, "body=%s", rec.Body.String())
				assert.Equal(t, []int64{tc.wantMinor}, f.savedMinor(t))
				return
			}
			require.Equal(t, http.StatusOK, rec.Code)
			body := rec.Body.String()
			assert.Contains(t, body, f.Bundle.For(tc.loc).T(tc.errKey))
			assert.Regexp(t, amountHintRe, body, "the refusal must sit on the amount field")
			assert.Empty(t, f.savedMinor(t))
		})
	}
}

func TestTransactionSavedAmountsRenderInTheLocale(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	form := url.Values{"kind": {"expense"}, "amount": {"12.500"}, "wallet_id": {f.Cash.String()}, "date": {f.today()}, "again": {"1"}}
	rec := f.serveTx(t, f.request(t, http.MethodPost, f.path("/transactions"), form, true))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Rp\u00a012.500")
}

func TestTransactionAmountInputIsDecimalTextWithTheLocaleMark(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	rec := f.serveTx(t, f.request(t, http.MethodGet, f.path("/transactions/new"), nil, false))
	require.Equal(t, http.StatusOK, rec.Code)
	tag := regexp.MustCompile(`<input[^>]*id="tx-amount"[^>]*>`).FindString(rec.Body.String())
	require.NotEmpty(t, tag)
	assert.Contains(t, tag, `type="text"`)
	assert.Contains(t, tag, `inputmode="decimal"`)
	assert.Contains(t, tag, `data-decimal=","`)
	assert.Contains(t, tag, `data-exponent="0"`)
	assert.Contains(t, tag, `data-currency="IDR"`)
	assert.NotContains(t, tag, `pattern=`, "a pattern would block an expression before the server can name it")

	rec = f.serveTx(t, f.requestIn(t, i18n.EnUS, http.MethodGet, f.path("/transactions/new"), nil, false))
	tag = regexp.MustCompile(`<input[^>]*id="tx-amount"[^>]*>`).FindString(rec.Body.String())
	assert.Contains(t, tag, `data-decimal="."`)
}

func TestTransactionNoWalletShowsTheCreateWalletCallToAction(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	f.archiveAllWallets(t)
	cta := f.Bundle.For(i18n.IdID).T("tx.no_wallet.cta")

	for _, path := range []string{"/transactions/new", "/transactions"} {
		rec := f.serveTx(t, f.request(t, http.MethodGet, f.path(path), nil, false))
		require.Equal(t, http.StatusOK, rec.Code, path)
		body := rec.Body.String()
		assert.Contains(t, body, cta, path)
		assert.Contains(t, body, `href="`+f.path("/wallets/new")+`"`, path)
		assert.NotContains(t, body, `id="tx-amount"`, "%s must not render an empty wallet field", path)
	}
}

func TestTransactionPostWithoutAWalletIsAFieldError(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	f.archiveAllWallets(t)
	form := url.Values{"kind": {"expense"}, "amount": {"40.000"}, "wallet_id": {""}, "date": {f.today()}, "note": {"Kopi"}}

	rec := f.serveTx(t, f.request(t, http.MethodPost, f.path("/transactions"), form, true))
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), f.Bundle.For(i18n.IdID).T("tx.wallet_error.required"))

	rec = f.serveTx(t, f.request(t, http.MethodPost, f.path("/transactions"), form, false))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), f.Bundle.For(i18n.IdID).T("tx.wallet_error.required"))
	assert.Empty(t, f.savedMinor(t))
}

func TestTransactionNewHonorsTheAddSheetQuery(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	salary, err := f.TxCategories.Create(f.projectCtx(t), "Gaji", category.KindIncome, "banknote", "chart-2")
	require.NoError(t, err)

	q := url.Values{"kind": {"income"}, "category": {salary.ID.String()}, "wallet": {f.Bank.String()}}
	rec := f.serveTx(t, f.request(t, http.MethodGet, f.path("/transactions/new?"+q.Encode()), nil, false))
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Regexp(t, `<input[^>]*name="kind"[^>]*value="income"[^>]*checked`, body)
	assert.Regexp(t, `<input[^>]*name="category_id"[^>]*value="`+salary.ID.String()+`"[^>]*checked`, body)
	assert.Regexp(t, `<option[^>]*value="`+f.Bank.String()+`"[^>]*selected`, body)
}

func TestTransactionNewPreselectsNoCategoryUnlessTheURLNamesOne(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	f.recordTx(t, transaction.RecordInput{Amount: money.New(100000, money.IDR), CategoryID: &f.Food})

	rec := f.serveTx(t, f.request(t, http.MethodGet, f.path("/transactions/new"), nil, false))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.NotRegexp(t, `<input[^>]*name="category_id"[^>]*checked`, rec.Body.String(), "a plain opener leaves the category for the user, as in the prototype")
}

func TestTransactionNewDuplicatesAnEntryToToday(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	src := f.recordTx(t, transaction.RecordInput{
		WalletID: f.Bank, Amount: money.New(2500000, money.IDR), CategoryID: &f.Food,
		Note: "Bakso urat", OccurredAt: time.Now().AddDate(0, 0, -3),
	})

	rec := f.serveTx(t, f.request(t, http.MethodGet, f.path("/transactions/new?from="+src.ID.String()), nil, false))
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Regexp(t, `<input[^>]*id="tx-amount"[^>]*value="25000"`, body)
	assert.Contains(t, body, `value="Bakso urat"`)
	assert.Regexp(t, `<option[^>]*value="`+f.Bank.String()+`"[^>]*selected`, body)
	assert.Regexp(t, `<input[^>]*name="date"[^>]*value="`+f.today()+`"`, body)
	assert.Contains(t, body, `action="`+f.path("/transactions")+`"`, "a duplicate records a new entry")
}

func TestTransactionDetailOffersEditDuplicateAndDelete(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	tx := f.recordTx(t, transaction.RecordInput{Amount: money.New(1000000, money.IDR), Note: "Parkir"})

	rec := f.serveTx(t, f.request(t, http.MethodGet, f.path("/transactions/"+tx.ID.String()), nil, false))
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "Parkir")
	assert.Contains(t, body, "Rp\u00a010.000")
	assert.Regexp(t, `<a[^>]*href="`+f.path("/transactions/"+tx.ID.String()+"/edit")+`"[^>]*class="[^"]*btn-primary`, body)
	assert.Contains(t, body, `href="`+f.path("/transactions/new?from="+tx.ID.String())+`"`)
	assert.Regexp(t, `<form[^>]*action="`+f.path("/transactions/"+tx.ID.String()+"/delete")+`"`, body)
	assert.NotContains(t, body, "window.confirm")
	assert.NotContains(t, body, "data-missing-icon")
}

func TestTransactionDetailOverHTMXIsOnlyTheEndSideDrawer(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	tx := f.recordTx(t, transaction.RecordInput{
		Amount: money.New(1000000, money.IDR), CategoryID: &f.Food, Note: "Parkir",
		OccurredAt: time.Date(2026, time.November, 15, 5, 0, 0, 0, time.UTC),
	})
	id := tx.ID.String()
	r := f.request(t, http.MethodGet, f.path("/transactions/"+id), nil, true)
	r.Header.Set("HX-Current-URL", "http://example.test"+f.path("/overview")+"?period=x")

	rec := f.serveTx(t, r)
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.NotContains(t, body, "<html", "an HTMX detail is only the dialog")
	assert.True(t, strings.HasPrefix(strings.TrimSpace(body), `<dialog id="tx-detail-`+id+`" class="modal modal-bottom lg:modal-end"`), body)
	assert.Contains(t, body, "Parkir")
	assert.Contains(t, body, "Rp\u00a010.000")
	assert.Contains(t, body, `data-date-style="long"`)
	assert.Regexp(t, `<a[^>]*href="`+f.path("/transactions/"+id+"/edit")+`"[^>]*class="[^"]*btn-primary`, body)
	assert.Contains(t, body, `href="`+f.path("/transactions/new?from="+id)+`"`)
	assert.Regexp(t, `<form[^>]*action="`+f.path("/transactions/"+id+"/delete")+`"`, body)
	assert.Contains(t, body, `name="return" value="overview"`, "delete returns to the page the drawer opened on")
}

func TestTransactionDetailOfASiblingProjectIsNotFound(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	_, id := f.siblingProject(t)
	rec := f.serveTx(t, f.request(t, http.MethodGet, f.path("/transactions/"+id.String()), nil, false))
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestTransactionEditShowsTheKindAsAStyledActiveTab(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	tx := f.recordTx(t, transaction.RecordInput{Amount: money.New(1000000, money.IDR)})
	rec := f.serveTx(t, f.request(t, http.MethodGet, f.path("/transactions/"+tx.ID.String()+"/edit"), nil, false))
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Regexp(t, `<input[^>]*class="tab[^"]*"[^>]*value="expense"[^>]*checked[^>]*disabled|<input[^>]*value="expense"[^>]*class="tab[^"]*"[^>]*checked[^>]*disabled`, body)
	assert.Contains(t, body, `name="kind" value="expense" data-tx-kind-locked`)
}

func TestTransactionDeleteThenUndoReRecordsTheEntry(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	tx := f.recordTx(t, transaction.RecordInput{
		WalletID: f.Bank, Amount: money.New(1250000, money.IDR), CategoryID: &f.Food, Note: "Soto",
	})

	rec := f.serveTx(t, f.request(t, http.MethodPost, f.path("/transactions/"+tx.ID.String()+"/delete"), url.Values{}, false))
	require.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, f.path("/transactions"), rec.Header().Get("Location"))
	assert.Empty(t, f.savedMinor(t), "delete commits at once")

	var undo *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == handlers.ToastCookie {
			undo = c
		}
	}
	require.NotNil(t, undo, "the delete must hand the next page its Undo")

	list := f.request(t, http.MethodGet, f.path("/transactions"), nil, false)
	list.AddCookie(undo)
	rec = f.serveTx(t, list)
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	toast := regexp.MustCompile(`(?s)<div id="toast".*?</form>`).FindString(body)
	require.NotEmpty(t, toast, "no Undo toast")
	assert.Contains(t, toast, `data-undo-toast`)
	assert.Contains(t, toast, `action="`+f.path("/transactions")+`"`)
	assert.Contains(t, toast, `name="amount" value="12500"`)
	assert.Contains(t, toast, `name="wallet_id" value="`+f.Bank.String()+`"`)
	assert.Contains(t, toast, `name="category_id" value="`+f.Food.String()+`"`)
	assert.Contains(t, toast, `name="note" value="Soto"`)
	assert.Contains(t, toast, `name="return" value="transactions"`)
	cleared := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == handlers.ToastCookie && c.MaxAge < 0 {
			cleared = true
		}
	}
	assert.True(t, cleared, "the Undo shows once")

	form := url.Values{}
	for _, m := range regexp.MustCompile(`name="([a-z_]+)" value="([^"]*)"`).FindAllStringSubmatch(toast, -1) {
		form.Set(m[1], m[2])
	}
	rec = f.serveTx(t, f.request(t, http.MethodPost, f.path("/transactions"), form, false))
	require.Equal(t, http.StatusSeeOther, rec.Code, "body=%s", rec.Body.String())
	assert.Equal(t, f.path("/transactions"), rec.Header().Get("Location"))
	items, _, err := f.Transactions.List(f.projectCtx(t), transaction.ListOpts{})
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.NotEqual(t, tx.ID, items[0].ID, "Undo records a new entry")
	assert.Equal(t, int64(1250000), items[0].Amount.Minor)
	assert.Equal(t, f.Bank, items[0].WalletID)
	assert.Equal(t, "Soto", items[0].Note)
	require.NotNil(t, items[0].CategoryID)
	assert.Equal(t, f.Food, *items[0].CategoryID)
}

func TestTransactionCategoryGridFollowsFrequency(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	ride, err := f.TxCategories.Create(f.projectCtx(t), "Ojek", category.KindExpense, "bus", "chart-3")
	require.NoError(t, err)
	for range 2 {
		f.recordTx(t, transaction.RecordInput{Amount: money.New(100000, money.IDR), CategoryID: &ride.ID})
	}
	f.recordTx(t, transaction.RecordInput{Amount: money.New(100000, money.IDR), CategoryID: &f.Food})

	rec := f.serveTx(t, f.request(t, http.MethodGet, f.path("/transactions/new"), nil, false))
	body := rec.Body.String()
	iRide := strings.Index(body, `name="category_id" value="`+ride.ID.String()+`"`)
	iFood := strings.Index(body, `name="category_id" value="`+f.Food.String()+`"`)
	require.Positive(t, iRide)
	require.Positive(t, iFood)
	assert.Less(t, iRide, iFood, "the most used category comes first")
}

func TestTransactionRecentIsFilteredByCategory(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	f.recordTx(t, transaction.RecordInput{Amount: money.New(2500000, money.IDR), CategoryID: &f.Food, Note: "Nasi padang"})

	rec := f.serveTx(t, f.request(t, http.MethodGet, f.path("/transactions/new"), nil, false))
	body := rec.Body.String()
	chip := regexp.MustCompile(`<button[^>]*data-recent[^>]*>`).FindString(body)
	require.NotEmpty(t, chip, "no Recent chip")
	assert.Contains(t, chip, `data-category="`+f.Food.String()+`"`)
	assert.Contains(t, chip, `data-amount="25000"`)
	assert.Contains(t, chip, `data-note="Nasi padang"`)
	assert.Contains(t, chip, `data-wallet="`+f.Cash.String()+`"`)
	assert.Contains(t, body, `src="/static/recent.js`)
	assert.Contains(t, body, `src="/static/keypad.js`)
	assert.NotContains(t, body, "data-missing-icon")
}

func TestTransactionRowsOfferTheSwipeActionsInAMenu(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	tx := f.recordTx(t, transaction.RecordInput{Amount: money.New(1000000, money.IDR), Note: "Parkir"})

	rec := f.serveTx(t, f.request(t, http.MethodGet, f.path("/transactions"), nil, false))
	body := rec.Body.String()
	id := tx.ID.String()
	opener := regexp.MustCompile(`<a[^>]*data-dialog-open="tx-row-actions"[^>]*>`).FindString(body)
	require.NotEmpty(t, opener, "no row actions opener")
	assert.Contains(t, opener, `href="`+f.path("/transactions/"+id)+`"`, "without JS the opener lands on the detail page")
	assert.Contains(t, opener, `data-row-edit="`+f.path("/transactions/"+id+"/edit")+`"`)
	assert.Contains(t, opener, `data-row-duplicate="`+f.path("/transactions/new?from="+id)+`"`)
	assert.Contains(t, opener, `data-row-delete="`+f.path("/transactions/"+id+"/delete")+`"`)
	assert.Equal(t, 1, strings.Count(body, `<dialog id="tx-row-actions"`), "one shared actions dialog, not one per row")
	list := regexp.MustCompile(`(?s)<div id="tx-list".*?<div id="toast"`).FindString(body)
	assert.NotContains(t, list, "<dialog", "dialogs belong in the dialog host, outside every swap target")
	menu := regexp.MustCompile(`(?s)<dialog id="tx-row-actions" class="modal modal-bottom[^"]*".*?</dialog>`).FindString(body)
	require.NotEmpty(t, menu)
	assert.Contains(t, menu, `data-row-target="edit"`)
	assert.Contains(t, menu, `data-row-target="duplicate"`)
	assert.Regexp(t, `<form[^>]*data-row-target="delete"`, menu)
	assert.Contains(t, menu, `max-lg:[&_button]:min-h-11 max-lg:[&_button]:content-center`, "menu rows keep a 44px phone target with the content centered")
	assert.Contains(t, menu, `name="return" value="transactions"`)
	assert.Contains(t, body, `id="tx-delete-`+id+`" method="post" action="`+f.path("/transactions/"+id+"/delete")+`"`)
	assert.NotContains(t, body, `data-row-menu`)
	assert.Contains(t, body, `href="`+f.path("/transactions/"+tx.ID.String())+`"`, "a row opens its detail")
	assert.Contains(t, body, `data-swipe`)
	assert.Contains(t, body, `src="/static/swipe.js`)
	assert.NotContains(t, body, "data-missing-icon")
}

func (f *txFixture) toastCookie(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == handlers.ToastCookie && c.MaxAge > 0 {
			return c
		}
	}
	return nil
}

func TestTransactionSheetSaveRedirectsToTheAllowlistedReturn(t *testing.T) {
	t.Parallel()
	cases := []struct {
		ret  string
		want string
	}{
		{ret: "overview", want: "/overview"},
		{ret: "transactions", want: "/transactions"},
		{ret: "wallets/WALLET", want: "/wallets/WALLET"},
		{ret: "https://evil.example", want: "/overview"},
		{ret: "wallets/not-a-uuid", want: "/overview"},
		{ret: "", want: "/overview"},
	}
	for _, tc := range cases {
		t.Run(tc.ret, func(t *testing.T) {
			t.Parallel()
			f := newTxFixture(t)
			ret := strings.ReplaceAll(tc.ret, "WALLET", f.Cash.String())
			want := f.path(strings.ReplaceAll(tc.want, "WALLET", f.Cash.String()))
			form := url.Values{"kind": {"expense"}, "amount": {"40.000"}, "wallet_id": {f.Cash.String()}, "date": {f.today()}, "return": {ret}}

			rec := f.serveTx(t, f.request(t, http.MethodPost, f.path("/transactions"), form, true))
			require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
			assert.Equal(t, want, rec.Header().Get("HX-Redirect"), "the sheet must not depend on a #tx-list on the opener page")
			assert.NotNil(t, f.toastCookie(t, rec), "the saved toast rides to the next page")

			rec = f.serveTx(t, f.request(t, http.MethodPost, f.path("/transactions"), form, false))
			require.Equal(t, http.StatusSeeOther, rec.Code)
			assert.Equal(t, want, rec.Header().Get("Location"))
		})
	}
}

func TestTransactionSheetSaveAndAddAnotherStaysInTheSheet(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	form := url.Values{"kind": {"expense"}, "amount": {"40.000"}, "wallet_id": {f.Cash.String()}, "date": {f.today()}, "return": {"overview"}, "again": {"1"}}
	rec := f.serveTx(t, f.request(t, http.MethodPost, f.path("/transactions"), form, true))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, rec.Header().Get("HX-Redirect"))
	assert.Contains(t, rec.Body.String(), `id="toast" hx-swap-oob="true"`)
	assert.Contains(t, rec.Body.String(), "Rp\u00a040.000")
}

func TestTransactionSheetCarriesTheOpenerPageAsReturn(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	rec := f.serveTx(t, f.request(t, http.MethodGet, f.path("/transactions"), nil, false))
	dialog := txDialogRe.FindString(rec.Body.String())
	require.NotEmpty(t, dialog)
	assert.Contains(t, dialog, `name="return" value="transactions"`)
	tag := quickAddTag.FindString(dialog)
	assert.NotContains(t, tag, `hx-target="#tx-list"`)
	assert.Contains(t, tag, `hx-swap="none"`)
}

func TestTransactionDeleteReturnsToTheOpenerPageWithUndo(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	tx := f.recordTx(t, transaction.RecordInput{Amount: money.New(1000000, money.IDR)})
	rec := f.serveTx(t, f.request(t, http.MethodPost, f.path("/transactions/"+tx.ID.String()+"/delete"), url.Values{"return": {"overview"}}, false))
	require.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, f.path("/overview"), rec.Header().Get("Location"))
	require.NotNil(t, f.toastCookie(t, rec))

	tx = f.recordTx(t, transaction.RecordInput{Amount: money.New(1000000, money.IDR)})
	rec = f.serveTx(t, f.request(t, http.MethodPost, f.path("/transactions/"+tx.ID.String()+"/delete"), url.Values{"return": {"overview"}}, true))
	assert.Equal(t, f.path("/overview"), rec.Header().Get("HX-Redirect"))
}

func TestTransactionSavedToastShowsOnceOnTheNextPage(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	form := url.Values{"kind": {"expense"}, "amount": {"40.000"}, "wallet_id": {f.Cash.String()}, "date": {f.today()}, "return": {"transactions"}}
	rec := f.serveTx(t, f.request(t, http.MethodPost, f.path("/transactions"), form, false))
	c := f.toastCookie(t, rec)
	require.NotNil(t, c)

	list := f.request(t, http.MethodGet, f.path("/transactions"), nil, false)
	list.AddCookie(c)
	rec = f.serveTx(t, list)
	toast := regexp.MustCompile(`(?s)<div id="toast"[^>]*>.*?</div>`).FindString(rec.Body.String())
	assert.Contains(t, toast, "Rp\u00a040.000")
	assert.NotContains(t, toast, `name="amount"`, "a saved toast offers no Undo")
}

func TestTransactionToastCookieStaysUnderTheCookieLimit(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	tx := f.recordTx(t, transaction.RecordInput{Amount: money.New(1000000, money.IDR), Note: strings.Repeat("ع", 500)})
	rec := f.serveTx(t, f.request(t, http.MethodPost, f.path("/transactions/"+tx.ID.String()+"/delete"), url.Values{}, false))
	c := f.toastCookie(t, rec)
	require.NotNil(t, c, "a long note must not cost the Undo")
	assert.LessOrEqual(t, len(c.Value), 3500, "browsers drop a cookie over 4KB")

	list := f.request(t, http.MethodGet, f.path("/transactions"), nil, false)
	list.AddCookie(c)
	rec = f.serveTx(t, list)
	assert.Contains(t, rec.Body.String(), `name="amount" value="10000"`, "Undo still offered, without the note")
}

func TestTransactionNewPage_PinsTheKeypadAboveTheDock(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	rec := f.serveTx(t, f.request(t, http.MethodGet, f.path("/transactions/new"), nil, false))

	require.Equal(t, http.StatusOK, rec.Code)
	panel := regexp.MustCompile(`<div class="[^"]*max-lg:sticky[^"]*">\s*<div data-keypad`).FindString(rec.Body.String())
	require.NotEmpty(t, panel, "the keypad panel sticks on phones on the full page too, not only in the sheet")
	assert.Contains(t, panel, "max-lg:bottom-16", "it sits above the dock, not under it")
}

func TestTransactionNew_HTMXReturnsOnlyTheSheet(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	rec := f.serveTx(t, f.request(t, http.MethodGet, f.path("/transactions/new?kind=income"), nil, true))

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.True(t, strings.HasPrefix(strings.TrimSpace(body), "<dialog"), "a page without the sheet loads just the dialog into its host")
	assert.Contains(t, body, `id="tx-add-dialog"`)
	assert.NotContains(t, body, "<html", "no layout around the fragment")
	assert.Regexp(t, `value="income"[^>]*checked`, body, "the opener's query still prefills the sheet")
}
