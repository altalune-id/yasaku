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

	"altalune.id/yasaku/civil"
	"altalune.id/yasaku/internal/category"
	"altalune.id/yasaku/internal/ledger"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/period"
	"altalune.id/yasaku/internal/platform/capabilities"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/project"
	"altalune.id/yasaku/internal/report"
	"altalune.id/yasaku/internal/transaction"
	"altalune.id/yasaku/internal/user"
	"altalune.id/yasaku/internal/wallet"
	"altalune.id/yasaku/internal/web"
	"altalune.id/yasaku/internal/web/handlers"
	"altalune.id/yasaku/money"
	"altalune.id/yasaku/schema"
)

// periodFixture boots the yasaku domain services over a migrated SQLite file, so the close flow
// exercises the real stores rather than canned reader output.
// NOTE: these tests are deliberately not t.Parallel — go-jet v2.13.0 mutates a package-global
// expression singleton, so two concurrent SQLite writers trip the race detector (docs/BACKLOG.md).
type periodFixture struct {
	Deps      handlers.Deps
	Cfg       *config.Config
	Sessions  session.Store
	DB        *sql.DB
	Orgs      *org.Service
	Projects  *project.Service
	Ledgers   *ledger.Service
	Wallets   *wallet.Service
	Periods   *period.Service
	Reports   *report.Service
	Txs       *transaction.Service
	PerStore  period.Store
	User      *user.User
	Org       *org.Org
	Project   *project.Project
	Principal session.Principal
}

type periodSnapshotter struct{ reports *report.Service }

func (a periodSnapshotter) Snapshot(ctx context.Context, orgID, projectID, periodID uuid.UUID) (period.Snapshot, error) {
	sum, err := a.reports.SnapshotFor(ctx, orgID, projectID, periodID)
	if err != nil {
		return period.Snapshot{}, err
	}
	snap := period.Snapshot{
		Currency:   sum.Currency,
		Income:     sum.Income.Minor,
		Expense:    sum.Expense.Minor,
		Net:        sum.Net.Minor,
		TxCount:    sum.TxCount,
		ComputedAt: time.Now().UTC(),
	}
	for _, w := range sum.Wallets {
		snap.Wallets = append(snap.Wallets, period.WalletClosing{WalletID: w.WalletID, Name: w.Name, Closing: w.Closing.Minor})
	}
	return snap, nil
}

type periodResolver struct{ periods *period.Service }

