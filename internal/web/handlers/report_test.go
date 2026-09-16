package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/civil"
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
	"altalune.id/yasaku/internal/user"
	"altalune.id/yasaku/internal/web"
	"altalune.id/yasaku/internal/web/handlers"
	"altalune.id/yasaku/internal/web/templates"
	"altalune.id/yasaku/money"
)

type reportFixture struct {
	Deps     handlers.Deps
	Cfg      *config.Config
	Sessions session.Store
	Users    *user.Service
	Orgs     *org.Service
	Projects *project.Service
	Periods  *period.Service
	PerStore *fakes.Period
	Reports  *report.Service
	Reader   *fakes.ReportReader
}

type reportSnapshotter struct{}

func (reportSnapshotter) Snapshot(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (period.Snapshot, error) {
	return period.Snapshot{}, nil
}

func newReportFixture(t *testing.T) *reportFixture {
	t.Helper()
	cfg := &config.Config{}
	cfg.HTTP.BasePath = ""
	cfg.HTTP.StateSecret = "0123456789abcdef0123456789abcdef"
	cfg.HTTP.BaseURL = "http://localhost"
	caps := capabilities.Capabilities{OrgCreation: true, LocalIdentity: true}
	sessions := session.NewMemoryStore()

	users := user.NewService(fakes.NewUser(), user.GenesisConfig{}, discardLogger(), passthroughUnexpected())
	orgs := org.NewService(fakes.NewOrg(), caps, discardLogger(), passthroughUnexpected())
	projects := project.NewService(fakes.NewProject(), discardLogger(), passthroughUnexpected())
	ledgers := ledger.NewService(fakes.NewLedger(), discardLogger(), passthroughUnexpected())

	perStore := fakes.NewPeriod()
	uow := func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }
	periods := period.NewService(perStore, discardLogger(), passthroughUnexpected(),
		ledgers, reportSnapshotter{}, period.UnitOfWork(uow), time.Now)

	reader := fakes.NewReportReader()
	reports := report.NewService(reader, discardLogger(), passthroughUnexpected(), ledgers)

	return &reportFixture{
		Deps: handlers.Deps{
			Cfg: cfg, Caps: caps, Sessions: sessions, Logger: discardStdLogger(),
			Orgs: orgs, Projects: projects,
		},
		Cfg: cfg, Sessions: sessions, Users: users, Orgs: orgs, Projects: projects,
		Periods: periods, PerStore: perStore, Reports: reports, Reader: reader,
	}
}

type reportScope struct {
	orgSlug  string
	projSlug string
	orgID    uuid.UUID
	projID   uuid.UUID
	p        session.Principal
}

func (f *reportFixture) seedScope(t *testing.T) reportScope {
	t.Helper()
	ctx := context.Background()
	u, err := f.Users.Create(ctx, user.CreateRequest{Email: "r@b.co", Name: "R", Source: user.SourceLocal})
	require.NoError(t, err)
	o, err := f.Orgs.Create(ctx, org.CreateRequest{Slug: "acme", Name: "Acme", OwnerID: u.ID})
	require.NoError(t, err)
	octx := tenant.Into(ctx, tenant.Context{OrgID: o.ID, UserID: u.ID})
	pr, err := f.Projects.Create(octx, o.ID, "books", "Books")
	require.NoError(t, err)
	return reportScope{
		orgSlug: o.Slug, projSlug: pr.Slug, orgID: o.ID, projID: pr.ID,
		p: session.Principal{UserID: u.ID, ActiveOrgID: o.ID, ActiveProjectID: pr.ID, IssuedAt: time.Now().UTC()},
	}
}

func (f *reportFixture) seedPeriod(t *testing.T, sc reportScope, start civil.Date, name string, closed bool) *period.Period {
	t.Helper()
	p, err := period.New(sc.orgID, sc.projID, start, name)
	require.NoError(t, err)
	if closed {
		end := start.AddDays(29)
		p.EndDate = &end
		p.Status = period.StatusClosed
	}
	require.NoError(t, f.PerStore.Save(context.Background(), p))
	return p
}

