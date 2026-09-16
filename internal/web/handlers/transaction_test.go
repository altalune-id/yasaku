package handlers_test

import (
	"context"
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
	"altalune.id/yasaku/internal/ledger"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/period"
	"altalune.id/yasaku/internal/platform/capabilities"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/project"
	"altalune.id/yasaku/internal/report"
	"altalune.id/yasaku/internal/testutil/fakes"
	"altalune.id/yasaku/internal/transaction"
	"altalune.id/yasaku/internal/user"
	"altalune.id/yasaku/internal/wallet"
	"altalune.id/yasaku/internal/web"
	"altalune.id/yasaku/internal/web/handlers"
	"altalune.id/yasaku/money"
)

type txFixture struct {
	Deps     handlers.Deps
	Cfg      *config.Config
	Sessions session.Store
	Bundle   *i18n.Bundle

	Orgs         *org.Service
	Projects     *project.Service
	Wallets      *wallet.Service
	TxCategories *category.Service
	Periods      *period.Service
	Transactions *transaction.Service
	Reports      *report.Service
	Ledgers      *ledger.Service
	ReportReader *fakes.ReportReader

	Principal session.Principal
	OrgID     uuid.UUID
	ProjID    uuid.UUID
	OrgSlug   string
	ProjSlug  string
	Cash      uuid.UUID
	Bank      uuid.UUID
	Food      uuid.UUID
}

type txPeriodResolver struct{ periods *period.Service }

func (a txPeriodResolver) Containing(ctx context.Context, _, _ uuid.UUID, at time.Time) (transaction.PeriodInfo, bool, error) {
	p, err := a.periods.Containing(ctx, at)
	if err != nil {
		if period.IsNotFoundError(err) {
			return transaction.PeriodInfo{}, false, nil
		}
		return transaction.PeriodInfo{}, false, err
	}
	info, err := a.info(ctx, p)
	if err != nil {
		return transaction.PeriodInfo{}, false, err
	}
	return info, true, nil
}

func (a txPeriodResolver) ByID(ctx context.Context, _, _, id uuid.UUID) (transaction.PeriodInfo, error) {
	p, err := a.periods.ByID(ctx, id)
	if err != nil {
		return transaction.PeriodInfo{}, err
	}
	return a.info(ctx, p)
}

func (a txPeriodResolver) info(ctx context.Context, p *period.Period) (transaction.PeriodInfo, error) {
	prev, next, err := a.periods.Neighbors(ctx, p.ID)
	if err != nil {
		return transaction.PeriodInfo{}, err
	}
	info := transaction.PeriodInfo{ID: p.ID, Locked: p.IsLocked()}
	if prev != nil {
		id := prev.ID
		info.PrevID = &id
	}
	if next != nil {
		id := next.ID
		info.NextID = &id
	}
	return info, nil
}

type txSnapshotter struct{}