func (a periodResolver) Containing(ctx context.Context, _, _ uuid.UUID, at time.Time) (transaction.PeriodInfo, bool, error) {
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

func (a periodResolver) ByID(ctx context.Context, _, _, id uuid.UUID) (transaction.PeriodInfo, error) {
	p, err := a.periods.ByID(ctx, id)
	if err != nil {
		return transaction.PeriodInfo{}, err
	}
	return a.info(ctx, p)
}

func (a periodResolver) info(ctx context.Context, p *period.Period) (transaction.PeriodInfo, error) {
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

func newPeriodFixture(t *testing.T, caps capabilities.Capabilities) *periodFixture {
	t.Helper()

	cfg := config.Defaults()
	cfg.DB.Driver = db.DriverSQLite
	cfg.DB.DSN = filepath.Join(t.TempDir(), "web-period.db")
	cfg.HTTP.BasePath = ""
	cfg.HTTP.BaseURL = "http://localhost"
	cfg.HTTP.StateSecret = "0123456789abcdef0123456789abcdef"
	cfg.MCP.Audience = "http://localhost/mcp"

	sqlDB, err := db.Open(t.Context(), cfg.DB, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, schema.MigrateUp(t.Context(), sqlDB, cfg))

	dbCfg := db.DBConfig{Driver: db.DriverSQLite, TablePrefix: cfg.DB.TablePrefix}
	pool := db.Pool{W: sqlDB, R: sqlDB}
	uow := func(ctx context.Context, fn func(ctx context.Context) error) error {
		return db.RunInTx(ctx, pool, fn)
	}

	users := user.NewService(user.NewStore(dbCfg, pool), user.GenesisConfig{}, discardLogger(), passthroughUnexpected())
	orgs := org.NewService(org.NewStore(dbCfg, pool, nil), caps, discardLogger(), passthroughUnexpected())
	projects := project.NewService(project.NewStore(dbCfg, pool, nil), discardLogger(), passthroughUnexpected())
	ledgers := ledger.NewService(ledger.NewStore(dbCfg, pool, nil), discardLogger(), passthroughUnexpected())
	wallets := wallet.NewService(wallet.NewStore(dbCfg, pool, nil), discardLogger(), passthroughUnexpected())
	cats := category.NewService(category.NewStore(dbCfg, pool, nil), discardLogger(), passthroughUnexpected(), nil)
	reports := report.NewService(report.NewReader(dbCfg, pool, nil), discardLogger(), passthroughUnexpected(), ledgers)
	perStore := period.NewStore(dbCfg, pool, nil)
	periods := period.NewService(perStore, discardLogger(), passthroughUnexpected(),
		ledgers, periodSnapshotter{reports: reports}, period.UnitOfWork(uow), time.Now)
	txs := transaction.NewService(transaction.NewStore(dbCfg, pool, nil), discardLogger(), passthroughUnexpected(),
		transaction.WalletReaderFunc(func(ctx context.Context, orgID, projectID, id uuid.UUID) (transaction.WalletInfo, error) {
			w, wErr := wallets.ByID(ctx, id)
			if wErr != nil {
				return transaction.WalletInfo{}, wErr
			}
			return transaction.WalletInfo{ID: w.ID, Currency: w.Currency, Archived: w.IsArchived()}, nil
		}),
		transaction.CategoryReaderFunc(func(ctx context.Context, orgID, projectID, id uuid.UUID) (transaction.CategoryInfo, error) {
			c, cErr := cats.ByID(ctx, id)
			if cErr != nil {
				return transaction.CategoryInfo{}, cErr
			}
			return transaction.CategoryInfo{ID: c.ID, Kind: string(c.Kind), Archived: c.IsArchived()}, nil
		}),
		periodResolver{periods: periods}, transaction.UnitOfWork(uow))

	ctx := t.Context()
	u, err := users.Create(ctx, user.CreateRequest{Email: "books@yasaku.test", Name: "Books", Source: user.SourceLocal})
	require.NoError(t, err)
	o, err := orgs.Create(ctx, org.CreateRequest{Slug: "rumah", Name: "Rumah", OwnerID: u.ID})
	require.NoError(t, err)
	octx := tenant.Into(ctx, tenant.Context{OrgID: o.ID, UserID: u.ID})
	proj, err := projects.Create(octx, o.ID, "harian", "Harian")
	require.NoError(t, err)

	f := &periodFixture{
		Cfg:      cfg,
		Sessions: session.NewMemoryStore(),
		DB:       sqlDB,
		Orgs:     orgs,
		Projects: projects,
		Ledgers:  ledgers,
		Wallets:  wallets,
		Periods:  periods,
		Reports:  reports,
		Txs:      txs,
		PerStore: perStore,
		User:     u,
		Org:      o,
		Project:  proj,
	}
	f.Deps = handlers.Deps{
		Cfg:      cfg,
		Caps:     caps,
		Sessions: f.Sessions,
		Logger:   discardStdLogger(),
		Orgs:     orgs,
		Projects: projects,
	}
	f.Principal = session.Principal{UserID: u.ID, ActiveOrgID: o.ID, ActiveProjectID: proj.ID, IssuedAt: time.Now().UTC()}
	return f
}

// scoped returns a context already carrying the fixture's tenant scope, the way the handlers build one.
func (f *periodFixture) scoped(t *testing.T) context.Context {
	t.Helper()
	return tenant.Into(t.Context(), tenant.Context{OrgID: f.Org.ID, ProjectID: f.Project.ID, UserID: f.User.ID})
}

func (f *periodFixture) path(suffix string) string {
	return "/orgs/" + f.Org.Slug + "/projects/" + f.Project.Slug + suffix
}

func (f *periodFixture) mux(t *testing.T) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	handlers.NewPeriodHandler(f.Deps, f.Projects, f.Periods, f.Reports, f.Ledgers).Register(mux)
	handlers.NewSettingsHandler(f.Deps, f.Projects, f.Ledgers).Register(mux)
	return mux
}

func (f *periodFixture) do(t *testing.T, method, target string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	sid, err := web.NewSID()
	require.NoError(t, err)
	require.NoError(t, f.Sessions.Save(t.Context(), sid, f.Principal, time.Now().Add(web.SessionTTL)))

	var r *http.Request
	if form == nil {
		r = httptest.NewRequest(method, target, nil)
	} else {
		r = httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	r.AddCookie(&http.Cookie{Name: web.SessionCookieName, Value: web.SignCookie([]byte(f.Cfg.HTTP.StateSecret), sid)})
	r = r.WithContext(session.PrincipalInto(r.Context(), f.Principal))

	rec := httptest.NewRecorder()
	f.mux(t).ServeHTTP(rec, r)
	return rec
}

func (f *periodFixture) today(t *testing.T) civil.Date {
	t.Helper()
	st, err := f.Ledgers.Get(f.scoped(t))
	require.NoError(t, err)
	loc, err := st.Location()
	require.NoError(t, err)
	return civil.DateOf(time.Now(), loc)
}

// seedBooks opens a wallet and records one income and one expense into the project's first period.
func (f *periodFixture) seedBooks(t *testing.T) *period.Period {
	t.Helper()
	ctx := f.scoped(t)
	w, err := f.Wallets.Create(ctx, wallet.Params{Name: "BCA", Kind: wallet.KindBank, Currency: money.IDR})
	require.NoError(t, err)
	_, err = f.Txs.Record(ctx, transaction.RecordInput{
		WalletID: w.ID, Kind: transaction.KindIncome,
		Amount: money.New(500_000, money.IDR), OccurredAt: time.Now(),
	})
	require.NoError(t, err)
	_, err = f.Txs.Record(ctx, transaction.RecordInput{
		WalletID: w.ID, Kind: transaction.KindExpense,
		Amount: money.New(200_000, money.IDR), OccurredAt: time.Now(),
	})
	require.NoError(t, err)
	cur, err := f.Periods.Current(ctx)
	require.NoError(t, err)
	return cur
}

func defaultCaps() capabilities.Capabilities {
	return capabilities.Capabilities{OrgCreation: true, LocalIdentity: true}
}

func TestPeriods_GetPeriods_NoPeriodRendersEmptyStateWithoutCreatingOne(t *testing.T) {
	f := newPeriodFixture(t, defaultCaps())

	rec := f.do(t, http.MethodGet, f.path("/periods"), nil)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "period.no_period")

	// SECURITY-adjacent invariant: a read path must never open the books.
	_, err := f.Periods.Current(f.scoped(t))
	assert.True(t, period.IsNotFoundError(err), "GET /periods must not create the first period, got %v", err)
}

func TestPeriods_GetClose_PreviewShowsSeededIncomeAndExpense(t *testing.T) {
	f := newPeriodFixture(t, defaultCaps())
	cur := f.seedBooks(t)

	rec := f.do(t, http.MethodGet, f.path("/periods/"+cur.ID.String()+"/close"), nil)
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "Rp5,000", "income of 500000 minor IDR")
	assert.Contains(t, body, "Rp2,000", "expense of 200000 minor IDR")
	assert.Contains(t, body, "+Rp3,000", "net")
	assert.Contains(t, body, `id="close-preview"`)
	assert.Contains(t, body, `name="end_date"`)
	assert.Contains(t, body, f.today(t).String())
}

