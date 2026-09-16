package handlers_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/category"
	"altalune.id/yasaku/internal/i18n"
	"altalune.id/yasaku/internal/ledger"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform/capabilities"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/project"
	"altalune.id/yasaku/internal/transaction"
	"altalune.id/yasaku/internal/user"
	"altalune.id/yasaku/internal/wallet"
	"altalune.id/yasaku/internal/web"
	"altalune.id/yasaku/internal/web/handlers"
	"altalune.id/yasaku/schema"
)

const (
	walletFixtureOrgSlug     = "yasaku-org"
	walletFixtureProjectSlug = "yasaku-project"
)

type walletFixture struct {
	Deps         handlers.Deps
	Cfg          *config.Config
	Sessions     session.Store
	DB           *sql.DB
	Orgs         *org.Service
	Projects     *project.Service
	Ledgers      *ledger.Service
	Wallets      *wallet.Service
	WalletOpen   *wallet.OpenWorkflow
	TxCategories *category.Service
	Transactions *transaction.Service
	Principal    session.Principal
	OrgID        uuid.UUID
	ProjectID    uuid.UUID
}

type noPeriods struct{}

func (noPeriods) Containing(context.Context, uuid.UUID, uuid.UUID, time.Time) (transaction.PeriodInfo, bool, error) {
	return transaction.PeriodInfo{}, false, nil
}

func (noPeriods) ByID(_ context.Context, _, _, id uuid.UUID) (transaction.PeriodInfo, error) {
	return transaction.PeriodInfo{ID: id}, nil
}

type bundleNamer struct{}

func (bundleNamer) DefaultName(ctx context.Context, key string) string {
	t := i18n.TranslatorFrom(ctx)
	if t == nil {
		return ""
	}
	return t.T(key)
}

func newWalletFixture(t *testing.T) *walletFixture {
	t.Helper()

	cfg := config.Defaults()
	cfg.HTTP.BasePath = ""
	cfg.HTTP.StateSecret = "0123456789abcdef0123456789abcdef"
	cfg.HTTP.BaseURL = "http://localhost"
	cfg.DB.Driver = db.DriverSQLite
	cfg.DB.DSN = filepath.Join(t.TempDir(), "yasaku-web.db")

	sqlDB, err := db.Open(t.Context(), cfg.DB, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, schema.MigrateUp(t.Context(), sqlDB, cfg))

	pool := db.Pool{W: sqlDB, R: sqlDB}
	dbCfg := db.DBConfig{Driver: db.DriverSQLite, TablePrefix: cfg.DB.TablePrefix}
	uow := func(ctx context.Context, fn func(context.Context) error) error {
		return db.RunInTx(ctx, pool, fn)
	}

	caps := capabilities.Capabilities{OrgCreation: true, LocalIdentity: true}
	log := discardLogger()
	unexpected := passthroughUnexpected()

	users := user.NewService(user.NewStore(dbCfg, pool), user.GenesisConfig{}, log, unexpected)
	orgs := org.NewService(org.NewStore(dbCfg, pool, nil), caps, log, unexpected)
	projects := project.NewService(project.NewStore(dbCfg, pool, nil), log, unexpected)
	ledgers := ledger.NewService(ledger.NewStore(dbCfg, pool, nil), log, unexpected)
	wallets := wallet.NewService(wallet.NewStore(dbCfg, pool, nil), log, unexpected)
	cats := category.NewService(category.NewStore(dbCfg, pool, nil), log, unexpected, bundleNamer{})
	txs := transaction.NewService(
		transaction.NewStore(dbCfg, pool, nil), log, unexpected,
		walletReaderForTest(wallets), categoryReaderForTest(cats),
		noPeriods{}, transaction.UnitOfWork(uow),
	)
	walletOpen := wallet.NewOpenWorkflow(wallets, txs, wallet.UnitOfWork(uow), log, unexpected)

	bundle := i18n.NewEmbeddedBundle(i18n.EnUS)
	deps := handlers.Deps{
		Cfg:      cfg,
		Caps:     caps,
		Sessions: session.NewMemoryStore(),
		Logger:   discardStdLogger(),
		Orgs:     orgs,
		Projects: projects,
		I18n:     bundle,
	}

	f := &walletFixture{
		Deps: deps, Cfg: cfg, Sessions: deps.Sessions, DB: sqlDB,
		Orgs: orgs, Projects: projects, Ledgers: ledgers,
		Wallets: wallets, WalletOpen: walletOpen, TxCategories: cats, Transactions: txs,
	}

	ctx := t.Context()
	u, err := users.Create(ctx, user.CreateRequest{Email: "owner@yasaku.test", Name: "Owner", Source: user.SourceLocal})
	require.NoError(t, err)
	o, err := orgs.Create(ctx, org.CreateRequest{Slug: walletFixtureOrgSlug, Name: "Yasaku Org", OwnerID: u.ID})
	require.NoError(t, err)
	pctx := tenant.Into(ctx, tenant.Context{OrgID: o.ID, UserID: u.ID})
	p, err := projects.Create(pctx, o.ID, walletFixtureProjectSlug, "Yasaku Project")
	require.NoError(t, err)

	f.OrgID, f.ProjectID = o.ID, p.ID
	f.Principal = session.Principal{
		UserID: u.ID, ActiveOrgID: o.ID, ActiveProjectID: p.ID, IssuedAt: time.Now().UTC(),
	}
	return f
}

