package handlers_test

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/category"
	"altalune.id/yasaku/internal/transaction"
	"altalune.id/yasaku/internal/wallet"
	"altalune.id/yasaku/internal/web/handlers"
	"altalune.id/yasaku/money"
)

func (f *walletFixture) txCategoryMux(t *testing.T) *http.ServeMux {
	t.Helper()
	h := handlers.NewTxCategoryHandler(f.Deps, f.Projects, f.TxCategories)
	mux := http.NewServeMux()
	h.Register(mux)
	return mux
}

func TestTxCategory_Seed_InsertsTwentyThenNone(t *testing.T) {
	t.Parallel()
	f := newWalletFixture(t)
	mux := f.txCategoryMux(t)

	first := f.do(t, mux, http.MethodPost, "/categories/seed", url.Values{})
	require.Equal(t, http.StatusOK, first.Code, first.Body.String())
	assert.Contains(t, first.Body.String(), `data-seeded="20"`)

	second := f.do(t, mux, http.MethodPost, "/categories/seed", url.Values{})
	require.Equal(t, http.StatusOK, second.Code)
	assert.Contains(t, second.Body.String(), `data-seeded="0"`)

	rows, err := f.TxCategories.List(f.scoped(t), category.ListOpts{})
	require.NoError(t, err)
	assert.Len(t, rows, 20)
}

func TestTxCategory_Page_ShowsBothColumnsAndEmptyStateSeedAction(t *testing.T) {
	t.Parallel()
	f := newWalletFixture(t)
	mux := f.txCategoryMux(t)

	empty := f.do(t, mux, http.MethodGet, "/categories", nil)
	require.Equal(t, http.StatusOK, empty.Code)
	assert.Contains(t, empty.Body.String(), f.path("/categories/seed"))

	require.Equal(t, http.StatusOK, f.do(t, mux, http.MethodPost, "/categories/seed", url.Values{}).Code)

	page := f.do(t, mux, http.MethodGet, "/categories", nil)
	require.Equal(t, http.StatusOK, page.Code)
	body := page.Body.String()
	assert.Contains(t, body, `id="tx-category-expense"`)
	assert.Contains(t, body, `id="tx-category-income"`)
	assert.Contains(t, body, "Makan &amp; Minum")
	assert.Contains(t, body, "Gaji")
	assert.NotContains(t, body, f.path("/categories/seed"))
}

func TestTxCategory_CreateAndRename(t *testing.T) {
	t.Parallel()
	f := newWalletFixture(t)
	mux := f.txCategoryMux(t)

	rec := f.do(t, mux, http.MethodPost, "/categories", url.Values{
		"name": {"Kopi"}, "kind": {"expense"},
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "Kopi")

	c := f.onlyCategory(t)
	renamed := f.do(t, mux, http.MethodPost, "/categories/"+c.ID.String()+"/rename", url.Values{"name": {"Kopi Susu"}})
	require.Equal(t, http.StatusOK, renamed.Code)
	assert.Contains(t, renamed.Body.String(), "Kopi Susu")

	after, err := f.TxCategories.ByID(f.scoped(t), c.ID)
	require.NoError(t, err)
	assert.Equal(t, "Kopi Susu", after.Name)
}

func TestTxCategory_CreateWithBlankName_ShowsBanner(t *testing.T) {
	t.Parallel()
	f := newWalletFixture(t)
	mux := f.txCategoryMux(t)

	rec := f.do(t, mux, http.MethodPost, "/categories", url.Values{"name": {"  "}, "kind": {"expense"}})
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "CTG002")

	rows, err := f.TxCategories.List(f.scoped(t), category.ListOpts{IncludeArchived: true})
	require.NoError(t, err)
	assert.Empty(t, rows)
}