func (f *reportFixture) authedRequest(t *testing.T, method, target string, p session.Principal) *http.Request {
	t.Helper()
	sid, err := web.NewSID()
	require.NoError(t, err)
	require.NoError(t, f.Sessions.Save(context.Background(), sid, p, time.Now().Add(web.SessionTTL)))
	r := httptest.NewRequest(method, target, nil)
	r.AddCookie(&http.Cookie{Name: web.SessionCookieName, Value: web.SignCookie([]byte(f.Cfg.HTTP.StateSecret), sid)})
	return r.WithContext(session.PrincipalInto(r.Context(), p))
}

func (f *reportFixture) get(t *testing.T, sc reportScope, query string) *httptest.ResponseRecorder {
	t.Helper()
	h := handlers.NewReportHandler(f.Deps, f.Projects, f.Reports, f.Periods)
	mux := http.NewServeMux()
	h.Register(mux)
	target := "/orgs/" + sc.orgSlug + "/projects/" + sc.projSlug + "/reports" + query
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, f.authedRequest(t, http.MethodGet, target, sc.p))
	return rec
}

func reportIDR(minor int64) money.Amount { return money.New(minor, money.IDR) }

func reportCatID() *uuid.UUID { id := uuid.Must(uuid.NewV7()); return &id }

// seedData fills the reader with a period's worth of movement: two wallets, seven expense
// categories (one of them uncategorized) so the top-5 fold is exercised, and flows for both wallets.
func (f *reportFixture) seedData(ref report.PeriodRef) {
	food, transport, rent, fun, health, pets := reportCatID(), reportCatID(), reportCatID(), reportCatID(), reportCatID(), reportCatID()
	walletCash, walletBank := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	f.Reader.Ref = ref
	f.Reader.Summaries = report.PeriodSummary{
		Period:   ref,
		Currency: money.IDR,
		Income:   reportIDR(900000000),
		Expense:  reportIDR(560000000),
		Net:      reportIDR(340000000),
		TxCount:  42,
		Wallets: []report.WalletLine{
			{WalletID: walletCash, Name: "Dompet Tunai", Kind: "cash",
				Opening: reportIDR(10000000), In: reportIDR(200000000), Out: reportIDR(160000000), Closing: reportIDR(50000000)},
			{WalletID: walletBank, Name: "BCA", Kind: "bank",
				Opening: reportIDR(500000000), In: reportIDR(700000000), Out: reportIDR(400000000), Closing: reportIDR(800000000)},
		},
		SpendableTotal: reportIDR(850000000),
		Total:          reportIDR(850000000),
	}
	f.Reader.Spend = []report.CategorySlice{
		{CategoryID: food, Name: "Makan & Minum", Amount: reportIDR(200000000), Share: 0.357, Count: 20},
		{CategoryID: transport, Name: "Transportasi", Amount: reportIDR(120000000), Share: 0.214, Count: 9},
		{CategoryID: rent, Name: "Sewa", Amount: reportIDR(100000000), Share: 0.179, Count: 1},
		{CategoryID: fun, Name: "Hiburan", Amount: reportIDR(60000000), Share: 0.107, Count: 5},
		{CategoryID: health, Name: "Kesehatan", Amount: reportIDR(40000000), Share: 0.071, Count: 3},
		{CategoryID: pets, Name: "Hewan", Amount: reportIDR(30000000), Share: 0.054, Count: 2},
		{CategoryID: nil, Name: "", Amount: reportIDR(10000000), Share: 0.018, Count: 2},
	}
	f.Reader.Income = []report.CategorySlice{
		{CategoryID: reportCatID(), Name: "Gaji", Amount: reportIDR(800000000), Share: 0.889, Count: 1},
		{CategoryID: reportCatID(), Name: "Bonus", Amount: reportIDR(100000000), Share: 0.111, Count: 1},
	}
	f.Reader.FlowLines = []report.Flow{
		{WalletID: walletBank, WalletName: "BCA", CategoryID: rent, CategoryName: "Sewa", Amount: reportIDR(100000000)},
		{WalletID: walletBank, WalletName: "BCA", CategoryID: food, CategoryName: "Makan & Minum", Amount: reportIDR(120000000)},
		{WalletID: walletCash, WalletName: "Dompet Tunai", CategoryID: food, CategoryName: "Makan & Minum", Amount: reportIDR(80000000)},
		{WalletID: walletCash, WalletName: "Dompet Tunai", CategoryID: transport, CategoryName: "Transportasi", Amount: reportIDR(120000000)},
		{WalletID: walletCash, WalletName: "Dompet Tunai", CategoryID: pets, CategoryName: "Hewan", Amount: reportIDR(30000000)},
		{WalletID: walletCash, WalletName: "Dompet Tunai", CategoryID: nil, CategoryName: "", Amount: reportIDR(10000000)},
	}
}