func walletReaderForTest(wallets *wallet.Service) transaction.WalletReaderFunc {
	return func(ctx context.Context, orgID, projectID, id uuid.UUID) (transaction.WalletInfo, error) {
		w, err := wallets.ByID(ctx, id)
		if err != nil {
			return transaction.WalletInfo{}, err
		}
		if w.OrgID != orgID || w.ProjectID != projectID {
			return transaction.WalletInfo{}, &wallet.NotFoundError{ID: id.String()}
		}
		return transaction.WalletInfo{ID: w.ID, Currency: w.Currency, Archived: w.IsArchived()}, nil
	}
}

func categoryReaderForTest(cats *category.Service) transaction.CategoryReaderFunc {
	return func(ctx context.Context, orgID, projectID, id uuid.UUID) (transaction.CategoryInfo, error) {
		c, err := cats.ByID(ctx, id)
		if err != nil {
			return transaction.CategoryInfo{}, err
		}
		if c.OrgID != orgID || c.ProjectID != projectID {
			return transaction.CategoryInfo{}, &category.NotFoundError{ID: id.String()}
		}
		return transaction.CategoryInfo{ID: c.ID, Kind: string(c.Kind), Archived: c.IsArchived()}, nil
	}
}

func (f *walletFixture) scoped(t *testing.T) context.Context {
	t.Helper()
	ctx := tenant.Into(t.Context(), tenant.Context{
		OrgID: f.OrgID, ProjectID: f.ProjectID, UserID: f.Principal.UserID,
	})
	return f.withLocale(ctx)
}

func (f *walletFixture) withLocale(ctx context.Context) context.Context {
	loc := i18n.IdID
	ctx = i18n.LocaleInto(ctx, loc)
	return i18n.TranslatorInto(ctx, f.Deps.I18n.For(loc))
}

func (f *walletFixture) path(sub string) string {
	return "/orgs/" + walletFixtureOrgSlug + "/projects/" + walletFixtureProjectSlug + sub
}

func (f *walletFixture) do(t *testing.T, mux *http.ServeMux, method, sub string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	sid, err := web.NewSID()
	require.NoError(t, err)
	require.NoError(t, f.Sessions.Save(t.Context(), sid, f.Principal, time.Now().Add(web.SessionTTL)))

	var r *http.Request
	if form != nil {
		r = httptest.NewRequest(method, f.path(sub), strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		r = httptest.NewRequest(method, f.path(sub), nil)
	}
	r.AddCookie(&http.Cookie{
		Name:  web.SessionCookieName,
		Value: web.SignCookie([]byte(f.Cfg.HTTP.StateSecret), sid),
	})
	ctx := session.PrincipalInto(r.Context(), f.Principal)
	r = r.WithContext(f.withLocale(ctx))

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, r)
	return rec
}

func (f *walletFixture) walletMux(t *testing.T) *http.ServeMux {
	t.Helper()
	h := handlers.NewWalletHandler(f.Deps, f.Projects, f.Wallets, f.WalletOpen, f.Transactions, f.Ledgers)
	mux := http.NewServeMux()
	h.Register(mux)
	return mux
}