func TestTxCategory_ArchiveCollapsesTheRowAndUnarchiveRestoresIt(t *testing.T) {
	t.Parallel()
	f := newWalletFixture(t)
	mux := f.txCategoryMux(t)

	require.Equal(t, http.StatusOK, f.do(t, mux, http.MethodPost, "/categories", url.Values{
		"name": {"Kopi"}, "kind": {"expense"},
	}).Code)
	c := f.onlyCategory(t)

	rec := f.do(t, mux, http.MethodPost, "/categories/"+c.ID.String()+"/archive", url.Values{})
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, `id="tx-category-archived"`)
	assert.False(t, containsBefore(body, "Kopi", `id="tx-category-archived"`))

	active, err := f.TxCategories.List(f.scoped(t), category.ListOpts{})
	require.NoError(t, err)
	assert.Empty(t, active)

	back := f.do(t, mux, http.MethodPost, "/categories/"+c.ID.String()+"/unarchive", url.Values{})
	require.Equal(t, http.StatusOK, back.Code)
	assert.True(t, containsBefore(back.Body.String(), "Kopi", `id="tx-category-archived"`))
}

func TestTxCategory_DeleteInUse_ShowsBannerAndKeepsIt(t *testing.T) {
	t.Parallel()
	f := newWalletFixture(t)
	mux := f.txCategoryMux(t)

	require.Equal(t, http.StatusOK, f.do(t, mux, http.MethodPost, "/categories", url.Values{
		"name": {"Kopi"}, "kind": {"expense"},
	}).Code)
	c := f.onlyCategory(t)

	ctx := f.scoped(t)
	w, err := f.Wallets.Create(ctx, wallet.Params{Name: "Dompet", Kind: wallet.KindCash, Currency: money.IDR})
	require.NoError(t, err)
	_, err = f.Transactions.Record(ctx, transaction.RecordInput{
		WalletID:   w.ID,
		Kind:       transaction.KindExpense,
		Amount:     money.New(2500000, money.IDR),
		CategoryID: &c.ID,
		OccurredAt: time.Now().UTC(),
	})
	require.NoError(t, err)

	rec := f.do(t, mux, http.MethodPost, "/categories/"+c.ID.String()+"/delete", url.Values{})
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "CTG005")
	assert.Contains(t, rec.Body.String(), "Kopi")

	rows, err := f.TxCategories.List(f.scoped(t), category.ListOpts{IncludeArchived: true})
	require.NoError(t, err)
	assert.Len(t, rows, 1)
}

func TestTxCategory_DeleteUnused_RemovesIt(t *testing.T) {
	t.Parallel()
	f := newWalletFixture(t)
	mux := f.txCategoryMux(t)

	require.Equal(t, http.StatusOK, f.do(t, mux, http.MethodPost, "/categories", url.Values{
		"name": {"Kopi"}, "kind": {"expense"},
	}).Code)
	c := f.onlyCategory(t)

	rec := f.do(t, mux, http.MethodPost, "/categories/"+c.ID.String()+"/delete", url.Values{})
	require.Equal(t, http.StatusOK, rec.Code)

	rows, err := f.TxCategories.List(f.scoped(t), category.ListOpts{IncludeArchived: true})
	require.NoError(t, err)
	assert.Empty(t, rows)
}

func TestTxCategory_UnknownID_Is404(t *testing.T) {
	t.Parallel()
	f := newWalletFixture(t)
	mux := f.txCategoryMux(t)

	rec := f.do(t, mux, http.MethodPost, "/categories/"+uuid.NewString()+"/rename", url.Values{"name": {"x"}})
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// TestTxCategory_FragmentsCarryTheOrgScopedActionURLs guards the HTMX trap: a fragment rendered on a
// bare Base has a nil ActiveOrg, so every action URL collapses to /orgs.
func TestTxCategory_FragmentsCarryTheOrgScopedActionURLs(t *testing.T) {
	t.Parallel()
	f := newWalletFixture(t)
	mux := f.txCategoryMux(t)

	rec := f.do(t, mux, http.MethodPost, "/categories", url.Values{"name": {"Kopi"}, "kind": {"expense"}})
	require.Equal(t, http.StatusOK, rec.Code)
	c := f.onlyCategory(t)
	assert.Contains(t, rec.Body.String(), f.path("/categories/"+c.ID.String()+"/rename"))
}

func (f *walletFixture) onlyCategory(t *testing.T) *category.Category {
	t.Helper()
	rows, err := f.TxCategories.List(f.scoped(t), category.ListOpts{IncludeArchived: true})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	return rows[0]
}