func (f *reportFixture) seedCashflow(refs ...report.PeriodRef) {
	points := make([]report.CashflowPoint, 0, len(refs))
	for i, ref := range refs {
		n := int64(i + 1)
		points = append(points, report.CashflowPoint{
			Period:  ref,
			Income:  reportIDR(n * 100000000),
			Expense: reportIDR(n * 60000000),
			Net:     reportIDR(n * 40000000),
		})
	}
	f.Reader.Points = points
}

type reportDonutItemJSON struct {
	Name      string `json:"name"`
	Value     int64  `json:"value"`
	Formatted string `json:"formatted"`
}

type reportDonutJSON struct {
	Items []reportDonutItemJSON `json:"items"`
	Empty string                `json:"empty"`
}

type reportBarsJSON struct {
	Categories []string            `json:"categories"`
	Income     []int64             `json:"income"`
	Expense    []int64             `json:"expense"`
	Net        []int64             `json:"net"`
	Formatted  map[string][]string `json:"formatted"`
	Labels     map[string]string   `json:"labels"`
	Unit       struct {
		Divisor int64  `json:"divisor"`
		Symbol  string `json:"symbol"`
	} `json:"unit"`
	Empty string `json:"empty"`
}

type reportSankeyJSON struct {
	Nodes []struct {
		Name string `json:"name"`
	} `json:"nodes"`
	Links []struct {
		Source    string `json:"source"`
		Target    string `json:"target"`
		Value     int64  `json:"value"`
		Formatted string `json:"formatted"`
	} `json:"links"`
	Empty string `json:"empty"`
}

func reportPayloadFor(t *testing.T, body, id string) []byte {
	t.Helper()
	re := regexp.MustCompile(`data-chart-for="([^"]+)">(.*?)</script>`)
	for _, m := range re.FindAllStringSubmatch(body, -1) {
		if m[1] == id {
			return []byte(m[2])
		}
	}
	t.Fatalf("no chart payload for %q in body", id)
	return nil
}

func TestReportHandler_GetReports_RendersEveryChart(t *testing.T) {
	t.Parallel()
	f := newReportFixture(t)
	sc := f.seedScope(t)
	cur := f.seedPeriod(t, sc, civil.Date{Year: 2026, Month: time.September, Day: 1}, "Sep 2026", false)
	prev := f.seedPeriod(t, sc, civil.Date{Year: 2026, Month: time.August, Day: 1}, "Aug 2026", true)
	ref := report.PeriodRef{ID: cur.ID, Name: cur.Name, Start: cur.StartDate}
	f.seedData(ref)
	f.seedCashflow(
		report.PeriodRef{ID: prev.ID, Name: prev.Name, Start: prev.StartDate},
		ref,
	)

	rec := f.get(t, sc, "")
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()

	assert.Contains(t, body, `data-chart="donut"`)
	assert.Contains(t, body, `data-chart="bars"`)
	assert.Contains(t, body, `data-chart="sankey"`)
	assert.Contains(t, body, "charts.js")
	assert.Contains(t, body, "Dompet Tunai")
	assert.NotContains(t, body, "/orgs/projects/", "ProjectPath must not collapse to /orgs")
}

