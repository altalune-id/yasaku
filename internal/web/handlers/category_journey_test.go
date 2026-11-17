package handlers_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/category"
	"altalune.id/yasaku/internal/transaction"
	"altalune.id/yasaku/internal/wallet"
	"altalune.id/yasaku/money"
)

func TestTxCategory_Tabs_ShowOneKindAtATime(t *testing.T) {
	t.Parallel()
	f := newWalletFixture(t)
	mux := f.txCategoryMux(t)
	require.Equal(t, http.StatusOK, f.do(t, mux, http.MethodPost, "/categories/seed", url.Values{}).Code)

	exp := f.do(t, mux, http.MethodGet, "/categories", nil).Body.String()
	assert.Contains(t, exp, `role="tablist"`)
	assert.Contains(t, exp, `id="tx-category-expense"`)
	assert.NotContains(t, exp, `id="tx-category-income"`)
	assert.Contains(t, exp, "Makan &amp; Minum")
	assert.NotContains(t, exp, "Gaji")
	assert.Contains(t, exp, `aria-selected="true"`)

	inc := f.do(t, mux, http.MethodGet, "/categories?kind=income", nil).Body.String()
	assert.Contains(t, inc, `id="tx-category-income"`)
	assert.Contains(t, inc, "Gaji")
	assert.NotContains(t, inc, "Makan &amp; Minum")
}

func TestTxCategory_Tabs_UnknownKindFallsBackToExpense(t *testing.T) {
	t.Parallel()
	f := newWalletFixture(t)
	mux := f.txCategoryMux(t)
	require.Equal(t, http.StatusOK, f.do(t, mux, http.MethodPost, "/categories/seed", url.Values{}).Code)

	body := f.do(t, mux, http.MethodGet, "/categories?kind=bogus", nil).Body.String()
	assert.Contains(t, body, `id="tx-category-expense"`)
}

func TestTxCategory_ActionOnAnIncomeCategoryKeepsTheIncomeTab(t *testing.T) {
	t.Parallel()
	f := newWalletFixture(t)
	mux := f.txCategoryMux(t)
	require.Equal(t, http.StatusOK, f.do(t, mux, http.MethodPost, "/categories", url.Values{"name": {"Gaji"}, "kind": {"income"}}).Code)
	c := f.onlyCategory(t)

	rec := f.do(t, mux, http.MethodPost, "/categories/"+c.ID.String()+"/rename", url.Values{"name": {"Gaji pokok"}})
	assert.Contains(t, rec.Body.String(), `id="tx-category-income"`)
	assert.Contains(t, rec.Body.String(), "Gaji pokok")
}