func TestWallet_CreateWithOpeningBalance_ListsFormattedBalance(t *testing.T) {
	t.Parallel()
	f := newWalletFixture(t)
	mux := f.walletMux(t)

	rec := f.do(t, mux, http.MethodPost, "/wallets", url.Values{
		"name":            {"BCA"},
		"kind":            {"bank"},
		"opening_balance": {"100000"},
	})
	require.Equal(t, http.StatusSeeOther, rec.Code, rec.Body.String())

	list := f.do(t, mux, http.MethodGet, "/wallets", nil)
	require.Equal(t, http.StatusOK, list.Code)
	body := list.Body.String()
	assert.Contains(t, body, "BCA")
	assert.Contains(t, body, "Rp100.000")

	items, err := f.Wallets.List(f.scoped(t), wallet.ListOpts{})
	require.NoError(t, err)
	require.Len(t, items, 1)

	txs, _, err := f.Transactions.List(f.scoped(t), transaction.ListOpts{WalletID: &items[0].ID})
	require.NoError(t, err)
	require.Len(t, txs, 1, "the opening balance must be a transaction, not a stored column")
	assert.Equal(t, transaction.KindOpening, txs[0].Kind)
}

func TestWallet_CreateWithoutOpeningBalance_RecordsNoTransaction(t *testing.T) {
	t.Parallel()
	f := newWalletFixture(t)
	mux := f.walletMux(t)

	rec := f.do(t, mux, http.MethodPost, "/wallets", url.Values{"name": {"Dompet"}, "kind": {"cash"}})
	require.Equal(t, http.StatusSeeOther, rec.Code, rec.Body.String())
	require.Equal(t, http.StatusSeeOther, f.do(t, mux, http.MethodPost, "/wallets", url.Values{
		"name": {"BCA"}, "kind": {"bank"}, "opening_balance": {"100000"},
	}).Code, "a funded sibling keeps the Total card off Rp0")

	empty := f.walletNamed(t, "Dompet")
	txs, _, err := f.Transactions.List(f.scoped(t), transaction.ListOpts{WalletID: &empty.ID})
	require.NoError(t, err)
	assert.Empty(t, txs)

	list := f.do(t, mux, http.MethodGet, "/wallets", nil)
	require.Equal(t, http.StatusOK, list.Code)
	body := list.Body.String()
	require.NotContains(t, body, ">Rp0<", "the Total card must not itself render Rp0, or this proves nothing")
	assert.Contains(t, body, "Rp0", "a wallet absent from the balances map renders as a zero, not a panic")
}

func TestWallet_CreateWithBlankName_ReRendersFormWithBanner(t *testing.T) {
	t.Parallel()
	f := newWalletFixture(t)
	mux := f.walletMux(t)

	rec := f.do(t, mux, http.MethodPost, "/wallets", url.Values{"name": {"  "}, "kind": {"cash"}})
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "WLT002")

	items, err := f.Wallets.List(f.scoped(t), wallet.ListOpts{})
	require.NoError(t, err)
	assert.Empty(t, items)
}

func TestWallet_DeleteWithTransactions_ShowsInUseBannerAndKeepsWallet(t *testing.T) {
	t.Parallel()
	f := newWalletFixture(t)
	mux := f.walletMux(t)

	require.Equal(t, http.StatusSeeOther, f.do(t, mux, http.MethodPost, "/wallets", url.Values{
		"name": {"BCA"}, "kind": {"bank"}, "opening_balance": {"100000"},
	}).Code)
	w := f.onlyWallet(t)

	rec := f.do(t, mux, http.MethodPost, "/wallets/"+w.ID.String()+"/delete", url.Values{})
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "WLT005")
	assert.Contains(t, body, "BCA")

	items, err := f.Wallets.List(f.scoped(t), wallet.ListOpts{})
	require.NoError(t, err)
	require.Len(t, items, 1, "a refused delete must leave the wallet in place")
}

func TestWallet_DeleteWithoutTransactions_RemovesIt(t *testing.T) {
	t.Parallel()
	f := newWalletFixture(t)
	mux := f.walletMux(t)

	require.Equal(t, http.StatusSeeOther, f.do(t, mux, http.MethodPost, "/wallets", url.Values{
		"name": {"Dompet"}, "kind": {"cash"},
	}).Code)
	w := f.onlyWallet(t)

	rec := f.do(t, mux, http.MethodPost, "/wallets/"+w.ID.String()+"/delete", url.Values{})
	require.Equal(t, http.StatusOK, rec.Code)

	items, err := f.Wallets.List(f.scoped(t), wallet.ListOpts{IncludeArchived: true})
	require.NoError(t, err)
	assert.Empty(t, items)
}