func TestReportHandler_GetReports_DonutPayloadIsTheContractChartsJSReads(t *testing.T) {
	t.Parallel()
	f := newReportFixture(t)
	sc := f.seedScope(t)
	cur := f.seedPeriod(t, sc, civil.Date{Year: 2026, Month: time.September, Day: 1}, "Sep 2026", false)
	ref := report.PeriodRef{ID: cur.ID, Name: cur.Name, Start: cur.StartDate}
	f.seedData(ref)

	rec := f.get(t, sc, "")
	require.Equal(t, http.StatusOK, rec.Code)

	var got reportDonutJSON
	require.NoError(t, json.Unmarshal(reportPayloadFor(t, rec.Body.String(), templates.ReportDonutID), &got))

	require.Len(t, got.Items, 6, "five categories plus one aggregated Other slice")
	assert.Equal(t, "Makan & Minum", got.Items[0].Name)
	assert.Equal(t, int64(200000000), got.Items[0].Value)
	assert.Contains(t, got.Items[0].Formatted, "Rp")
	assert.Contains(t, got.Items[0].Formatted, "2")
	// The tail (Hewan + the uncategorized slice) folds into one aggregated slice.
	assert.Equal(t, int64(40000000), got.Items[5].Value)
	assert.NotEmpty(t, got.Empty)
	for _, it := range got.Items {
		assert.NotEmpty(t, it.Name)
		assert.Positive(t, it.Value)
	}
}

func TestReportHandler_GetReports_BarsPayloadRunsOldestFirst(t *testing.T) {
	t.Parallel()
	f := newReportFixture(t)
	sc := f.seedScope(t)
	cur := f.seedPeriod(t, sc, civil.Date{Year: 2026, Month: time.September, Day: 1}, "Sep 2026", false)
	prev := f.seedPeriod(t, sc, civil.Date{Year: 2026, Month: time.August, Day: 1}, "Aug 2026", true)
	ref := report.PeriodRef{ID: cur.ID, Name: cur.Name, Start: cur.StartDate}
	f.seedData(ref)
	f.seedCashflow(
		report.PeriodRef{ID: prev.ID, Name: prev.Name, Start: prev.StartDate},
		ref,
	)

	rec := f.get(t, sc, "")
	require.Equal(t, http.StatusOK, rec.Code)

	var got reportBarsJSON
	require.NoError(t, json.Unmarshal(reportPayloadFor(t, rec.Body.String(), templates.ReportBarsID), &got))
	assert.Equal(t, []string{"Aug 2026", "Sep 2026"}, got.Categories)
	assert.Equal(t, []int64{100000000, 200000000}, got.Income)
	assert.Equal(t, []int64{60000000, 120000000}, got.Expense)
	assert.Equal(t, []int64{40000000, 80000000}, got.Net)
	assert.Len(t, got.Formatted["income"], 2)
	assert.Contains(t, got.Formatted["income"][0], "Rp")
	assert.Equal(t, int64(100), got.Unit.Divisor)
	assert.Equal(t, "Rp", got.Unit.Symbol)
	assert.NotEmpty(t, got.Labels["income"])
	assert.NotEmpty(t, got.Labels["expense"])
	assert.NotEmpty(t, got.Labels["net"])

	// Cashflow must be asked for oldest-first so the x-axis reads left to right.
	call, ok := f.Reader.Last("Cashflow")
	require.True(t, ok)
	require.Len(t, call.PeriodIDs, 2)
	assert.Equal(t, prev.ID, call.PeriodIDs[0])
	assert.Equal(t, cur.ID, call.PeriodIDs[1])
}

func TestReportHandler_GetReports_SankeyIsAcyclicWalletToCategory(t *testing.T) {
	t.Parallel()
	f := newReportFixture(t)
	sc := f.seedScope(t)
	cur := f.seedPeriod(t, sc, civil.Date{Year: 2026, Month: time.September, Day: 1}, "Sep 2026", false)
	ref := report.PeriodRef{ID: cur.ID, Name: cur.Name, Start: cur.StartDate}
	f.seedData(ref)

	rec := f.get(t, sc, "")
	require.Equal(t, http.StatusOK, rec.Code)

	var got reportSankeyJSON
	require.NoError(t, json.Unmarshal(reportPayloadFor(t, rec.Body.String(), templates.ReportSankeyID), &got))

	names := map[string]bool{}
	for _, n := range got.Nodes {
		require.NotEmpty(t, n.Name)
		require.False(t, names[n.Name], "duplicate sankey node %q would merge two entities", n.Name)
		names[n.Name] = true
	}
	require.NotEmpty(t, got.Links)
	sources := map[string]bool{}
	targets := map[string]bool{}
	for _, l := range got.Links {
		assert.True(t, names[l.Source], "link source %q is not a node", l.Source)
		assert.True(t, names[l.Target], "link target %q is not a node", l.Target)
		assert.Positive(t, l.Value)
		assert.Contains(t, l.Formatted, "Rp")
		sources[l.Source] = true
		targets[l.Target] = true
	}
	for s := range sources {
		assert.False(t, targets[s], "node %q is both a source and a target — the sankey would cycle", s)
	}
	assert.Contains(t, names, "BCA")
	assert.Contains(t, names, "Makan & Minum")
}