func TestPeriods_GetClose_HTMXReturnsOnlyThePreviewFragment(t *testing.T) {
	f := newPeriodFixture(t, defaultCaps())
	cur := f.seedBooks(t)

	sid, err := web.NewSID()
	require.NoError(t, err)
	require.NoError(t, f.Sessions.Save(t.Context(), sid, f.Principal, time.Now().Add(web.SessionTTL)))
	r := httptest.NewRequest(http.MethodGet, f.path("/periods/"+cur.ID.String()+"/close?end_date="+f.today(t).String()), nil)
	r.Header.Set("HX-Request", "true")
	r.AddCookie(&http.Cookie{Name: web.SessionCookieName, Value: web.SignCookie([]byte(f.Cfg.HTTP.StateSecret), sid)})
	r = r.WithContext(session.PrincipalInto(r.Context(), f.Principal))
	rec := httptest.NewRecorder()
	f.mux(t).ServeHTTP(rec, r)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, `id="close-preview"`)
	assert.NotContains(t, body, "<html", "the fragment must not carry the page chrome")
	assert.Contains(t, body, "Rp5,000")
}

func TestPeriods_PostClose_ClosesWritesClosingAndOpensTheNextPeriod(t *testing.T) {
	f := newPeriodFixture(t, defaultCaps())
	cur := f.seedBooks(t)
	end := f.today(t)

	rec := f.do(t, http.MethodPost, f.path("/periods/"+cur.ID.String()+"/close"), url.Values{"end_date": {end.String()}})
	require.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, f.path("/periods"), rec.Header().Get("Location"))

	ctx := f.scoped(t)
	closed, err := f.Periods.ByID(ctx, cur.ID)
	require.NoError(t, err)
	assert.Equal(t, period.StatusClosed, closed.Status)
	require.NotNil(t, closed.EndDate)
	assert.Equal(t, end, *closed.EndDate)
	require.NotNil(t, closed.Snapshot)
	assert.Equal(t, int64(500_000), closed.Snapshot.Income)
	assert.Equal(t, int64(200_000), closed.Snapshot.Expense)

	closings, err := f.Periods.Closings(ctx, cur.ID)
	require.NoError(t, err)
	require.Len(t, closings, 1)
	assert.Equal(t, f.User.ID, closings[0].ClosedBy)

	next, err := f.Periods.Current(ctx)
	require.NoError(t, err)
	assert.NotEqual(t, cur.ID, next.ID)
	assert.Equal(t, end.AddDays(1), next.StartDate)

	page := f.do(t, http.MethodGet, f.path("/periods"), nil)
	require.Equal(t, http.StatusOK, page.Code)
	assert.Contains(t, page.Body.String(), f.path("/periods/"+cur.ID.String()+"/reopen"))
}