func TestWallet_Archive_MovesItUnderArchivedAndUnarchiveRestoresIt(t *testing.T) {
	t.Parallel()
	f := newWalletFixture(t)
	mux := f.walletMux(t)

	require.Equal(t, http.StatusSeeOther, f.do(t, mux, http.MethodPost, "/wallets", url.Values{
		"name": {"GoPay"}, "kind": {"ewallet"},
	}).Code)
	w := f.onlyWallet(t)

	rec := f.do(t, mux, http.MethodPost, "/wallets/"+w.ID.String()+"/archive", url.Values{})
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, `id="wallet-archived"`)
	assert.False(t, containsBefore(body, "GoPay", `id="wallet-archived"`),
		"an archived wallet must not stay in the active list")

	active, err := f.Wallets.List(f.scoped(t), wallet.ListOpts{})
	require.NoError(t, err)
	assert.Empty(t, active)

	back := f.do(t, mux, http.MethodPost, "/wallets/"+w.ID.String()+"/unarchive", url.Values{})
	require.Equal(t, http.StatusOK, back.Code)
	assert.True(t, containsBefore(back.Body.String(), "GoPay", `id="wallet-archived"`))
}

func TestWallet_Adjust_WritesAdjustmentAndShowsNewBalance(t *testing.T) {
	t.Parallel()
	f := newWalletFixture(t)
	mux := f.walletMux(t)

	require.Equal(t, http.StatusSeeOther, f.do(t, mux, http.MethodPost, "/wallets", url.Values{
		"name": {"BCA"}, "kind": {"bank"}, "opening_balance": {"100000"},
	}).Code)
	w := f.onlyWallet(t)

	rec := f.do(t, mux, http.MethodPost, "/wallets/"+w.ID.String()+"/adjust", url.Values{"target": {"150000"}})
	require.Equal(t, http.StatusSeeOther, rec.Code, rec.Body.String())
	detail := f.follow(t, mux, rec)
	require.Equal(t, http.StatusOK, detail.Code)
	assert.Contains(t, detail.Body.String(), "Rp150.000")

	txs, _, err := f.Transactions.List(f.scoped(t), transaction.ListOpts{WalletID: &w.ID})
	require.NoError(t, err)
	require.Len(t, txs, 2)
	assert.Equal(t, transaction.KindAdjustmentIn, txs[0].Kind)
	assert.EqualValues(t, 5000000, txs[0].Amount.Minor)
}

func TestWallet_AdjustToTheSameBalance_RendersNoChange(t *testing.T) {
	t.Parallel()
	f := newWalletFixture(t)
	mux := f.walletMux(t)

	require.Equal(t, http.StatusSeeOther, f.do(t, mux, http.MethodPost, "/wallets", url.Values{
		"name": {"BCA"}, "kind": {"bank"}, "opening_balance": {"100000"},
	}).Code)
	w := f.onlyWallet(t)

	rec := f.do(t, mux, http.MethodPost, "/wallets/"+w.ID.String()+"/adjust", url.Values{"target": {"100000"}})
	require.Equal(t, http.StatusSeeOther, rec.Code, rec.Body.String())
	detail := f.follow(t, mux, rec)
	require.Equal(t, http.StatusOK, detail.Code)
	body := detail.Body.String()
	assert.Contains(t, body, `data-adjust-state="unchanged"`)
	assert.NotContains(t, body, "Rp0", "an unchanged balance must not report a zero adjustment")

	txs, _, err := f.Transactions.List(f.scoped(t), transaction.ListOpts{WalletID: &w.ID})
	require.NoError(t, err)
	assert.Len(t, txs, 1, "an adjustment to the current balance writes nothing")
}

