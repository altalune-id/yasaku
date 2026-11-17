package handlers_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/i18n"
)

func (f *walletFixture) tr(key string) string {
	return f.Deps.I18n.For(i18n.IdID).T(key)
}

func (f *walletFixture) walletWithOpening(t *testing.T, mux *http.ServeMux, name, opening string) {
	t.Helper()
	require.Equal(t, http.StatusSeeOther, f.do(t, mux, http.MethodPost, "/wallets", url.Values{
		"name": {name}, "kind": {"ewallet"}, "opening_balance": {opening},
	}).Code)
}

func TestWallet_AdjustPreview_NamesTheSignedDifferenceFromTheServer(t *testing.T) {
	t.Parallel()
	f := newWalletFixture(t)
	mux := f.walletMux(t)
	f.walletWithOpening(t, mux, "GoPay", "100000")
	w := f.onlyWallet(t)
	base := "/wallets/" + w.ID.String() + "/adjust/preview?target="

	up := f.do(t, mux, http.MethodGet, base+"112500", nil)
	require.Equal(t, http.StatusOK, up.Code, up.Body.String())
	assert.Contains(t, up.Body.String(), `data-adjust-diff="1250000"`)
	assert.Contains(t, up.Body.String(), "Rp\u00a012.500")
	assert.NotContains(t, up.Body.String(), "<html")

	down := f.do(t, mux, http.MethodGet, base+"90000", nil)
	assert.Contains(t, down.Body.String(), `data-adjust-diff="-1000000"`)

	same := f.do(t, mux, http.MethodGet, base+"100000", nil)
	assert.Contains(t, same.Body.String(), `data-adjust-diff="0"`)

	empty := f.do(t, mux, http.MethodGet, base, nil)
	require.Equal(t, http.StatusOK, empty.Code)
	assert.NotContains(t, empty.Body.String(), "data-adjust-diff")

	bad := f.do(t, mux, http.MethodGet, base+"12%2B5", nil)
	require.Equal(t, http.StatusOK, bad.Code)
	assert.Contains(t, bad.Body.String(), `data-adjust-error="1"`)
	assert.NotContains(t, bad.Body.String(), "data-adjust-diff")
}

func TestWallet_AdjustPreview_WritesNothing(t *testing.T) {
	t.Parallel()
	f := newWalletFixture(t)
	mux := f.walletMux(t)
	f.walletWithOpening(t, mux, "GoPay", "100000")
	w := f.onlyWallet(t)

	require.Equal(t, http.StatusOK, f.do(t, mux, http.MethodGet, "/wallets/"+w.ID.String()+"/adjust/preview?target=1", nil).Code)
	detail := f.do(t, mux, http.MethodGet, "/wallets/"+w.ID.String(), nil)
	assert.Contains(t, detail.Body.String(), "Rp\u00a0100.000")
}

func TestWallet_AdjustPage_WiresThePreviewThroughHTMX(t *testing.T) {
	t.Parallel()
	f := newWalletFixture(t)
	mux := f.walletMux(t)
	f.walletWithOpening(t, mux, "GoPay", "100000")
	w := f.onlyWallet(t)

	body := f.do(t, mux, http.MethodGet, "/wallets/"+w.ID.String()+"/adjust", nil).Body.String()
	assert.Contains(t, body, `hx-get="`+f.path("/wallets/"+w.ID.String()+"/adjust/preview")+`"`)
	assert.Contains(t, body, `aria-live="polite"`)
	assert.Contains(t, body, f.tr("wallet.adjust"))
}

func TestWallet_List_DeleteOnlyBehindArchiveWithConfirmInTheDialogHost(t *testing.T) {
	t.Parallel()
	f := newWalletFixture(t)
	mux := f.walletMux(t)
	f.walletWithOpening(t, mux, "GoPay", "0")
	w := f.onlyWallet(t)
	deletePath := f.path("/wallets/" + w.ID.String() + "/delete")
	dialogID := `data-dialog-open="wallet-delete-` + w.ID.String() + `"`

	active := f.do(t, mux, http.MethodGet, "/wallets", nil).Body.String()
	assert.NotContains(t, active, deletePath, "an active wallet offers no delete")
	assert.NotContains(t, active, dialogID)

	plain := f.do(t, mux, http.MethodPost, "/wallets/"+w.ID.String()+"/archive", url.Values{}).Body.String()
	assert.NotContains(t, plain, "hx-swap-oob", "a non-htmx post must not carry out-of-band parts")
	f.do(t, mux, http.MethodPost, "/wallets/"+w.ID.String()+"/unarchive", url.Values{})
	archived := f.doHTMX(t, mux, http.MethodPost, "/wallets/"+w.ID.String()+"/archive", url.Values{}).Body.String()
	assert.Contains(t, archived, dialogID)
	assert.Contains(t, archived, `id="wallet-dialogs" hx-swap-oob="true"`, "a swap must hand the new dialog to the host")
	assert.Contains(t, archived, deletePath)

	page := f.do(t, mux, http.MethodGet, "/wallets", nil).Body.String()
	host := page[strings.Index(page, `id="dialog-host"`):]
	assert.Contains(t, host, `<dialog id="wallet-delete-`+w.ID.String()+`"`, "the confirm lives in the body-level host")
	assert.NotContains(t, page[:strings.Index(page, `id="dialog-host"`)], "<dialog id=\"wallet-delete-")
}

