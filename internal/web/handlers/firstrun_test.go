package handlers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/category"
	"altalune.id/yasaku/internal/i18n"
	"altalune.id/yasaku/internal/ledger"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/transaction"
	"altalune.id/yasaku/internal/wallet"
	"altalune.id/yasaku/internal/web/handlers"
	"altalune.id/yasaku/money"
)

func newFirstRunFixture(t *testing.T) (*txFixture, *http.ServeMux) {
	t.Helper()
	return newFirstRunFixtureWith(t, txPassthroughUoW)
}

func newFirstRunFixtureWith(t *testing.T, uow wallet.UnitOfWork) (*txFixture, *http.ServeMux) {
	t.Helper()
	f := newTxFixture(t)
	octx := f.scoped(f.OrgID, f.ProjID, f.Principal.UserID)
	proj, err := f.Projects.Create(octx, f.OrgID, "fresh", "Fresh")
	require.NoError(t, err)
	f.ProjID, f.ProjSlug = proj.ID, proj.Slug
	f.Principal.ActiveProjectID = proj.ID
	open := wallet.NewOpenWorkflow(f.Wallets, f.Transactions, openingDater(f.Periods), uow,
		discardLogger(), passthroughUnexpected())
	mux := http.NewServeMux()
	handlers.NewFirstRunHandler(f.Deps, f.Projects, f.Wallets, open, f.TxCategories, f.Ledgers).Register(mux)
	return f, mux
}

func (f *txFixture) serve(t *testing.T, mux *http.ServeMux, method, sub string, form url.Values, hdr ...string) *httptest.ResponseRecorder {
	t.Helper()
	r := f.request(t, method, f.path(sub), form, false)
	for i := 0; i+1 < len(hdr); i += 2 {
		r.Header.Set(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, r)
	return rec
}

func (f *txFixture) configureCurrency(t *testing.T) {
	t.Helper()
	cur := money.IDR
	_, err := f.Ledgers.Update(f.projectCtx(t), ledger.Patch{Currency: &cur})
	require.NoError(t, err)
}

func TestFirstRun_CurrencyStep_PrefilledFromTheAppLocale(t *testing.T) {
	t.Parallel()
	f, mux := newFirstRunFixture(t)
	for loc, want := range map[i18n.Locale]string{i18n.MsMY: "MYR", i18n.IdID: "IDR", i18n.JaJP: "JPY"} {
		r := f.requestIn(t, loc, http.MethodGet, f.path("/first-run"), nil, false)
		r.Header.Set("Accept-Language", "en-US,en;q=0.9")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, r)
		require.Equal(t, http.StatusOK, rec.Code)
		assert.Regexp(t, `<option value="`+want+`" selected`, rec.Body.String(), "the page language %s wins over the browser header", loc)
		assert.NotContains(t, rec.Body.String(), `name="kind"`)
	}
}

func TestFirstRun_IsAFocusedPageWithoutTheAppShell(t *testing.T) {
	t.Parallel()
	f, mux := newFirstRunFixture(t)
	rec := f.serve(t, mux, http.MethodGet, "/first-run", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.NotContains(t, body, `id="alt-drawer"`, "first run shows no sidebar, like the prototype")
	assert.NotContains(t, body, `data-dialog-open="tx-add-dialog"`, "no dock or add button before a wallet exists")
	assert.Contains(t, body, `class="steps`)
}

func TestFirstRun_PostCurrency_SavesAndShowsWalletStep(t *testing.T) {
	t.Parallel()
	f, mux := newFirstRunFixture(t)
	rec := f.serve(t, mux, http.MethodPost, "/first-run", url.Values{"step": {"currency"}, "currency": {"USD"}})
	require.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, f.path("/first-run"), rec.Header().Get("Location"))
	st, err := f.Ledgers.Get(f.projectCtx(t))
	require.NoError(t, err)
	assert.Equal(t, money.Currency("USD"), st.Currency)
	assert.False(t, st.UpdatedAt.IsZero())

	rec = f.serve(t, mux, http.MethodGet, "/first-run", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `name="add"`)
}

func TestFirstRun_PostCurrency_UnknownCurrencyRerenders(t *testing.T) {
	t.Parallel()
	f, mux := newFirstRunFixture(t)
	rec := f.serve(t, mux, http.MethodPost, "/first-run", url.Values{"step": {"currency"}, "currency": {"XXX"}})
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	st, err := f.Ledgers.Get(f.projectCtx(t))
	require.NoError(t, err)
	assert.True(t, st.UpdatedAt.IsZero())
}

func TestFirstRun_AddChip_AddsRowWithAriaLabel(t *testing.T) {
	t.Parallel()
	f, mux := newFirstRunFixture(t)
	f.configureCurrency(t)
	rec := f.serve(t, mux, http.MethodPost, "/first-run", url.Values{"step": {"wallets"}, "add": {"ewallet"}})
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, `name="amount"`)
	assert.Contains(t, body, `aria-label=`)
	assert.Contains(t, body, `value="ewallet"`)
}