func TestWallet_UnknownID_Is404(t *testing.T) {
	t.Parallel()
	f := newWalletFixture(t)
	mux := f.walletMux(t)

	rec := f.do(t, mux, http.MethodGet, "/wallets/"+uuid.NewString(), nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestWallet_EditAndUpdate_ChangesNameAndKind(t *testing.T) {
	t.Parallel()
	f := newWalletFixture(t)
	mux := f.walletMux(t)

	require.Equal(t, http.StatusSeeOther, f.do(t, mux, http.MethodPost, "/wallets", url.Values{
		"name": {"Dompet"}, "kind": {"cash"},
	}).Code)
	w := f.onlyWallet(t)

	form := f.do(t, mux, http.MethodGet, "/wallets/"+w.ID.String()+"/edit", nil)
	require.Equal(t, http.StatusOK, form.Code)
	assert.Contains(t, form.Body.String(), "Dompet")

	rec := f.do(t, mux, http.MethodPost, "/wallets/"+w.ID.String(), url.Values{
		"name": {"Dompet Harian"}, "kind": {"bank"}, "provider": {"Cash"}, "exclude_from_total": {"on"},
	})
	require.Equal(t, http.StatusSeeOther, rec.Code, rec.Body.String())

	updated, err := f.Wallets.ByID(f.scoped(t), w.ID)
	require.NoError(t, err)
	assert.Equal(t, "Dompet Harian", updated.Name)
	assert.Equal(t, wallet.KindBank, updated.Kind, "the posted kind must replace the stored one")
	assert.Equal(t, "Cash", updated.Provider)
	assert.True(t, updated.ExcludeFromTotal)
}

func TestWallet_NewAndAdjustForms_Render(t *testing.T) {
	t.Parallel()
	f := newWalletFixture(t)
	mux := f.walletMux(t)

	newForm := f.do(t, mux, http.MethodGet, "/wallets/new", nil)
	require.Equal(t, http.StatusOK, newForm.Code)
	assert.Contains(t, newForm.Body.String(), f.path("/wallets"))

	require.Equal(t, http.StatusSeeOther, f.do(t, mux, http.MethodPost, "/wallets", url.Values{
		"name": {"BCA"}, "kind": {"bank"}, "opening_balance": {"100000"},
	}).Code)
	w := f.onlyWallet(t)

	adjust := f.do(t, mux, http.MethodGet, "/wallets/"+w.ID.String()+"/adjust", nil)
	require.Equal(t, http.StatusOK, adjust.Code)
	assert.Contains(t, adjust.Body.String(), "Rp100.000")
}

func TestWallet_FragmentsCarryTheOrgScopedActionURLs(t *testing.T) {
	t.Parallel()
	f := newWalletFixture(t)
	mux := f.walletMux(t)

	require.Equal(t, http.StatusSeeOther, f.do(t, mux, http.MethodPost, "/wallets", url.Values{
		"name": {"GoPay"}, "kind": {"ewallet"},
	}).Code)
	w := f.onlyWallet(t)

	rec := f.do(t, mux, http.MethodPost, "/wallets/"+w.ID.String()+"/archive", url.Values{})
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), f.path("/wallets/"+w.ID.String()+"/unarchive"))
}

func (f *walletFixture) onlyWallet(t *testing.T) *wallet.Wallet {
	t.Helper()
	items, err := f.Wallets.List(f.scoped(t), wallet.ListOpts{IncludeArchived: true})
	require.NoError(t, err)
	require.Len(t, items, 1)
	return items[0]
}

func containsBefore(body, needle, marker string) bool {
	m := strings.Index(body, marker)
	n := strings.Index(body, needle)
	return n >= 0 && (m < 0 || n < m)
}

func TestWallet_NegativeOpeningBalance_ShowsTypedAmountBanner(t *testing.T) {
	t.Parallel()
	f := newWalletFixture(t)
	mux := f.walletMux(t)

	rec := f.do(t, mux, http.MethodPost, "/wallets", url.Values{
		"name": {"BCA"}, "kind": {"bank"}, "opening_balance": {"-50000"},
	})
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.NotContains(t, body, "money:", "a parse failure must not leak the Go package name")
	assert.Contains(t, body, apperror.CodeTransactionInvalidAmount)
	assert.Contains(t, strings.ToLower(body), "negative", "the parse reason must survive the conversion")

	items, err := f.Wallets.List(f.scoped(t), wallet.ListOpts{IncludeArchived: true})
	require.NoError(t, err)
	assert.Empty(t, items)
}