func TestTxCategory_PostIcon_ChangesOnlyTheIconAndRefusesUnknownOnes(t *testing.T) {
	t.Parallel()
	f := newWalletFixture(t)
	mux := f.txCategoryMux(t)
	_, err := f.TxCategories.Create(f.scoped(t), "Kopi", category.KindExpense, "utensils", "chart-2")
	require.NoError(t, err)
	c := f.onlyCategory(t)

	rec := f.do(t, mux, http.MethodPost, "/categories/"+c.ID.String()+"/icon", url.Values{"icon": {"bus"}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	got, err := f.TxCategories.ByID(f.scoped(t), c.ID)
	require.NoError(t, err)
	assert.Equal(t, "bus", got.Icon)
	assert.Equal(t, "chart-2", got.Color)

	bad := f.do(t, mux, http.MethodPost, "/categories/"+c.ID.String()+"/icon", url.Values{"icon": {"<script>"}})
	require.Equal(t, http.StatusOK, bad.Code)
	assert.Contains(t, bad.Body.String(), "CTG")
	got, err = f.TxCategories.ByID(f.scoped(t), c.ID)
	require.NoError(t, err)
	assert.Equal(t, "bus", got.Icon)
}

func TestTxCategory_IconPicker_UsesTheKitDropdownNotAdHocDetails(t *testing.T) {
	t.Parallel()
	f := newWalletFixture(t)
	mux := f.txCategoryMux(t)
	_, err := f.TxCategories.Create(f.scoped(t), "Kopi", category.KindExpense, "utensils", "")
	require.NoError(t, err)
	c := f.onlyCategory(t)

	body := f.do(t, mux, http.MethodGet, "/categories", nil).Body.String()
	assert.Contains(t, body, `class="dropdown`)
	assert.NotContains(t, body, "data-dd")
	assert.Contains(t, body, f.path("/categories/"+c.ID.String()+"/icon"))
	for _, icon := range category.AllowedIcons {
		assert.Contains(t, body, `value="`+icon+`"`)
	}
}

func TestTxCategory_Page_ShowsEntriesThisPeriodFromTheReportSlice(t *testing.T) {
	f := newPeriodFixture(t, defaultCaps())
	ctx := f.scoped(t)
	kopi, err := f.Cats.Create(ctx, "Kopi", category.KindExpense, "", "")
	require.NoError(t, err)
	_, err = f.Cats.Create(ctx, "Sepi", category.KindExpense, "", "")
	require.NoError(t, err)
	w, err := f.Wallets.Create(ctx, wallet.Params{Name: "BCA", Kind: wallet.KindBank, Currency: money.IDR})
	require.NoError(t, err)
	for range 3 {
		_, err = f.Txs.Record(ctx, transaction.RecordInput{
			WalletID: w.ID, CategoryID: &kopi.ID, Kind: transaction.KindExpense,
			Amount: money.New(10_000, money.IDR), OccurredAt: time.Now(),
		})
		require.NoError(t, err)
	}

	body := f.do(t, http.MethodGet, f.path("/categories"), nil).Body.String()
	assert.Contains(t, body, `data-entries="3"`)
	assert.Contains(t, body, `data-entries="0"`)
}

func TestTxCategory_CreateKindFollowsTheTabOnAFullPage(t *testing.T) {
	t.Parallel()
	f := newWalletFixture(t)
	mux := f.txCategoryMux(t)
	require.Equal(t, http.StatusOK, f.do(t, mux, http.MethodPost, "/categories/seed", url.Values{}).Code)

	income := f.do(t, mux, http.MethodGet, "/categories?kind=income", nil).Body.String()
	assert.Regexp(t, `<input id="tx-category-kind" type="hidden" name="kind" form="tx-category-create" value="income"`, income)
	expense := f.do(t, mux, http.MethodGet, "/categories", nil).Body.String()
	assert.Regexp(t, `<input id="tx-category-kind" type="hidden" name="kind" form="tx-category-create" value="expense"`, expense)
}

func TestTxCategory_TabSwitchSwapsTheCreateKindOutOfBand(t *testing.T) {
	t.Parallel()
	f := newWalletFixture(t)
	mux := f.txCategoryMux(t)
	require.Equal(t, http.StatusOK, f.do(t, mux, http.MethodPost, "/categories/seed", url.Values{}).Code)

	tab := f.doHTMX(t, mux, http.MethodGet, "/categories?kind=income", nil).Body.String()
	assert.Contains(t, tab, `id="tx-category-kind"`)
	assert.Contains(t, tab, `hx-swap-oob="true"`)
	assert.Contains(t, tab, `value="income"`)

	action := f.doHTMX(t, mux, http.MethodPost, "/categories", url.Values{"name": {"Bonus tahunan"}, "kind": {"income"}}).Body.String()
	assert.Regexp(t, `id="tx-category-kind"[^>]*value="income"`, action, "the create kind follows the list after an action too")
}

func TestTxCategory_IconButtonsCarryTranslatedNamesNotSlugs(t *testing.T) {
	t.Parallel()
	f := newWalletFixture(t)
	mux := f.txCategoryMux(t)
	_, err := f.TxCategories.Create(f.scoped(t), "Kopi", category.KindExpense, "utensils", "")
	require.NoError(t, err)

	body := f.do(t, mux, http.MethodGet, "/categories", nil).Body.String()
	assert.Contains(t, body, `aria-label="`+f.tr("category.icon.utensils")+`"`)
	assert.NotContains(t, body, `aria-label="utensils"`)
	assert.NotEqual(t, "category.icon.utensils", f.tr("category.icon.utensils"), "the key must resolve")
}

func TestTxCategory_ArchivedRowsShowAPlainIconNotThePicker(t *testing.T) {
	t.Parallel()
	f := newWalletFixture(t)
	mux := f.txCategoryMux(t)
	c, err := f.TxCategories.Create(f.scoped(t), "Kopi", category.KindExpense, "utensils", "")
	require.NoError(t, err)

	body := f.doHTMX(t, mux, http.MethodPost, "/categories/"+c.ID.String()+"/archive", url.Values{}).Body.String()
	archived := body[strings.Index(body, `id="tx-category-archived"`):]
	assert.NotContains(t, archived, `class="dropdown`)
	assert.NotContains(t, archived, "/icon")
}

func TestTxCategory_NonHTMXPostRendersNoStraySelect(t *testing.T) {
	t.Parallel()
	f := newWalletFixture(t)
	mux := f.txCategoryMux(t)

	plain := f.do(t, mux, http.MethodPost, "/categories", url.Values{"name": {"Kopi"}, "kind": {"expense"}}).Body.String()
	assert.NotContains(t, plain, `id="tx-category-kind"`)
	assert.NotContains(t, plain, "hx-swap-oob=\"true\" hx-nonce", "no out-of-band select without htmx")
}