func (txSnapshotter) Snapshot(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (period.Snapshot, error) {
	return period.Snapshot{Currency: money.IDR}, nil
}

func txPassthroughUoW(ctx context.Context, fn func(ctx context.Context) error) error { return fn(ctx) }

func newTxFixture(t *testing.T) *txFixture {
	t.Helper()
	cfg := &config.Config{}
	cfg.HTTP.BasePath = ""
	cfg.HTTP.StateSecret = "0123456789abcdef0123456789abcdef"
	cfg.HTTP.BaseURL = "http://localhost"
	caps := capabilities.Capabilities{OrgCreation: true, LocalIdentity: true}
	sessions := session.NewMemoryStore()
	bundle := i18n.NewEmbeddedBundle(i18n.IdID)

	users := user.NewService(fakes.NewUser(), user.GenesisConfig{}, discardLogger(), passthroughUnexpected())
	orgs := org.NewService(fakes.NewOrg(), caps, discardLogger(), passthroughUnexpected())
	projects := project.NewService(fakes.NewProject(), discardLogger(), passthroughUnexpected())

	ledgers := ledger.NewService(fakes.NewLedger(), discardLogger(), passthroughUnexpected())
	wallets := wallet.NewService(fakes.NewWallet(), discardLogger(), passthroughUnexpected())
	cats := category.NewService(fakes.NewTxCategory(), discardLogger(), passthroughUnexpected(), txNamer{})
	reader := fakes.NewReportReader()
	reports := report.NewService(reader, discardLogger(), passthroughUnexpected(), ledgers)
	periods := period.NewService(fakes.NewPeriod(), discardLogger(), passthroughUnexpected(),
		ledgers, txSnapshotter{}, period.UnitOfWork(txPassthroughUoW), time.Now)
	txs := transaction.NewService(fakes.NewTransaction(), discardLogger(), passthroughUnexpected(),
		txWalletReader(wallets), txCategoryReader(cats),
		txPeriodResolver{periods: periods}, transaction.UnitOfWork(txPassthroughUoW))

	ctx := context.Background()
	u, err := users.Create(ctx, user.CreateRequest{Email: "tx@b.co", Name: "Tx", Source: user.SourceLocal})
	require.NoError(t, err)
	o, err := orgs.Create(ctx, org.CreateRequest{Slug: "acme", Name: "Acme", OwnerID: u.ID})
	require.NoError(t, err)
	octx := tenant.Into(ctx, tenant.Context{OrgID: o.ID, UserID: u.ID})
	proj, err := projects.Create(octx, o.ID, "main", "Main")
	require.NoError(t, err)

	f := &txFixture{
		Cfg: cfg, Sessions: sessions, Bundle: bundle,
		Orgs: orgs, Projects: projects, Wallets: wallets, TxCategories: cats,
		Periods: periods, Transactions: txs, Reports: reports, Ledgers: ledgers,
		ReportReader: reader,
		Principal:    session.Principal{UserID: u.ID, ActiveOrgID: o.ID, IssuedAt: time.Now().UTC()},
		OrgID:        o.ID, ProjID: proj.ID,
		OrgSlug: o.Slug, ProjSlug: proj.Slug,
	}
	f.Deps = handlers.Deps{
		Cfg: cfg, Caps: caps, Sessions: sessions, Logger: discardStdLogger(),
		Orgs: orgs, Projects: projects, I18n: bundle,
	}

	pctx := f.scoped(o.ID, proj.ID, u.ID)
	cash, err := wallets.Create(pctx, wallet.Params{Name: "Tunai", Kind: wallet.KindCash, Currency: money.IDR})
	require.NoError(t, err)
	bank, err := wallets.Create(pctx, wallet.Params{Name: "BCA", Kind: wallet.KindBank, Currency: money.IDR})
	require.NoError(t, err)
	food, err := cats.Create(pctx, "Makan", category.KindExpense, "utensils", "chart-1")
	require.NoError(t, err)
	f.Cash, f.Bank, f.Food = cash.ID, bank.ID, food.ID
	f.Principal.ActiveProjectID = proj.ID
	f.ReportReader.Balances = []report.WalletLine{
		{WalletID: cash.ID, Name: "Tunai", Kind: "cash", Closing: money.New(2500000, money.IDR)},
	}
	return f
}

type txNamer struct{}

func (txNamer) DefaultName(context.Context, string) string { return "" }

func txWalletReader(wallets *wallet.Service) transaction.WalletReaderFunc {
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

func txCategoryReader(cats *category.Service) transaction.CategoryReaderFunc {
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

func (f *txFixture) scoped(orgID, projectID, userID uuid.UUID) context.Context {
	return tenant.Into(context.Background(), tenant.Context{OrgID: orgID, ProjectID: projectID, UserID: userID})
}

func (f *txFixture) projectCtx(t *testing.T) context.Context {
	t.Helper()
	return f.scoped(f.OrgID, f.ProjID, f.Principal.UserID)
}

func (f *txFixture) overviewHandler() *handlers.OverviewHandler {
	return handlers.NewOverviewHandler(f.Deps, f.Projects, f.Wallets, f.Transactions,
		f.Periods, f.Reports, f.TxCategories, f.Ledgers)
}

func (f *txFixture) txHandler() *handlers.TransactionHandler {
	return handlers.NewTransactionHandler(f.Deps, f.Projects, f.Wallets, f.Transactions,
		f.Periods, f.TxCategories, f.Ledgers)
}

func (f *txFixture) request(t *testing.T, method, target string, form url.Values, htmx bool) *http.Request {
	t.Helper()
	sid, err := web.NewSID()
	require.NoError(t, err)
	require.NoError(t, f.Sessions.Save(context.Background(), sid, f.Principal, time.Now().Add(web.SessionTTL)))

	var r *http.Request
	if form != nil {
		r = httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		r = httptest.NewRequest(method, target, nil)
	}
	if htmx {
		r.Header.Set("HX-Request", "true")
	}
	r.AddCookie(&http.Cookie{Name: web.SessionCookieName, Value: web.SignCookie([]byte(f.Cfg.HTTP.StateSecret), sid)})
	ctx := session.PrincipalInto(r.Context(), f.Principal)
	ctx = i18n.LocaleInto(ctx, i18n.IdID)
	ctx = i18n.TranslatorInto(ctx, f.Bundle.For(i18n.IdID))
	return r.WithContext(ctx)
}

func (f *txFixture) path(sub string) string {
	return "/orgs/" + f.OrgSlug + "/projects/" + f.ProjSlug + sub
}

func TestOverviewHandler_Get_RendersSeededProject(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	mux := http.NewServeMux()
	f.overviewHandler().Register(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, f.request(t, http.MethodGet, f.path("/overview"), nil, false))

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "Sisa yang bisa dipakai")
	assert.Contains(t, body, "Rp25.000")
	assert.Contains(t, body, "Belum ada periode yang berjalan.")
	assert.Contains(t, body, `id="tx-quick-add"`)
	assert.Contains(t, body, `name="list" value="recent"`)
}

func TestOverviewHandler_Get_RemembersTheProject(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	f.Principal.ActiveProjectID = uuid.Nil
	mux := http.NewServeMux()
	f.overviewHandler().Register(mux)

	r := f.request(t, http.MethodGet, f.path("/overview"), nil, false)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, r)
	require.Equal(t, http.StatusOK, rec.Code)

	c, err := r.Cookie(web.SessionCookieName)
	require.NoError(t, err)
	sid, err := web.VerifyCookie([]byte(f.Cfg.HTTP.StateSecret), c.Value)
	require.NoError(t, err)
	p, ok, err := f.Sessions.Load(context.Background(), sid)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, f.ProjID, p.ActiveProjectID)
}