func TestWallet_UnparseableAdjustTarget_ShowsTypedAmountBanner(t *testing.T) {
	t.Parallel()
	f := newWalletFixture(t)
	mux := f.walletMux(t)

	require.Equal(t, http.StatusSeeOther, f.do(t, mux, http.MethodPost, "/wallets", url.Values{
		"name": {"BCA"}, "kind": {"bank"}, "opening_balance": {"100000"},
	}).Code)
	w := f.onlyWallet(t)

	rec := f.do(t, mux, http.MethodPost, "/wallets/"+w.ID.String()+"/adjust", url.Values{"target": {"Rp 100.000,-"}})
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.NotContains(t, body, "money:", "a parse failure must not leak the Go package name")
	assert.Contains(t, body, apperror.CodeTransactionInvalidAmount)

	txs, _, err := f.Transactions.List(f.scoped(t), transaction.ListOpts{WalletID: &w.ID})
	require.NoError(t, err)
	assert.Len(t, txs, 1, "a refused target writes nothing")
}

func TestWallet_ArchivedWallet_OffersNeitherEditNorAdjust(t *testing.T) {
	t.Parallel()
	f := newWalletFixture(t)
	mux := f.walletMux(t)

	require.Equal(t, http.StatusSeeOther, f.do(t, mux, http.MethodPost, "/wallets", url.Values{
		"name": {"GoPay"}, "kind": {"ewallet"},
	}).Code)
	w := f.onlyWallet(t)
	require.Equal(t, http.StatusOK, f.do(t, mux, http.MethodPost, "/wallets/"+w.ID.String()+"/archive", url.Values{}).Code)

	list := f.do(t, mux, http.MethodGet, "/wallets", nil)
	require.Equal(t, http.StatusOK, list.Code)
	assert.NotContains(t, list.Body.String(), f.path("/wallets/"+w.ID.String()+"/edit"),
		"an archived row must not offer an edit that can only be refused")
	assert.Contains(t, list.Body.String(), f.path("/wallets/"+w.ID.String()+"/unarchive"),
		"unarchive stays the way forward")

	detail := f.do(t, mux, http.MethodGet, "/wallets/"+w.ID.String(), nil)
	require.Equal(t, http.StatusOK, detail.Code)
	assert.NotContains(t, detail.Body.String(), f.path("/wallets/"+w.ID.String()+"/edit"))
	assert.NotContains(t, detail.Body.String(), f.path("/wallets/"+w.ID.String()+"/adjust"))

	for _, sub := range []string{"/edit", "/adjust"} {
		form := f.do(t, mux, http.MethodGet, "/wallets/"+w.ID.String()+sub, nil)
		require.Equal(t, http.StatusOK, form.Code)
		assert.Contains(t, form.Body.String(), apperror.CodeWalletArchived,
			"a bookmarked "+sub+" must say why it cannot be submitted")
	}
}

func TestWallet_UpdateArchived_IsRefusedAndKeepsEveryField(t *testing.T) {
	t.Parallel()
	f := newWalletFixture(t)
	mux := f.walletMux(t)

	require.Equal(t, http.StatusSeeOther, f.do(t, mux, http.MethodPost, "/wallets", url.Values{
		"name": {"GoPay"}, "kind": {"ewallet"},
	}).Code)
	w := f.onlyWallet(t)
	require.Equal(t, http.StatusOK, f.do(t, mux, http.MethodPost, "/wallets/"+w.ID.String()+"/archive", url.Values{}).Code)

	rec := f.do(t, mux, http.MethodPost, "/wallets/"+w.ID.String(), url.Values{
		"name": {"OVO"}, "kind": {"bank"}, "provider": {"Bank"},
	})
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), apperror.CodeWalletArchived)

	after, err := f.Wallets.ByID(f.scoped(t), w.ID)
	require.NoError(t, err)
	assert.Equal(t, "GoPay", after.Name, "a refused edit must not land a partial rename")
	assert.Equal(t, wallet.KindEwallet, after.Kind)
	assert.Empty(t, after.Provider)
}

func (f *walletFixture) walletNamed(t *testing.T, name string) *wallet.Wallet {
	t.Helper()
	items, err := f.Wallets.List(f.scoped(t), wallet.ListOpts{IncludeArchived: true})
	require.NoError(t, err)
	for _, it := range items {
		if it.Name == name {
			return it
		}
	}
	t.Fatalf("no wallet named %q", name)
	return nil
}

func (f *walletFixture) follow(t *testing.T, mux *http.ServeMux, rec *httptest.ResponseRecorder) *httptest.ResponseRecorder {
	t.Helper()
	loc := rec.Header().Get("Location")
	require.NotEmpty(t, loc, "expected a redirect")
	return f.do(t, mux, http.MethodGet, strings.TrimPrefix(loc, f.path("")), nil)
}
