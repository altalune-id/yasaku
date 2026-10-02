package handlers_test

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/report"
	"altalune.id/yasaku/internal/wallet"
	"altalune.id/yasaku/internal/web/handlers"
	"altalune.id/yasaku/money"
)

var (
	walletsTotalRe      = regexp.MustCompile(`text-3xl[^>]*>([^<]+)<`)
	overviewSpendableRe = regexp.MustCompile(`text-4xl[^>]*>([^<]+)<`)
	overviewTotalRe     = regexp.MustCompile(`ms-1 tabular-nums[^>]*>([^<]+)<`)
)

func (f *walletFixture) overviewMux(t *testing.T) *http.ServeMux {
	t.Helper()
	pool := db.Pool{W: f.DB, R: f.DB}
	dbCfg := db.DBConfig{Driver: db.DriverSQLite, TablePrefix: f.Cfg.DB.TablePrefix}
	reports := report.NewService(report.NewReader(dbCfg, pool, nil), discardLogger(), passthroughUnexpected(), f.Ledgers)
	h := handlers.NewOverviewHandler(f.Deps, f.Projects, f.Wallets, f.Transactions,
		f.Periods, reports, f.TxCategories, f.Ledgers)
	mux := http.NewServeMux()
	h.Register(mux)
	return mux
}

func (f *walletFixture) openWallet(t *testing.T, name string, kind wallet.Kind, exclude bool, minor int64) *wallet.Wallet {
	t.Helper()
	opening := money.New(minor, money.IDR)
	w, _, err := f.WalletOpen.Run(f.scoped(t), wallet.Params{
		Name: name, Kind: kind, Currency: money.IDR, ExcludeFromTotal: exclude,
	}, &opening, time.Now())
	require.NoError(t, err)
	return w
}

func firstMatch(t *testing.T, re *regexp.Regexp, body string) string {
	t.Helper()
	m := re.FindStringSubmatch(body)
	require.Len(t, m, 2, "no match for %s", re)
	return m[1]
}

func TestOverview_TotalsMatchTheWalletsPage(t *testing.T) {
	t.Parallel()
	f := newWalletFixture(t)
	f.openWallet(t, "Tunai", wallet.KindCash, false, 1_000_000_00)
	f.openWallet(t, "Tabungan", wallet.KindSavings, true, 300_000_00)
	bank := f.openWallet(t, "Bank Lama", wallet.KindBank, false, 12_875_000_00)
	_, err := f.Wallets.Archive(f.scoped(t), bank.ID)
	require.NoError(t, err)

	wallets := f.do(t, f.walletMux(t), http.MethodGet, "/wallets", nil)
	require.Equal(t, http.StatusOK, wallets.Code)
	overview := f.do(t, f.overviewMux(t), http.MethodGet, "/overview", nil)
	require.Equal(t, http.StatusOK, overview.Code)

	walletsTotal := firstMatch(t, walletsTotalRe, wallets.Body.String())
	body := overview.Body.String()
	assert.Equal(t, "Rp1.000.000", walletsTotal)
	assert.Equal(t, walletsTotal, firstMatch(t, overviewSpendableRe, body),
		"the overview's spendable must equal the wallets page total")
	assert.Equal(t, "Rp1.300.000", firstMatch(t, overviewTotalRe, body),
		"the total keeps the excluded wallet and drops the archived one")
	cards := overviewWalletCards(t, f, body)
	assert.NotContains(t, cards, "Bank Lama", "an archived wallet has no card on the overview")
	assert.Contains(t, cards, "Tabungan")
	assert.Contains(t, cards, "Tunai")
}

func overviewWalletCards(t *testing.T, f *walletFixture, body string) string {
	t.Helper()
	start := strings.Index(body, `href="`+f.path("/wallets")+`"`)
	require.Positive(t, start, "the overview has no wallets card")
	end := strings.Index(body[start:], "</ul>")
	require.Positive(t, end)
	return body[start : start+end]
}