func TestTransactionHandler_PostCreate_RecordsExpenseAndReturnsFragment(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	mux := http.NewServeMux()
	f.txHandler().Register(mux)

	form := url.Values{
		"kind":      {"expense"},
		"amount":    {"40.000"},
		"wallet_id": {f.Cash.String()},
		"date":      {civil.DateOf(time.Now(), time.UTC).String()},
		"note":      {"Kopi pagi"},
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, f.request(t, http.MethodPost, f.path("/transactions"), form, true))

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, `id="tx-list"`)
	assert.Contains(t, body, "Rp40.000")
	assert.Contains(t, body, `id="toast"`)
	assert.Contains(t, body, `hx-swap-oob="true"`)
	// SECURITY: a fragment rendered with a bare Deps.Base collapses every action URL to /orgs.
	assert.Contains(t, body, f.path("/transactions/"))
	assert.NotContains(t, body, `href="/orgs/transactions`)

	items, _, err := f.Transactions.List(f.projectCtx(t), transaction.ListOpts{})
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, int64(4000000), items[0].Amount.Minor)
	assert.Equal(t, transaction.KindExpense, items[0].Kind)
	assert.Equal(t, f.Cash, items[0].WalletID)

	var last string
	for _, c := range rec.Result().Cookies() {
		if c.Name == "yasaku_last_wallet" {
			last = c.Value
		}
	}
	assert.Equal(t, f.Cash.String(), last)
}