func TestWallet_List_TotalIsAStatsBlockWithKindAvatars(t *testing.T) {
	t.Parallel()
	f := newWalletFixture(t)
	mux := f.walletMux(t)
	f.walletWithOpening(t, mux, "GoPay", "0")
	f.walletWithOpening(t, mux, "BCA", "0")

	body := f.do(t, mux, http.MethodGet, "/wallets", nil).Body.String()
	assert.Contains(t, body, `class="stats w-full bg-base-200"`)
	assert.Contains(t, body, "Total dari 2 dompet")
	assert.Contains(t, body, `rounded-full bg-base-200 text-primary`)
}

func TestWallet_List_RowsKeepTheirNamesAndAmountsWhole(t *testing.T) {
	t.Parallel()
	f := newWalletFixture(t)
	mux := f.walletMux(t)
	f.walletWithOpening(t, mux, "Tabungan darurat keluarga besar", "100000")
	w := f.onlyWallet(t)

	page := f.do(t, mux, http.MethodGet, "/wallets", nil).Body.String()
	body := page[strings.Index(page, `id="wallet-list"`):]
	body = body[:strings.Index(body, "</ul>")]
	assert.NotContains(t, body, "truncate")
	assert.Contains(t, body, `aria-label="`+f.tr("wallet.adjust")+`: Tabungan darurat keluarga besar"`)
	assert.Contains(t, body, f.path("/wallets/"+w.ID.String()+"/adjust"))
	assert.Contains(t, body, "min-w-0")
	assert.Contains(t, body, "break-words")
}

func TestWallet_Page_LinksTransferToTheAddSheet(t *testing.T) {
	t.Parallel()
	f := newWalletFixture(t)
	mux := f.walletMux(t)
	f.walletWithOpening(t, mux, "GoPay", "0")
	f.walletWithOpening(t, mux, "BCA", "0")
	w := f.walletNamed(t, "GoPay")

	list := f.do(t, mux, http.MethodGet, "/wallets", nil).Body.String()
	assert.Contains(t, list, f.path("/transactions/new?kind=transfer"))

	detail := f.do(t, mux, http.MethodGet, "/wallets/"+w.ID.String(), nil).Body.String()
	assert.Contains(t, detail, strings.ReplaceAll(f.path("/transactions/new?kind=transfer&wallet="+w.ID.String()), "&", "&amp;"))
}

func TestWallet_Form_CallsTheOpeningBalanceTheStartingBalance(t *testing.T) {
	t.Parallel()
	f := newWalletFixture(t)
	mux := f.walletMux(t)

	body := f.do(t, mux, http.MethodGet, "/wallets/new", nil).Body.String()
	assert.Contains(t, body, f.tr("wallet.opening_balance"))
	assert.Contains(t, body, `inputmode="decimal"`)
}

func TestWallet_AdjustOpensAsADialogFromTheList(t *testing.T) {
	t.Parallel()
	f := newWalletFixture(t)
	mux := f.walletMux(t)
	f.walletWithOpening(t, mux, "BCA", "100000")
	id := f.onlyWallet(t).ID.String()

	list := f.do(t, mux, http.MethodGet, "/wallets", nil).Body.String()
	assert.Regexp(t, `<a[^>]*href="`+f.path("/wallets/"+id+"/adjust")+`"[^>]*data-dialog-open="wallet-adjust-`+id+`"[^>]*data-dialog-src="`+f.path("/wallets/"+id+"/adjust")+`"`, list)
	assert.NotContains(t, list, f.path("/wallets/"+id+"/archive"), "archive moved to the wallet page, as in the prototype list")

	dialog := f.doHTMX(t, mux, http.MethodGet, "/wallets/"+id+"/adjust", nil).Body.String()
	assert.True(t, strings.HasPrefix(strings.TrimSpace(dialog), "<dialog"), "an HTMX open loads only the dialog")
	assert.Contains(t, dialog, `id="wallet-adjust-`+id+`"`)
	assert.Contains(t, dialog, `action="`+f.path("/wallets/"+id+"/adjust")+`"`)

	page := f.do(t, mux, http.MethodGet, "/wallets/"+id+"/adjust", nil).Body.String()
	assert.Contains(t, page, "<html", "without JS the adjust page stays a full page")

	detail := f.do(t, mux, http.MethodGet, "/wallets/"+id, nil).Body.String()
	assert.Regexp(t, `<form[^>]*action="`+f.path("/wallets/"+id+"/archive")+`"`, detail)
}

func TestWallet_ArchiveWithoutJSRedirectsToTheListPage(t *testing.T) {
	t.Parallel()
	f := newWalletFixture(t)
	mux := f.walletMux(t)
	f.walletWithOpening(t, mux, "BCA", "100000")
	id := f.onlyWallet(t).ID.String()

	rec := f.do(t, mux, http.MethodPost, "/wallets/"+id+"/archive", url.Values{})
	require.Equal(t, http.StatusSeeOther, rec.Code, "a plain form post from the wallet page lands on the full list page, not a bare fragment")
	assert.Equal(t, f.path("/wallets"), rec.Header().Get("Location"))
	assert.Contains(t, f.do(t, mux, http.MethodGet, "/wallets", nil).Body.String(), `id="wallet-archived"`)

	hx := f.doHTMX(t, mux, http.MethodPost, "/wallets/"+id+"/unarchive", url.Values{})
	require.Equal(t, http.StatusOK, hx.Code)
	assert.True(t, strings.HasPrefix(strings.TrimSpace(hx.Body.String()), `<div id="wallet-list"`), "HTMX still swaps the list")
}