func TestPeriods_PostClose_DoubleSubmitRendersAlreadyClosedState(t *testing.T) {
	f := newPeriodFixture(t, defaultCaps())
	cur := f.seedBooks(t)
	end := f.today(t)
	form := url.Values{"end_date": {end.String()}}

	require.Equal(t, http.StatusSeeOther, f.do(t, http.MethodPost, f.path("/periods/"+cur.ID.String()+"/close"), form).Code)

	again := f.do(t, http.MethodPost, f.path("/periods/"+cur.ID.String()+"/close"), form)
	assert.Equal(t, http.StatusOK, again.Code, "a double submit is state, not an error page")
	body := again.Body.String()
	assert.Contains(t, body, "period.status_closed")
	assert.NotContains(t, body, "PRD005")
}

func TestPeriods_GetPeriods_ReopenOfferedOnlyOnTheLatestClosedPeriod(t *testing.T) {
	f := newPeriodFixture(t, defaultCaps())
	ctx := f.scoped(t)

	older := f.seedClosed(ctx, t, civil.Date{Year: 2026, Month: 7, Day: 1}, civil.Date{Year: 2026, Month: 7, Day: 31})
	latest := f.seedClosed(ctx, t, civil.Date{Year: 2026, Month: 8, Day: 1}, civil.Date{Year: 2026, Month: 8, Day: 31})
	current, err := period.New(f.Org.ID, f.Project.ID, civil.Date{Year: 2026, Month: 9, Day: 1}, "Sep 2026")
	require.NoError(t, err)
	require.NoError(t, f.PerStore.Save(ctx, current))

	rec := f.do(t, http.MethodGet, f.path("/periods"), nil)
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, f.path("/periods/"+latest.ID.String()+"/reopen"))
	assert.NotContains(t, body, f.path("/periods/"+older.ID.String()+"/reopen"))
}

func TestPeriods_ReopenThenRecloseRefusesADifferentDate(t *testing.T) {
	f := newPeriodFixture(t, defaultCaps())
	cur := f.seedBooks(t)
	end := f.today(t)
	closePath := f.path("/periods/" + cur.ID.String() + "/close")

	require.Equal(t, http.StatusSeeOther, f.do(t, http.MethodPost, closePath, url.Values{"end_date": {end.String()}}).Code)
	require.Equal(t, http.StatusSeeOther, f.do(t, http.MethodPost, f.path("/periods/"+cur.ID.String()+"/reopen"), url.Values{}).Code)

	reopened, err := f.Periods.ByID(f.scoped(t), cur.ID)
	require.NoError(t, err)
	require.Equal(t, period.StatusOpen, reopened.Status)
	require.NotNil(t, reopened.EndDate)

	page := f.do(t, http.MethodGet, closePath, nil)
	require.Equal(t, http.StatusOK, page.Code)
	assert.Contains(t, page.Body.String(), "disabled", "the frozen end date must not be editable")
	assert.Contains(t, page.Body.String(), "period.locked_hint")

	wrong := f.do(t, http.MethodPost, closePath, url.Values{"end_date": {end.AddDays(-1).String()}})
	assert.Equal(t, http.StatusOK, wrong.Code)
	assert.Contains(t, wrong.Body.String(), "PRD003")

	still, err := f.Periods.ByID(f.scoped(t), cur.ID)
	require.NoError(t, err)
	assert.Equal(t, period.StatusOpen, still.Status)

	right := f.do(t, http.MethodPost, closePath, url.Values{"end_date": {end.String()}})
	require.Equal(t, http.StatusSeeOther, right.Code)
	relocked, err := f.Periods.ByID(f.scoped(t), cur.ID)
	require.NoError(t, err)
	assert.Equal(t, period.StatusClosed, relocked.Status)

	closings, err := f.Periods.Closings(f.scoped(t), cur.ID)
	require.NoError(t, err)
	assert.Len(t, closings, 2, "a reopened period accumulates one closing per close")
}