func TestTransactionHandler_PostCreate_ClosedPeriodRendersBannerAndWritesNothing(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	ctx := f.projectCtx(t)
	cur, err := f.Periods.EnsureCurrent(ctx)
	require.NoError(t, err)
	loc, err := f.Ledgers.Location(ctx, cur.OrgID, cur.ProjectID)
	require.NoError(t, err)
	_, err = f.Periods.Close(ctx, cur.ID, civil.DateOf(time.Now(), loc), f.Principal.UserID)
	require.NoError(t, err)

	mux := http.NewServeMux()
	f.txHandler().Register(mux)
	form := url.Values{
		"kind":      {"expense"},
		"amount":    {"40000"},
		"wallet_id": {f.Cash.String()},
		"date":      {civil.DateOf(time.Now(), loc).String()},
		"note":      {"Kopi"},
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, f.request(t, http.MethodPost, f.path("/transactions"), form, true))

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, `id="tx-list"`)
	assert.Contains(t, body, "TXN008")
	assert.Contains(t, body, "Periode ini sudah ditutup")

	items, _, err := f.Transactions.List(ctx, transaction.ListOpts{})
	require.NoError(t, err)
	assert.Empty(t, items)
}

func TestTransactionHandler_PostSuggest_PreselectsSuggestedCategory(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	ctx := f.projectCtx(t)
	catID := f.Food
	_, err := f.Transactions.Record(ctx, transaction.RecordInput{
		WalletID:   f.Cash,
		Kind:       transaction.KindExpense,
		Amount:     money.New(1500000, money.IDR),
		CategoryID: &catID,
		Note:       "Kopi pagi",
		OccurredAt: time.Now(),
	})
	require.NoError(t, err)

	mux := http.NewServeMux()
	f.txHandler().Register(mux)
	form := url.Values{"note": {"  kopi   PAGI "}, "kind": {"expense"}}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, f.request(t, http.MethodPost, f.path("/transactions/suggest"), form, true))

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, `id="tx-category"`)
	assert.Contains(t, body, f.Food.String())
	i := strings.Index(body, f.Food.String())
	require.Positive(t, i)
	assert.Contains(t, body[i:min(i+240, len(body))], "checked")
}

func TestTransactionHandler_GetList_FiltersByWallet(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	ctx := f.projectCtx(t)
	for _, tc := range []struct {
		wallet uuid.UUID
		note   string
	}{{f.Cash, "Bakso tunai"}, {f.Bank, "Listrik bank"}} {
		_, err := f.Transactions.Record(ctx, transaction.RecordInput{
			WalletID:   tc.wallet,
			Kind:       transaction.KindExpense,
			Amount:     money.New(2000000, money.IDR),
			Note:       tc.note,
			OccurredAt: time.Now(),
		})
		require.NoError(t, err)
	}

	mux := http.NewServeMux()
	f.txHandler().Register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, f.request(t, http.MethodGet, f.path("/transactions")+"?wallet="+f.Cash.String(), nil, false))

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "Bakso tunai")
	assert.NotContains(t, body, "Listrik bank")
}

func TestTransactionHandler_GetNew_RendersForm(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	mux := http.NewServeMux()
	f.txHandler().Register(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, f.request(t, http.MethodGet, f.path("/transactions/new"), nil, false))

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, `inputmode="numeric"`)
	assert.Contains(t, body, "Kemarin")
	assert.Contains(t, body, "Tunai")
}

func TestTransactionHandler_PostCreate_WithoutHTMXRedirectsToOverview(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	mux := http.NewServeMux()
	f.txHandler().Register(mux)

	form := url.Values{
		"kind":      {"income"},
		"amount":    {"1000000"},
		"wallet_id": {f.Bank.String()},
		"date":      {civil.DateOf(time.Now(), time.UTC).String()},
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, f.request(t, http.MethodPost, f.path("/transactions"), form, false))

	require.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, f.path("/overview"), rec.Header().Get("Location"))
}

var hxTargetRe = regexp.MustCompile(`hx-target="#([^"]+)"`)