func TestFirstRun_NoWallets_Refused(t *testing.T) {
	t.Parallel()
	f, mux := newFirstRunFixture(t)
	f.configureCurrency(t)
	rec := f.serve(t, mux, http.MethodPost, "/first-run", url.Values{"step": {"wallets"}})
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	ws, err := f.Wallets.List(f.projectCtx(t), wallet.ListOpts{IncludeArchived: true})
	require.NoError(t, err)
	assert.Empty(t, ws)
}

func TestFirstRun_BlankNameAndBadAmount_RerenderWithoutWriting(t *testing.T) {
	t.Parallel()
	f, mux := newFirstRunFixture(t)
	f.configureCurrency(t)
	rec := f.serve(t, mux, http.MethodPost, "/first-run", url.Values{
		"step": {"wallets"}, "kind": {"cash", "bank"}, "name": {"", "BCA"}, "amount": {"1", "12+5"},
	})
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), `value="BCA"`)
	ws, err := f.Wallets.List(f.projectCtx(t), wallet.ListOpts{IncludeArchived: true})
	require.NoError(t, err)
	assert.Empty(t, ws, "validation runs before any wallet is created")
}

func TestFirstRun_Submit_CreatesWalletsSeedsCategoriesAndIsIdempotent(t *testing.T) {
	t.Parallel()
	f, mux := newFirstRunFixture(t)
	f.configureCurrency(t)
	form := url.Values{"step": {"wallets"}, "kind": {"cash", "bank"}, "name": {"Tunai", "BCA"}, "amount": {"150.000", "0"}}

	rec := f.serve(t, mux, http.MethodPost, "/first-run", form)
	require.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, f.path("/overview"), rec.Header().Get("Location"))
	first := f.firstRunCounts(t)
	assert.Equal(t, 2, first.wallets)
	assert.Equal(t, 1, first.openings)
	assert.Positive(t, first.categories)

	form.Set("amount", "999")
	form["amount"] = []string{"999", "999"}
	rec = f.serve(t, mux, http.MethodPost, "/first-run", form)
	require.Equal(t, http.StatusSeeOther, rec.Code, "a retry shows no error")
	assert.Equal(t, first, f.firstRunCounts(t), "a retry keeps the first set and the first balances")
}

func TestFirstRun_PostCurrencyWhenDone_RedirectsHomeWithoutWriting(t *testing.T) {
	t.Parallel()
	f, mux := newFirstRunFixture(t)
	f.configureCurrency(t)
	_, err := f.Wallets.Create(f.projectCtx(t), wallet.Params{Name: "Tunai", Kind: wallet.KindCash, Currency: money.IDR})
	require.NoError(t, err)

	rec := f.serve(t, mux, http.MethodPost, "/first-run", url.Values{"step": {"currency"}, "currency": {"USD"}})

	require.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, f.path("/overview"), rec.Header().Get("Location"))
	st, err := f.Ledgers.Get(f.projectCtx(t))
	require.NoError(t, err)
	assert.Equal(t, money.IDR, st.Currency, "settings owns the currency once setup is done")
}

type walletStoreDownError struct{}

func (*walletStoreDownError) Error() string { return "wallet store down" }

func TestFirstRun_SecondWalletFails_CategoriesStillSeededAndRetryGoesHome(t *testing.T) {
	t.Parallel()
	calls := 0
	f, mux := newFirstRunFixtureWith(t, func(ctx context.Context, fn func(context.Context) error) error {
		calls++
		if calls == 2 {
			return &walletStoreDownError{}
		}
		return fn(ctx)
	})
	f.configureCurrency(t)
	form := url.Values{"step": {"wallets"}, "kind": {"cash", "bank"}, "name": {"Tunai", "BCA"}, "amount": {"", ""}}

	rec := f.serve(t, mux, http.MethodPost, "/first-run", form)
	require.Equal(t, http.StatusInternalServerError, rec.Code)
	got := f.firstRunCounts(t)
	assert.Equal(t, 1, got.wallets)
	assert.Positive(t, got.categories, "a partial wallet failure still leaves the default categories")

	rec = f.serve(t, mux, http.MethodPost, "/first-run", form)
	require.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, f.path("/overview"), rec.Header().Get("Location"))
}

func TestFirstRun_GetWhenDone_RedirectsHome(t *testing.T) {
	t.Parallel()
	f, mux := newFirstRunFixture(t)
	f.configureCurrency(t)
	_, err := f.Wallets.Create(f.projectCtx(t), wallet.Params{Name: "Tunai", Kind: wallet.KindCash, Currency: money.IDR})
	require.NoError(t, err)
	rec := f.serve(t, mux, http.MethodGet, "/first-run", nil)
	require.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, f.path("/overview"), rec.Header().Get("Location"))
}

type firstRunCounts struct{ wallets, openings, categories int }