func TestReportHandler_GetReports_PeriodQuerySelectsThatPeriod(t *testing.T) {
	t.Parallel()
	f := newReportFixture(t)
	sc := f.seedScope(t)
	cur := f.seedPeriod(t, sc, civil.Date{Year: 2026, Month: time.September, Day: 1}, "Sep 2026", false)
	prev := f.seedPeriod(t, sc, civil.Date{Year: 2026, Month: time.August, Day: 1}, "Aug 2026", true)
	f.seedData(report.PeriodRef{ID: prev.ID, Name: prev.Name, Start: prev.StartDate})

	rec := f.get(t, sc, "?period="+prev.ID.String())
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()

	call, ok := f.Reader.Last("SpendByCategory")
	require.True(t, ok)
	assert.Equal(t, prev.ID, call.PeriodID)

	assert.Contains(t, body, `value="`+prev.ID.String()+`" selected`)
	assert.NotContains(t, body, `value="`+cur.ID.String()+`" selected`)
	assert.Contains(t, body, "Aug 2026")
}

func TestReportHandler_GetReports_UnknownPeriodIs404(t *testing.T) {
	t.Parallel()
	f := newReportFixture(t)
	sc := f.seedScope(t)
	f.seedPeriod(t, sc, civil.Date{Year: 2026, Month: time.September, Day: 1}, "Sep 2026", false)

	rec := f.get(t, sc, "?period="+uuid.Must(uuid.NewV7()).String())
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestReportHandler_GetReports_EmptyProjectRendersEmptyStatesAndNoCharts(t *testing.T) {
	t.Parallel()
	f := newReportFixture(t)
	sc := f.seedScope(t)
	cur := f.seedPeriod(t, sc, civil.Date{Year: 2026, Month: time.September, Day: 1}, "Sep 2026", false)
	f.Reader.Ref = report.PeriodRef{ID: cur.ID, Name: cur.Name, Start: cur.StartDate}
	f.Reader.Summaries = report.PeriodSummary{Period: f.Reader.Ref, Currency: money.IDR}

	rec := f.get(t, sc, "")
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()

	assert.Contains(t, body, "report.no_data")
	assert.NotContains(t, body, "data-chart=")
	assert.NotContains(t, body, "data-chart-for=")
}

func TestReportHandler_GetReports_NoPeriodRendersThePeriodEmptyState(t *testing.T) {
	t.Parallel()
	f := newReportFixture(t)
	sc := f.seedScope(t)

	rec := f.get(t, sc, "")
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()

	assert.Contains(t, body, "period.no_period")
	assert.NotContains(t, body, "data-chart=")
	assert.Empty(t, f.Reader.Calls(), "a project with no period must not be reported on")
}

func TestReportHandler_GetReports_RedirectsAnonymousCaller(t *testing.T) {
	t.Parallel()
	f := newReportFixture(t)
	h := handlers.NewReportHandler(f.Deps, f.Projects, f.Reports, f.Periods)
	mux := http.NewServeMux()
	h.Register(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/orgs/acme/projects/books/reports", nil))
	assert.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, "/login", rec.Header().Get("Location"))
}

// TestReportHandler_GetReports_NeverEnsuresAPeriod pins the read-only contract: rendering the
// page must not create the project's first period the way EnsureCurrent would.
func TestReportHandler_GetReports_NeverEnsuresAPeriod(t *testing.T) {
	t.Parallel()
	f := newReportFixture(t)
	sc := f.seedScope(t)

	require.Equal(t, http.StatusOK, f.get(t, sc, "").Code)

	got, err := f.PerStore.List(context.Background(), sc.orgID, sc.projID, period.ListOpts{})
	require.NoError(t, err)
	assert.Empty(t, got)
}