func txHeadline(t *testing.T, body string) string {
	t.Helper()
	i := strings.Index(body, "tx-quick-add")
	require.Positive(t, i)
	return body[:i]
}

func assertHXTargetsResolve(t *testing.T, body string) {
	t.Helper()
	for _, m := range hxTargetRe.FindAllStringSubmatch(body, -1) {
		assert.Contains(t, body, `id="`+m[1]+`"`,
			"hx-target #%s has no matching element in the same document", m[1])
	}
}

func (f *txFixture) siblingProject(t *testing.T) (uuid.UUID, uuid.UUID) {
	t.Helper()
	octx := tenant.Into(context.Background(), tenant.Context{OrgID: f.OrgID, UserID: f.Principal.UserID})
	other, err := f.Projects.Create(octx, f.OrgID, "other", "Other")
	require.NoError(t, err)
	ctx := f.scoped(f.OrgID, other.ID, f.Principal.UserID)
	w, err := f.Wallets.Create(ctx, wallet.Params{Name: "Lain", Kind: wallet.KindCash, Currency: money.IDR})
	require.NoError(t, err)
	tx, err := f.Transactions.Record(ctx, transaction.RecordInput{
		WalletID:   w.ID,
		Kind:       transaction.KindExpense,
		Amount:     money.New(1000000, money.IDR),
		Note:       "Punya proyek lain",
		OccurredAt: time.Now(),
	})
	require.NoError(t, err)
	return other.ID, tx.ID
}

func (f *txFixture) closedPeriodTx(t *testing.T) uuid.UUID {
	t.Helper()
	ctx := f.projectCtx(t)
	cur, err := f.Periods.EnsureCurrent(ctx)
	require.NoError(t, err)
	loc, err := f.Ledgers.Location(ctx, cur.OrgID, cur.ProjectID)
	require.NoError(t, err)
	tx, err := f.Transactions.Record(ctx, transaction.RecordInput{
		WalletID:   f.Cash,
		Kind:       transaction.KindExpense,
		Amount:     money.New(1000000, money.IDR),
		Note:       "Sudah terkunci",
		OccurredAt: time.Now(),
	})
	require.NoError(t, err)
	_, err = f.Periods.Close(ctx, cur.ID, civil.DateOf(time.Now(), loc), f.Principal.UserID)
	require.NoError(t, err)
	return tx.ID
}

func TestTransactionHandler_PostCreate_RejectsSystemReservedKinds(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"opening", "adjustment_in", "adjustment_out", "refund"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			f := newTxFixture(t)
			mux := http.NewServeMux()
			f.txHandler().Register(mux)

			form := url.Values{
				"kind":      {kind},
				"amount":    {"40000"},
				"wallet_id": {f.Cash.String()},
				"date":      {civil.DateOf(time.Now(), time.UTC).String()},
			}
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, f.request(t, http.MethodPost, f.path("/transactions"), form, true))

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			items, _, err := f.Transactions.List(f.projectCtx(t), transaction.ListOpts{})
			require.NoError(t, err)
			assert.Empty(t, items)
		})
	}
}

func TestTransactionHandler_GetNew_EveryHXTargetExistsInTheDocument(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	mux := http.NewServeMux()
	f.txHandler().Register(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, f.request(t, http.MethodGet, f.path("/transactions/new"), nil, false))

	require.Equal(t, http.StatusOK, rec.Code)
	assertHXTargetsResolve(t, rec.Body.String())
}

func TestTransactionHandler_GetEdit_EveryHXTargetExistsInTheDocument(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	ctx := f.projectCtx(t)
	tx, err := f.Transactions.Record(ctx, transaction.RecordInput{
		WalletID:   f.Cash,
		Kind:       transaction.KindExpense,
		Amount:     money.New(1000000, money.IDR),
		Note:       "Bakso",
		OccurredAt: time.Now(),
	})
	require.NoError(t, err)

	mux := http.NewServeMux()
	f.txHandler().Register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, f.request(t, http.MethodGet, f.path("/transactions/"+tx.ID.String()+"/edit"), nil, false))

	require.Equal(t, http.StatusOK, rec.Code)
	assertHXTargetsResolve(t, rec.Body.String())
}