func TestPeriods_PostRename_PersistsAndRefusesAnEmptyName(t *testing.T) {
	f := newPeriodFixture(t, defaultCaps())
	cur := f.seedBooks(t)
	renamePath := f.path("/periods/" + cur.ID.String() + "/rename")

	rec := f.do(t, http.MethodPost, renamePath, url.Values{"name": {"Gajian September"}})
	require.Equal(t, http.StatusSeeOther, rec.Code)
	got, err := f.Periods.ByID(f.scoped(t), cur.ID)
	require.NoError(t, err)
	assert.Equal(t, "Gajian September", got.Name)

	bad := f.do(t, http.MethodPost, renamePath, url.Values{"name": {"   "}})
	assert.Equal(t, http.StatusOK, bad.Code)
	assert.Contains(t, bad.Body.String(), "PRD002")
}

func TestSettings_PostSettings_PersistsTokyo(t *testing.T) {
	f := newPeriodFixture(t, defaultCaps())

	rec := f.do(t, http.MethodPost, f.path("/settings"), url.Values{
		"timezone":         {"Asia/Tokyo"},
		"currency":         {"IDR"},
		"period_start_day": {"25"},
	})
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "settings.saved")

	st, err := f.Ledgers.Get(f.scoped(t))
	require.NoError(t, err)
	assert.Equal(t, "Asia/Tokyo", st.Timezone)
	assert.Equal(t, 25, st.PeriodStartDay)
}

func TestSettings_PostSettings_TypedRefusalsRenderBanners(t *testing.T) {
	cases := []struct {
		name string
		form url.Values
		code string
	}{
		{"invalid timezone", url.Values{"timezone": {"Mars/Olympus"}, "currency": {"IDR"}, "period_start_day": {"1"}}, "LDG001"},
		{"invalid start day", url.Values{"timezone": {"Asia/Jakarta"}, "currency": {"IDR"}, "period_start_day": {"31"}}, "LDG002"},
		{"unknown currency", url.Values{"timezone": {"Asia/Jakarta"}, "currency": {"XYZ"}, "period_start_day": {"1"}}, "LDG003"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newPeriodFixture(t, defaultCaps())
			rec := f.do(t, http.MethodPost, f.path("/settings"), tc.form)
			require.Equal(t, http.StatusOK, rec.Code, "a typed refusal is a banner, never a 500")
			assert.Contains(t, rec.Body.String(), tc.code)

			st, err := f.Ledgers.Get(f.scoped(t))
			require.NoError(t, err)
			assert.Equal(t, ledger.DefaultTimezone, st.Timezone, "a refused patch leaves the stored settings alone")
		})
	}
}

func TestSettings_GetSettings_MCPBlockFollowsTheCapability(t *testing.T) {
	off := newPeriodFixture(t, defaultCaps())
	rec := off.do(t, http.MethodGet, off.path("/settings"), nil)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.NotContains(t, rec.Body.String(), "yasaku:write")

	caps := defaultCaps()
	caps.MCPEnabled = true
	on := newPeriodFixture(t, caps)
	rec = on.do(t, http.MethodGet, on.path("/settings"), nil)
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "http://localhost/mcp")
	assert.Contains(t, body, "yasaku:read")
	assert.Contains(t, body, "yasaku:write")
}

func (f *periodFixture) seedClosed(ctx context.Context, t *testing.T, start, end civil.Date) *period.Period {
	t.Helper()
	p, err := period.New(f.Org.ID, f.Project.ID, start, "")
	require.NoError(t, err)
	endCopy := end
	p.EndDate = &endCopy
	p.Status = period.StatusClosed
	closedAt := end.In(time.UTC).Add(3 * time.Hour)
	p.ClosedAt = &closedAt
	p.Snapshot = &period.Snapshot{Currency: money.IDR, Income: 400_000, Expense: 150_000, Net: 250_000, TxCount: 3}
	require.NoError(t, f.PerStore.Save(ctx, p))
	return p
}