func (f *txFixture) firstRunCounts(t *testing.T) firstRunCounts {
	t.Helper()
	ctx := f.projectCtx(t)
	ws, err := f.Wallets.List(ctx, wallet.ListOpts{IncludeArchived: true})
	require.NoError(t, err)
	txs, _, err := f.Transactions.List(ctx, transaction.ListOpts{Kinds: []transaction.Kind{transaction.KindOpening}})
	require.NoError(t, err)
	cats, err := f.TxCategories.List(ctx, category.ListOpts{IncludeArchived: true})
	require.NoError(t, err)
	return firstRunCounts{len(ws), len(txs), len(cats)}
}

func TestFirstRun_Member_CannotSetCurrency(t *testing.T) {
	t.Parallel()
	f, mux := newFirstRunFixture(t)
	member := uuid.New()
	_, err := f.Orgs.AddMember(f.scoped(f.OrgID, uuid.Nil, f.Principal.UserID), f.OrgID, member, org.RoleMember)
	require.NoError(t, err)
	f.Principal.UserID = member

	rec := f.serve(t, mux, http.MethodGet, "/first-run", nil)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	rec = f.serve(t, mux, http.MethodPost, "/first-run", url.Values{"step": {"currency"}, "currency": {"USD"}})
	assert.Equal(t, http.StatusForbidden, rec.Code)
	st, err := f.Ledgers.Get(f.projectCtx(t))
	require.NoError(t, err)
	assert.True(t, st.UpdatedAt.IsZero(), "a member's POST writes nothing")
}

func TestFirstRun_DuplicateNames_RefusedOnTheDuplicateRow(t *testing.T) {
	t.Parallel()
	f, mux := newFirstRunFixture(t)
	f.configureCurrency(t)
	rec := f.serve(t, mux, http.MethodPost, "/first-run", url.Values{
		"step": {"wallets"}, "kind": {"cash", "bank"}, "name": {"Tunai", " tunai "}, "amount": {"", ""},
	})
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Equal(t, 1, strings.Count(rec.Body.String(), `aria-invalid="true"`), "only the second row is flagged")
	ws, err := f.Wallets.List(f.projectCtx(t), wallet.ListOpts{IncludeArchived: true})
	require.NoError(t, err)
	assert.Empty(t, ws)
}

func TestFirstRun_StaleResubmit_RedirectsHomeWithoutWriting(t *testing.T) {
	t.Parallel()
	f, mux := newFirstRunFixture(t)
	f.configureCurrency(t)
	form := url.Values{"step": {"wallets"}, "kind": {"cash"}, "name": {"Tunai"}, "amount": {"100"}}
	require.Equal(t, http.StatusSeeOther, f.serve(t, mux, http.MethodPost, "/first-run", form).Code)
	ws, err := f.Wallets.List(f.projectCtx(t), wallet.ListOpts{})
	require.NoError(t, err)
	_, err = f.Wallets.Rename(f.projectCtx(t), ws[0].ID, "Dompet")
	require.NoError(t, err)
	before := f.firstRunCounts(t)

	rec := f.serve(t, mux, http.MethodPost, "/first-run", form)

	require.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, f.path("/overview"), rec.Header().Get("Location"))
	assert.Equal(t, before, f.firstRunCounts(t), "a stale resubmit creates and seeds nothing")
}

func TestFirstRun_ConcurrentDoubleSubmit_CreatesOneSetWithoutError(t *testing.T) {
	t.Parallel()
	f, mux := newFirstRunFixture(t)
	f.configureCurrency(t)
	form := url.Values{"step": {"wallets"}, "kind": {"cash", "bank"}, "name": {"Tunai", "BCA"}, "amount": {"150.000", "0"}}
	const n = 8
	reqs := make([]*http.Request, n)
	recs := make([]*httptest.ResponseRecorder, n)
	for i := range reqs {
		reqs[i] = f.request(t, http.MethodPost, f.path("/first-run"), form, false)
		recs[i] = httptest.NewRecorder()
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range reqs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			mux.ServeHTTP(recs[i], reqs[i])
		}()
	}
	close(start)
	wg.Wait()

	for _, rec := range recs {
		assert.Equal(t, http.StatusSeeOther, rec.Code, rec.Body.String())
	}
	got := f.firstRunCounts(t)
	assert.Equal(t, 2, got.wallets)
	assert.Equal(t, 1, got.openings)
}

func TestFirstRun_UnknownCurrency_ShowsTranslatedMessage(t *testing.T) {
	t.Parallel()
	f, mux := newFirstRunFixture(t)
	rec := f.serve(t, mux, http.MethodPost, "/first-run", url.Values{"step": {"currency"}, "currency": {"XXX"}})
	tr := f.Bundle.For(i18n.IdID)
	assert.Contains(t, rec.Body.String(), tr.T("firstrun.currency_invalid"))
	assert.NotContains(t, rec.Body.String(), "not a supported currency")
}

func TestFirstRun_BalancePrefix_ShowsSymbol(t *testing.T) {
	t.Parallel()
	f, mux := newFirstRunFixture(t)
	f.configureCurrency(t)
	rec := f.serve(t, mux, http.MethodPost, "/first-run", url.Values{"step": {"wallets"}, "add": {"cash"}})
	assert.Contains(t, rec.Body.String(), ">Rp<")
}