func TestTransactionHandler_GetList_EveryHXTargetExistsInTheDocument(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	mux := http.NewServeMux()
	f.txHandler().Register(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, f.request(t, http.MethodGet, f.path("/transactions"), nil, false))

	require.Equal(t, http.StatusOK, rec.Code)
	assertHXTargetsResolve(t, rec.Body.String())
}

func TestTransactionHandler_GetEdit_LocksTheKind(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	ctx := f.projectCtx(t)
	tx, err := f.Transactions.Record(ctx, transaction.RecordInput{
		WalletID:   f.Cash,
		Kind:       transaction.KindExpense,
		Amount:     money.New(1000000, money.IDR),
		OccurredAt: time.Now(),
	})
	require.NoError(t, err)

	mux := http.NewServeMux()
	f.txHandler().Register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, f.request(t, http.MethodGet, f.path("/transactions/"+tx.ID.String()+"/edit"), nil, false))

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, `data-tx-kind-locked="1"`)
	assert.Contains(t, body, `value="income" disabled`)
}

func TestTransactionHandler_PostUpdate_RevisesAmountAndKeepsTheStoredKind(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	ctx := f.projectCtx(t)
	tx, err := f.Transactions.Record(ctx, transaction.RecordInput{
		WalletID:   f.Cash,
		Kind:       transaction.KindExpense,
		Amount:     money.New(1000000, money.IDR),
		Note:       "Bakso",
		OccurredAt: time.Now(),
	})
	require.NoError(t, err)

	mux := http.NewServeMux()
	f.txHandler().Register(mux)
	form := url.Values{
		"kind":      {"income"},
		"amount":    {"25000"},
		"wallet_id": {f.Bank.String()},
		"date":      {civil.DateOf(time.Now(), time.UTC).String()},
		"note":      {"Bakso urat"},
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, f.request(t, http.MethodPost, f.path("/transactions/"+tx.ID.String()), form, true))

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `id="tx-list"`)

	got, err := f.Transactions.ByID(ctx, tx.ID)
	require.NoError(t, err)
	assert.Equal(t, transaction.KindExpense, got.Kind)
	assert.Equal(t, int64(2500000), got.Amount.Minor)
	assert.Equal(t, f.Bank, got.WalletID)
	assert.Equal(t, "Bakso urat", got.Note)
}

func TestTransactionHandler_PostDelete_RemovesTheTransaction(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	ctx := f.projectCtx(t)
	tx, err := f.Transactions.Record(ctx, transaction.RecordInput{
		WalletID:   f.Cash,
		Kind:       transaction.KindExpense,
		Amount:     money.New(1000000, money.IDR),
		OccurredAt: time.Now(),
	})
	require.NoError(t, err)

	mux := http.NewServeMux()
	f.txHandler().Register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, f.request(t, http.MethodPost, f.path("/transactions/"+tx.ID.String()+"/delete"), url.Values{}, false))

	require.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, f.path("/overview"), rec.Header().Get("Location"))

	items, _, err := f.Transactions.List(ctx, transaction.ListOpts{})
	require.NoError(t, err)
	assert.Empty(t, items)
}

func TestTransactionHandler_PostDelete_ClosedPeriodWithoutHTMXRendersFullPage(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	id := f.closedPeriodTx(t)

	mux := http.NewServeMux()
	f.txHandler().Register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, f.request(t, http.MethodPost, f.path("/transactions/"+id.String()+"/delete"), url.Values{}, false))

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "<html")
	assert.Contains(t, body, "</html>")
	assert.Contains(t, body, "TXN008")

	items, _, err := f.Transactions.List(f.projectCtx(t), transaction.ListOpts{})
	require.NoError(t, err)
	assert.Len(t, items, 1)
}

func TestTransactionHandler_SiblingProjectTransactionIsNotFound(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	_, id := f.siblingProject(t)

	mux := http.NewServeMux()
	f.txHandler().Register(mux)
	for _, tc := range []struct {
		name, method, target string
		form                 url.Values
	}{
		{"edit", http.MethodGet, f.path("/transactions/" + id.String() + "/edit"), nil},
		{"update", http.MethodPost, f.path("/transactions/" + id.String()), url.Values{
			"kind": {"expense"}, "amount": {"1000"}, "wallet_id": {f.Cash.String()},
		}},
		{"delete", http.MethodPost, f.path("/transactions/" + id.String() + "/delete"), url.Values{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, f.request(t, tc.method, tc.target, tc.form, false))
			assert.Equal(t, http.StatusNotFound, rec.Code)
		})
	}
}

func TestTransactionHandler_GetNew_AmountFollowsTheSelectedWalletCurrency(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	usd, err := f.Wallets.Create(f.projectCtx(t), wallet.Params{
		Name: "Payoneer", Kind: wallet.KindBank, Currency: money.Currency("USD"),
	})
	require.NoError(t, err)

	mux := http.NewServeMux()
	f.txHandler().Register(mux)
	r := f.request(t, http.MethodGet, f.path("/transactions/new"), nil, false)
	r.AddCookie(&http.Cookie{Name: handlers.LastWalletCookie, Value: usd.ID.String()})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, r)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, `data-tx-group="0"`, "USD must not be grouped like a zero-decimal currency")
	assert.Contains(t, body, `data-tx-symbol="$"`)
	assert.Contains(t, body, `data-tx-symbol="Rp"`)
}

func TestTransactionHandler_PostCreate_KeepsTheActiveFilter(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	ctx := f.projectCtx(t)
	_, err := f.Transactions.Record(ctx, transaction.RecordInput{
		WalletID:   f.Cash,
		Kind:       transaction.KindExpense,
		Amount:     money.New(2000000, money.IDR),
		Note:       "Bakso tunai",
		OccurredAt: time.Now(),
	})
	require.NoError(t, err)

	mux := http.NewServeMux()
	f.txHandler().Register(mux)
	form := url.Values{
		"kind":      {"expense"},
		"amount":    {"30000"},
		"wallet_id": {f.Bank.String()},
		"date":      {civil.DateOf(time.Now(), time.UTC).String()},
		"note":      {"Listrik bank"},
	}
	r := f.request(t, http.MethodPost, f.path("/transactions"), form, true)
	r.Header.Set("HX-Current-URL", "http://localhost"+f.path("/transactions")+"?wallet="+f.Cash.String())
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, r)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "Bakso tunai")
	assert.NotContains(t, body, "Listrik bank")
}

func TestOverviewHandler_Get_MixedCurrencyWalletsQualifyTheHeadline(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	f.ReportReader.Balances = []report.WalletLine{
		{WalletID: f.Cash, Name: "Tunai", Kind: "cash", Closing: money.New(2500000, money.IDR)},
		{WalletID: f.Bank, Name: "Payoneer", Kind: "bank", Closing: money.New(120000, money.Currency("USD"))},
	}
	mux := http.NewServeMux()
	f.overviewHandler().Register(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, f.request(t, http.MethodGet, f.path("/overview"), nil, false))

	require.Equal(t, http.StatusOK, rec.Code)
	head := txHeadline(t, rec.Body.String())
	assert.Contains(t, head, "Rp25.000")
	assert.Contains(t, head, `data-mixed-currency="1"`)
}

func TestOverviewHandler_Get_SingleForeignCurrencyStillTotals(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	f.ReportReader.Balances = []report.WalletLine{
		{WalletID: f.Bank, Name: "Payoneer", Kind: "bank", Closing: money.New(120000, money.Currency("USD"))},
	}
	mux := http.NewServeMux()
	f.overviewHandler().Register(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, f.request(t, http.MethodGet, f.path("/overview"), nil, false))

	require.Equal(t, http.StatusOK, rec.Code)
	head := txHeadline(t, rec.Body.String())
	assert.Contains(t, head, "$1.200,00")
	assert.NotContains(t, head, `data-mixed-currency="1"`)
}
