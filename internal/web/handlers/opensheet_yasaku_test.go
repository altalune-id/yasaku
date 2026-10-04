package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/i18n"
	"altalune.id/yasaku/internal/opensheetsync"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/platform/queue"
	"altalune.id/yasaku/internal/platform/sealer"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/testutil/fakes"
	"altalune.id/yasaku/internal/user"
	"altalune.id/yasaku/internal/wallet"
	"altalune.id/yasaku/internal/web"
	"altalune.id/yasaku/internal/web/handlers"
	"altalune.id/yasaku/internal/web/templates"
	"altalune.id/yasaku/money"
)

const opensheetTestKey = "osk_live_0123456789abcd"

type opensheetFixture struct {
	*walletFixture
	sheets *fakes.Opensheet
	store  opensheetsync.Store
	mux    *http.ServeMux
	jobs   *inlineJobs
}

// NOTE: inlineJobs runs a submitted job's real handler in the caller, as boot's submitter does with the queue off.
type inlineJobs struct {
	mu       sync.Mutex
	handlers []queue.Handler
}

func (q *inlineJobs) Submit(ctx context.Context, j queue.Job, data any) error {
	q.mu.Lock()
	hs := slices.Clone(q.handlers)
	q.mu.Unlock()
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	for _, h := range hs {
		if h.Job == j {
			_ = h.Handle(ctx, queue.Message{ID: uuid.New(), Name: j.Name, Version: j.Version, Data: raw})
		}
	}
	return nil
}

type walletOnlySource struct{ wallets *wallet.Service }

func (s walletOnlySource) Transaction(context.Context, uuid.UUID) (opensheetsync.TransactionFacts, bool, error) {
	return opensheetsync.TransactionFacts{}, false, nil
}

func (s walletOnlySource) Category(context.Context, uuid.UUID) (opensheetsync.CategoryFacts, bool, error) {
	return opensheetsync.CategoryFacts{}, false, nil
}

func (s walletOnlySource) Wallet(ctx context.Context, id uuid.UUID) (opensheetsync.WalletFacts, bool, error) {
	w, err := s.wallets.ByID(ctx, id)
	if wallet.IsNotFoundError(err) {
		return opensheetsync.WalletFacts{}, false, nil
	}
	if err != nil {
		return opensheetsync.WalletFacts{}, false, err
	}
	return opensheetsync.WalletFacts{ID: w.ID, Name: w.Name, Kind: string(w.Kind), Balance: money.New(0, w.Currency), UpdatedAt: w.UpdatedAt}, true, nil
}

func newOpensheetFixture(t *testing.T) *opensheetFixture {
	t.Helper()
	return newOpensheetFixtureWith(t, func(*opensheetsync.Endpoint) {})
}

func newOpensheetFixtureWith(t *testing.T, endpoint func(*opensheetsync.Endpoint)) *opensheetFixture {
	t.Helper()
	f := newWalletFixture(t)
	sheets := fakes.NewOpensheet(t, "acme", "home", opensheetTestKey)
	f.Deps.Cfg.Opensheet.BaseURL = sheets.URL()
	pool := db.Pool{W: f.DB, R: f.DB}
	store := opensheetsync.NewStore(db.DBConfig{Driver: db.DriverSQLite, TablePrefix: f.Cfg.DB.TablePrefix}, pool, nil)
	key, err := sealer.GenerateKey()
	require.NoError(t, err)
	sl, err := sealer.New(key)
	require.NoError(t, err)
	jobs := &inlineJobs{}
	mirror := opensheetsync.NewMirror(store, jobs, false, discardLogger(), passthroughUnexpected(), time.Now)
	ep := opensheetsync.Endpoint{BaseURL: sheets.URL(), AllowPrivateHosts: true, Timeout: 2 * time.Second}
	endpoint(&ep)
	syncer := opensheetsync.NewSyncer(store, discardLogger(), passthroughUnexpected(), opensheetsync.SyncerDeps{
		Sealer: sl, Endpoint: ep, Source: walletOnlySource{wallets: f.Wallets}, Jobs: jobs,
	})
	jobs.handlers = opensheetsync.NewConsumer(syncer).ConsumerHandlers()
	svc := opensheetsync.NewService(store, discardLogger(), passthroughUnexpected(), opensheetsync.ServiceDeps{
		Mirror: mirror, Members: f.Orgs, Sealer: sl,
		Endpoint:   ep,
		UnitOfWork: func(ctx context.Context, fn func(context.Context) error) error { return db.RunInTx(ctx, pool, fn) },
	})
	mux := http.NewServeMux()
	handlers.NewOpensheetHandler(f.Deps, svc).Register(mux)
	return &opensheetFixture{walletFixture: f, sheets: sheets, store: store, mux: mux, jobs: jobs}
}

func (f *opensheetFixture) publishAll(writable bool) {
	for _, tab := range opensheetsync.Contract() {
		f.sheets.AddSheet(tab.DefaultSlug, fakes.OpensheetSheet{Columns: tab.Columns, Writable: writable})
	}
}

func (f *opensheetFixture) tenantCtx() context.Context {
	return tenant.Into(context.Background(), tenant.Context{OrgID: f.OrgID, ProjectID: f.ProjectID, UserID: f.Principal.UserID})
}

func (f *opensheetFixture) seedLink(t *testing.T, sealed []byte) *opensheetsync.Link {
	t.Helper()
	now := time.Now().UTC()
	l := opensheetsync.NewLink(f.OrgID, f.ProjectID, f.Principal.UserID, uuid.Nil, now)
	l.Configure(opensheetsync.Settings{OSOrg: "acme", OSProject: "home", Sheets: opensheetsync.DefaultSheetSlugs()}, sealed, "abcd", now)
	require.NoError(t, f.store.SaveLink(f.tenantCtx(), l))
	return l
}

func (f *opensheetFixture) tr(key string, args ...any) string {
	return f.Deps.I18n.For(i18n.IdID).T(key, args...)
}

func opensheetForm(key string) url.Values {
	return url.Values{
		"os_org": {"acme"}, "os_project": {"home"}, "api_key": {key},
		"transactions_sheet": {"yasaku-transactions"}, "wallets_sheet": {"yasaku-wallets"}, "categories_sheet": {"yasaku-categories"},
	}
}

func TestOpensheet_PageShowsTheTutorialFromTheContractAndTheNavEntry(t *testing.T) {
	t.Parallel()
	f := newOpensheetFixture(t)
	rec := f.do(t, f.mux, http.MethodGet, "/opensheet", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body := rec.Body.String()
	for _, tab := range opensheetsync.Contract() {
		assert.Contains(t, body, `data-opensheet-tab="`+tab.DefaultSlug+`"`)
		assert.Contains(t, body, tab.HeaderTSV(), "the copy button carries the contract's header row")
	}
	assert.Contains(t, body, f.path("/opensheet"), "the project nav links the module")
	assert.Contains(t, body, `data-opensheet-enabled="false"`)
	assert.Contains(t, body, `disabled`, "the switch is disabled before a passing Save")
	assert.Contains(t, body, `autocomplete="new-password"`, "browsers must not autofill a saved password into the key field")
	assert.Contains(t, body, f.tr("opensheet.subtitle"))
}

func TestOpensheet_TestFailsThenPasses(t *testing.T) {
	t.Parallel()
	f := newOpensheetFixture(t)
	failing := f.doHTMX(t, f.mux, http.MethodPost, "/opensheet/test", opensheetForm(opensheetTestKey))
	require.Equal(t, http.StatusOK, failing.Code, "a failing tab is a result, not a refusal")
	assert.Contains(t, failing.Body.String(), `id="opensheet-checks"`)
	assert.Contains(t, failing.Body.String(), `data-ok="false"`)
	assert.Contains(t, failing.Body.String(), "OSL004")

	f.sheets.AddSheet("yasaku-transactions", fakes.OpensheetSheet{Columns: []string{"id", "date"}, Writable: true})
	nodel := f.doHTMX(t, f.mux, http.MethodPost, "/opensheet/test", opensheetForm(opensheetTestKey))
	assert.Contains(t, nodel.Body.String(), `data-opensheet-deleted-at-hint="yasaku-transactions"`, "the checklist says why deleted_at is needed")
	assert.Contains(t, nodel.Body.String(), "opensheet mungkin masih memakai baris judul lama", "a header fixed a moment ago may not be in opensheet yet")

	f.publishAll(true)
	passing := f.doHTMX(t, f.mux, http.MethodPost, "/opensheet/test", opensheetForm(opensheetTestKey))
	require.Equal(t, http.StatusOK, passing.Code)
	assert.NotContains(t, passing.Body.String(), `data-ok="false"`)
	assert.Contains(t, passing.Body.String(), `data-opensheet-columns-deferred="yasaku-transactions"`, "empty tabs pass and say the first sync checks their columns")
	assert.NotContains(t, passing.Body.String(), opensheetTestKey, "the key is never echoed")
}

func TestOpensheet_SaveIsRefusedWith422ThenAllowed(t *testing.T) {
	t.Parallel()
	f := newOpensheetFixture(t)
	f.publishAll(false)
	refused := f.doHTMX(t, f.mux, http.MethodPost, "/opensheet", opensheetForm(opensheetTestKey))
	require.Equal(t, http.StatusUnprocessableEntity, refused.Code)
	body := refused.Body.String()
	assert.True(t, strings.HasPrefix(strings.TrimSpace(body), `<div id="opensheet-checks"`), "only the checklist is swapped, so the typed form survives")
	assert.Contains(t, body, "OSL007")
	assert.NotContains(t, body, opensheetTestKey)

	f.publishAll(true)
	saved := f.doHTMX(t, f.mux, http.MethodPost, "/opensheet", opensheetForm(opensheetTestKey))
	require.Equal(t, http.StatusOK, saved.Code, saved.Body.String())
	body = saved.Body.String()
	assert.Contains(t, body, `id="opensheet-module"`)
	assert.Contains(t, body, `data-opensheet-notice="opensheet.saved"`)
	assert.Contains(t, body, "•••• abcd")
	assert.Contains(t, body, `data-opensheet-awaiting-first-sync="true"`, "no sync has landed since the Save")
	assert.NotContains(t, body, opensheetTestKey)
	st, err := f.store.LinkByProject(t.Context(), f.OrgID, f.ProjectID)
	require.NoError(t, err)
	require.NotEmpty(t, st.APIKeySealed)
	assert.NotContains(t, body, string(st.APIKeySealed), "the view never renders the sealed bytes (Revision 5)")

	on := f.doHTMX(t, f.mux, http.MethodPost, "/opensheet/enabled", url.Values{"enabled": {"true"}})
	require.Equal(t, http.StatusOK, on.Code, on.Body.String())
	assert.Contains(t, on.Body.String(), `data-opensheet-enabled="true"`)
	assert.NotContains(t, on.Body.String(), string(st.APIKeySealed))

	queued := f.doHTMX(t, f.mux, http.MethodPost, "/opensheet/sync", url.Values{})
	require.Equal(t, http.StatusOK, queued.Code, queued.Body.String())
	assert.Contains(t, queued.Body.String(), `data-opensheet-notice="opensheet.sync_done"`, "with the queue off the sync ran inline, so nothing is queued")

	removed := f.doHTMX(t, f.mux, http.MethodPost, "/opensheet/delete", url.Values{})
	require.Equal(t, http.StatusOK, removed.Code, removed.Body.String())
	_, err = f.store.LinkByProject(t.Context(), f.OrgID, f.ProjectID)
	assert.True(t, opensheetsync.IsLinkNotFoundError(err))
}

func TestOpensheet_InvalidSettingsAre422InTheChecklist(t *testing.T) {
	t.Parallel()
	f := newOpensheetFixture(t)
	form := opensheetForm(opensheetTestKey)
	form.Set("os_org", "Not A Slug")
	rec := f.doHTMX(t, f.mux, http.MethodPost, "/opensheet/test", form)
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.True(t, strings.HasPrefix(strings.TrimSpace(rec.Body.String()), `<div id="opensheet-checks"`))
	assert.Contains(t, rec.Body.String(), "OSL002")
}

func TestOpensheet_ActionsWithoutALinkAreAnErrorBanner(t *testing.T) {
	t.Parallel()
	f := newOpensheetFixture(t)
	rec := f.doHTMX(t, f.mux, http.MethodPost, "/opensheet/enabled", url.Values{"enabled": {"true"}})
	require.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), "OSL001")
	assert.NotContains(t, rec.Body.String(), `id="opensheet-status"`, "a 404 goes to #hx-error, not the status fragment")
}

func TestOpensheet_SyncWhileOffIs422InTheStatus(t *testing.T) {
	t.Parallel()
	f := newOpensheetFixture(t)
	f.seedLink(t, []byte("sealed"))
	rec := f.doHTMX(t, f.mux, http.MethodPost, "/opensheet/sync", url.Values{})
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.True(t, strings.HasPrefix(strings.TrimSpace(rec.Body.String()), `<section id="opensheet-status"`))
	assert.Contains(t, rec.Body.String(), "OSL011")
}

func TestOpensheet_NoJavaScriptFallbackRendersTheWholePage(t *testing.T) {
	t.Parallel()
	f := newOpensheetFixture(t)
	rec := f.do(t, f.mux, http.MethodPost, "/opensheet", opensheetForm(opensheetTestKey))
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), `id="opensheet-module"`)
	assert.Contains(t, rec.Body.String(), `value="acme"`, "typed values come back in the full page")
	assert.NotContains(t, rec.Body.String(), opensheetTestKey)
}

func TestOpensheet_NavEntryIsHiddenWhenTheServerDoesNotMountTheModule(t *testing.T) {
	t.Parallel()
	f := newWalletFixture(t)
	rec := f.do(t, f.txCategoryMux(t), http.MethodGet, "/categories", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.NotContains(t, rec.Body.String(), f.path("/opensheet"))
}

// NOTE: Revision 3 (C1): today's opensheet passes an empty tab, so the refusal of the first sync must be visible where the person re-runs Test.
func TestOpensheet_TheChecklistShowsTheLastSyncError(t *testing.T) {
	t.Parallel()
	f := newOpensheetFixture(t)
	f.publishAll(true)
	l := f.seedLink(t, []byte("sealed"))
	_, err := f.store.SaveOutcome(f.tenantCtx(), f.OrgID, f.ProjectID, opensheetsync.Outcome{At: time.Now().UTC(), Err: "opensheet refused a write to sheet yasaku-transactions: SHT014 unknown column note", LinkUpdatedAt: l.UpdatedAt})
	require.NoError(t, err)

	rec := f.doHTMX(t, f.mux, http.MethodPost, "/opensheet/test", opensheetForm(opensheetTestKey))
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, `data-opensheet-last-sync-error="true"`, "a passing Test of an empty tab must not hide why the first sync was refused")
	assert.Contains(t, body, "SHT014 unknown column note")
}

func TestOpensheet_StatusTellsFailingRowsFromGivenUpOnes(t *testing.T) {
	t.Parallel()
	f := newOpensheetFixture(t)
	ctx := f.tenantCtx()
	now := time.Now().UTC()
	f.seedLink(t, []byte("sealed"))
	waiting, failing, givenUp := uuid.New(), uuid.New(), uuid.New()
	refs := []opensheetsync.Ref{{Entity: opensheetsync.EntityWallet, ID: waiting}, {Entity: opensheetsync.EntityWallet, ID: failing}, {Entity: opensheetsync.EntityWallet, ID: givenUp}}
	require.NoError(t, f.store.Mark(ctx, f.OrgID, f.ProjectID, refs, now))
	refuse := func(r opensheetsync.Ref, times int) {
		at := now
		for range times {
			tok := uuid.New()
			_, ok, err := f.store.Claim(ctx, f.OrgID, f.ProjectID, r, tok, at, time.Minute)
			require.NoError(t, err)
			require.True(t, ok)
			require.NoError(t, f.store.Release(ctx, f.OrgID, f.ProjectID, r, tok, opensheetsync.Failure{Reason: "SHT016", At: at, Count: true, RetryAfter: at.Add(time.Second)}))
			at = at.Add(time.Minute)
		}
	}
	refuse(refs[1], 1)
	refuse(refs[2], opensheetsync.MaxRowAttempts)

	rec := f.do(t, f.mux, http.MethodGet, "/opensheet", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body := rec.Body.String()
	assert.Contains(t, body, `data-opensheet-pending="2"`, "a given-up row is no longer waiting")
	assert.Contains(t, body, `data-opensheet-failing="1"`)
	assert.Contains(t, body, `data-opensheet-given-up="1"`)
}

func TestOpensheet_AnUnreadableSavedKeyIsATranslatedBanner(t *testing.T) {
	t.Parallel()
	f := newOpensheetFixture(t)
	f.publishAll(true)
	f.seedLink(t, []byte("sealed"))

	rec := f.doHTMX(t, f.mux, http.MethodPost, "/opensheet/test", opensheetForm(""))
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	body := rec.Body.String()
	assert.True(t, strings.HasPrefix(strings.TrimSpace(body), `<div id="opensheet-checks"`))
	assert.Contains(t, body, f.tr("opensheet.error.key_unreadable"), "the banner is translated, not the English AppError message")
	assert.Contains(t, body, "OSL015")

	on := f.doHTMX(t, f.mux, http.MethodPost, "/opensheet/enabled", url.Values{"enabled": {"true"}})
	require.Equal(t, http.StatusUnprocessableEntity, on.Code, on.Body.String())
	assert.True(t, strings.HasPrefix(strings.TrimSpace(on.Body.String()), `<section id="opensheet-status"`))
	assert.Contains(t, on.Body.String(), f.tr("opensheet.error.key_unreadable"))
}

func TestOpensheet_AMemberSeesTheModuleReadOnly(t *testing.T) {
	t.Parallel()
	f := newOpensheetFixture(t)
	pool := db.Pool{W: f.DB, R: f.DB}
	users := user.NewService(user.NewStore(db.DBConfig{Driver: db.DriverSQLite, TablePrefix: f.Cfg.DB.TablePrefix}, pool), user.GenesisConfig{}, discardLogger(), passthroughUnexpected())
	u, err := users.Create(t.Context(), user.CreateRequest{Email: "member@yasaku.test", Name: "Member", Source: user.SourceLocal})
	require.NoError(t, err)
	_, err = f.Orgs.AddMember(tenant.Into(t.Context(), tenant.Context{OrgID: f.OrgID, UserID: f.Principal.UserID}), f.OrgID, u.ID, org.RoleMember)
	require.NoError(t, err)
	f.Principal.UserID = u.ID

	rec := f.do(t, f.mux, http.MethodGet, "/opensheet", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body := rec.Body.String()
	assert.Contains(t, body, `data-opensheet-read-only="1"`)
	assert.NotContains(t, body, f.path("/opensheet/test"), "a member gets no Test button")

	refused := f.doHTMX(t, f.mux, http.MethodPost, "/opensheet/test", opensheetForm(opensheetTestKey))
	assert.Equal(t, http.StatusForbidden, refused.Code)
}

func TestOpensheet_AnAutoDisabledLinkSaysSoWithItsLastError(t *testing.T) {
	t.Parallel()
	f := newOpensheetFixture(t)
	l := f.seedLink(t, []byte("sealed"))
	require.NoError(t, l.Enable(l.UpdatedAt))
	require.NoError(t, f.store.SaveLink(f.tenantCtx(), l))
	disabled := false
	for i := range opensheetsync.DisableAfter {
		var err error
		disabled, err = f.store.SaveOutcome(f.tenantCtx(), f.OrgID, f.ProjectID, opensheetsync.Outcome{
			At: time.Now().UTC().Add(time.Duration(i) * time.Second), Err: "opensheet refused a write to sheet yasaku-transactions: SHT009 sheet is read only", LinkUpdatedAt: l.UpdatedAt,
		})
		require.NoError(t, err)
	}
	require.True(t, disabled, "DisableAfter link-level refusals turn the link off")

	rec := f.do(t, f.mux, http.MethodGet, "/opensheet", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body := rec.Body.String()
	assert.Contains(t, body, `data-opensheet-auto-disabled="1"`)
	assert.Contains(t, body, `data-opensheet-status-last-error="true"`)
	assert.Contains(t, body, "SHT009 sheet is read only")
	assert.Contains(t, body, `data-opensheet-enabled="false"`)
}

func TestOpensheet_StatusCountsArePlural(t *testing.T) {
	t.Parallel()
	f := newOpensheetFixture(t)
	v := templates.OpensheetView{ProjectSlug: "p", Linked: true, Pending: 1, Failing: 1, GivenUp: 1}
	render := func(loc i18n.Locale, v templates.OpensheetView) string {
		var b strings.Builder
		d := web.LayoutData{Translator: f.Deps.I18n.For(loc), Locale: loc}
		require.NoError(t, templates.OpensheetStatus(d, v).Render(t.Context(), &b))
		return b.String()
	}
	one := render(i18n.EnUS, v)
	assert.Contains(t, one, "1 row waiting")
	assert.Contains(t, one, "1 row failing, retrying")
	assert.Contains(t, one, "1 row given up")
	v.Pending, v.Failing, v.GivenUp = 3, 3, 3
	many := render(i18n.EnUS, v)
	assert.Contains(t, many, "3 rows waiting")
	assert.Contains(t, many, "3 rows failing, retrying")
	assert.Contains(t, many, "3 rows given up")
	v.Pending = 2
	assert.Contains(t, render(i18n.ArSA, v), "صفّان في الانتظار", "ar-SA picks its dual form")
}

func TestOpensheet_CodeLikeContentReadsLeftToRightAndTheLastSyncHasASpace(t *testing.T) {
	t.Parallel()
	f := newOpensheetFixture(t)
	l := f.seedLink(t, []byte("sealed"))
	_, err := f.store.SaveOutcome(f.tenantCtx(), f.OrgID, f.ProjectID, opensheetsync.Outcome{At: time.Now().UTC(), LinkUpdatedAt: l.UpdatedAt})
	require.NoError(t, err)

	rec := f.do(t, f.mux, http.MethodGet, "/opensheet", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body := rec.Body.String()
	for _, tab := range opensheetsync.Contract() {
		assert.Regexp(t, `<code[^>]*dir="ltr"[^>]*data-opensheet-header="`+tab.DefaultSlug+`"`, body, "a header row reads from id even in RTL")
	}
	assert.Regexp(t, `<ul[^>]*dir="ltr"[^>]*data-opensheet-slugs`, body)
	for _, name := range []string{"os_org", "os_project", "api_key", "transactions_sheet", "wallets_sheet", "categories_sheet"} {
		assert.Regexp(t, `<input[^>]*name="`+name+`"[^>]*dir="ltr"`, body, "%s is typed left to right", name)
	}
	assert.Contains(t, body, f.tr("opensheet.status.last_sync")+" <time", "the label and the date are apart")
	copyBtn := regexp.MustCompile(`<button[^>]*data-copy-text="`+regexp.QuoteMeta(f.tr("opensheet.tutorial.copy_header"))+`"[^>]*>`).FindAllString(body, -1)
	require.Len(t, copyBtn, len(opensheetsync.Contract()))
	for _, tag := range copyBtn {
		assert.NotContains(t, tag, "font-mono", "a translated label in a mono font breaks Arabic joining")
		assert.NotContains(t, tag, `dir="ltr"`)
	}
}

func TestOpensheet_TheSwitchKnobUsesLogicalMarginsSoRTLMirrorsIt(t *testing.T) {
	t.Parallel()
	f := newOpensheetFixture(t)
	render := func(on bool) string {
		var b strings.Builder
		d := web.LayoutData{Translator: f.Deps.I18n.For(i18n.ArSA), Locale: i18n.ArSA, Dir: "rtl"}
		require.NoError(t, templates.OpensheetStatus(d, templates.OpensheetView{ProjectSlug: "p", Enabled: on, Verified: true, CanManage: true}).Render(t.Context(), &b))
		return b.String()
	}
	off, on := render(false), render(true)
	assert.Regexp(t, `data-opensheet-knob[^>]*class="[^"]*\bms-0\.5\b`, off)
	assert.Regexp(t, `data-opensheet-knob[^>]*class="[^"]*\bms-5\b`, on)
	assert.NotContains(t, off+on, "translate-x", "a physical translate does not mirror in RTL")
}

func TestOpensheet_APrivateBaseURLSaysWhichServerFlagToSet(t *testing.T) {
	t.Parallel()
	f := newOpensheetFixtureWith(t, func(e *opensheetsync.Endpoint) {
		e.BaseURL = strings.Replace(e.BaseURL, "127.0.0.1", "localhost", 1)
		e.AllowPrivateHosts = false
	})
	f.publishAll(true)

	rec := f.doHTMX(t, f.mux, http.MethodPost, "/opensheet/test", opensheetForm(opensheetTestKey))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body := rec.Body.String()
	assert.Equal(t, len(opensheetsync.Contract()), strings.Count(body, `data-opensheet-private-endpoint=`), "every tab says why")
	assert.Contains(t, body, f.tr("opensheet.check.private_endpoint"))
	assert.Contains(t, body, "YASAKU_OPENSHEET_ALLOW_PRIVATE_HOSTS")
	assert.NotContains(t, body, f.tr("opensheet.check.unreachable_hint"), "the key and slug are not at fault")
	assert.Contains(t, body, "OSL016")
}

func TestOpensheet_SyncNowSaysQueuedOnlyWhenTheQueueIsOn(t *testing.T) {
	t.Parallel()
	f := newOpensheetFixture(t)
	f.publishAll(true)
	f.Cfg.Queue.Enabled = true
	saved := f.doHTMX(t, f.mux, http.MethodPost, "/opensheet", opensheetForm(opensheetTestKey))
	require.Equal(t, http.StatusOK, saved.Code, saved.Body.String())
	on := f.doHTMX(t, f.mux, http.MethodPost, "/opensheet/enabled", url.Values{"enabled": {"true"}})
	require.Equal(t, http.StatusOK, on.Code, on.Body.String())

	rec := f.doHTMX(t, f.mux, http.MethodPost, "/opensheet/sync", url.Values{})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `data-opensheet-notice="opensheet.sync_queued"`)
}

func TestOpensheet_AnErrorFromBeforeTheLastSaveIsNotShown(t *testing.T) {
	t.Parallel()
	f := newOpensheetFixture(t)
	f.publishAll(true)
	l := f.seedLink(t, []byte("sealed"))
	_, err := f.store.SaveOutcome(f.tenantCtx(), f.OrgID, f.ProjectID, opensheetsync.Outcome{At: time.Now().UTC(), Err: "SHT001 sheet not found", LinkUpdatedAt: l.UpdatedAt})
	require.NoError(t, err)
	l, err = f.store.LinkByProject(f.tenantCtx(), f.OrgID, f.ProjectID)
	require.NoError(t, err)
	require.Equal(t, 1, l.FailureStreak)
	l.Configure(l.Settings(), nil, "", time.Now().UTC().Add(time.Second))
	require.NoError(t, f.store.SaveLink(f.tenantCtx(), l))

	page := f.do(t, f.mux, http.MethodGet, "/opensheet", nil)
	require.Equal(t, http.StatusOK, page.Code, page.Body.String())
	assert.Contains(t, page.Body.String(), `data-opensheet-awaiting-first-sync="true"`)
	assert.NotContains(t, page.Body.String(), "SHT001", "a passing Save since the refusal makes it history")

	test := f.doHTMX(t, f.mux, http.MethodPost, "/opensheet/test", opensheetForm(opensheetTestKey))
	require.Equal(t, http.StatusOK, test.Code)
	assert.NotContains(t, test.Body.String(), `data-opensheet-last-sync-error`)
}

func TestOpensheet_ARefusalSinceTheSaveStaysVisibleWhileAwaitingTheFirstSync(t *testing.T) {
	t.Parallel()
	f := newOpensheetFixture(t)
	l := f.seedLink(t, []byte("sealed"))
	_, err := f.store.SaveOutcome(f.tenantCtx(), f.OrgID, f.ProjectID, opensheetsync.Outcome{At: time.Now().UTC(), Err: "SHT014 unknown column note", LinkUpdatedAt: l.UpdatedAt})
	require.NoError(t, err)

	page := f.do(t, f.mux, http.MethodGet, "/opensheet", nil)
	require.Equal(t, http.StatusOK, page.Code, page.Body.String())
	body := page.Body.String()
	assert.Contains(t, body, `data-opensheet-awaiting-first-sync="true"`)
	assert.Contains(t, body, `data-opensheet-status-last-error="true"`, "the first sync after the save was refused, and the person must see why")
	assert.Contains(t, body, "SHT014 unknown column note")
}

func TestOpensheet_AStrictlyPassingTestDoesNotRepeatTheLastSyncError(t *testing.T) {
	t.Parallel()
	f := newOpensheetFixture(t)
	f.sheets.SetHeaderColumns(true)
	f.publishAll(true)
	l := f.seedLink(t, []byte("sealed"))
	_, err := f.store.SaveOutcome(f.tenantCtx(), f.OrgID, f.ProjectID, opensheetsync.Outcome{At: time.Now().UTC(), Err: "SHT001 sheet not found", LinkUpdatedAt: l.UpdatedAt})
	require.NoError(t, err)

	pass := f.doHTMX(t, f.mux, http.MethodPost, "/opensheet/test", opensheetForm(opensheetTestKey))
	require.Equal(t, http.StatusOK, pass.Code)
	require.NotContains(t, pass.Body.String(), `data-ok="false"`)
	require.NotContains(t, pass.Body.String(), `data-opensheet-columns-deferred`)
	assert.NotContains(t, pass.Body.String(), `data-opensheet-last-sync-error`, "every column was checked and passed; the old refusal would contradict that")

	f.sheets.AddSheet("yasaku-wallets", fakes.OpensheetSheet{Columns: []string{"id", "name"}, Writable: true})
	fail := f.doHTMX(t, f.mux, http.MethodPost, "/opensheet/test", opensheetForm(opensheetTestKey))
	require.Equal(t, http.StatusOK, fail.Code)
	assert.Contains(t, fail.Body.String(), `data-ok="false"`)
	assert.Contains(t, fail.Body.String(), `data-opensheet-last-sync-error="true"`, "next to a failing tab the refusal is context")
}

func (f *opensheetFixture) linkWithAWallet(t *testing.T) {
	t.Helper()
	f.publishAll(true)
	saved := f.doHTMX(t, f.mux, http.MethodPost, "/opensheet", opensheetForm(opensheetTestKey))
	require.Equal(t, http.StatusOK, saved.Code, saved.Body.String())
	on := f.doHTMX(t, f.mux, http.MethodPost, "/opensheet/enabled", url.Values{"enabled": {"true"}})
	require.Equal(t, http.StatusOK, on.Code, on.Body.String())
	_, _, err := f.WalletOpen.Run(f.scoped(t), wallet.Params{Name: "Cash", Kind: wallet.KindCash, Currency: money.IDR}, nil, time.Now().UTC())
	require.NoError(t, err)
}

func TestOpensheet_InlineSyncNowSaysDoneOnlyWhenNothingWasRefused(t *testing.T) {
	t.Parallel()
	f := newOpensheetFixture(t)
	f.linkWithAWallet(t)

	rec := f.doHTMX(t, f.mux, http.MethodPost, "/opensheet/sync", url.Values{})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body := rec.Body.String()
	assert.Contains(t, body, `data-opensheet-notice="opensheet.sync_done"`)
	assert.NotContains(t, body, `data-opensheet-status-last-error`)
	assert.Len(t, f.sheets.Rows("yasaku-wallets"), 1, "the inline job pushed the wallet")
}

func TestOpensheet_InlineSyncNowRefusedAtLinkLevelShowsTheErrorNotDone(t *testing.T) {
	t.Parallel()
	f := newOpensheetFixture(t)
	f.linkWithAWallet(t)
	f.sheets.FailNext(http.MethodPost, "yasaku-wallets", http.StatusBadRequest, "SHT014", 10)

	rec := f.doHTMX(t, f.mux, http.MethodPost, "/opensheet/sync", url.Values{})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body := rec.Body.String()
	assert.NotContains(t, body, `data-opensheet-notice="opensheet.sync_done"`, "a refused inline sync is not done")
	assert.NotContains(t, body, f.tr("opensheet.sync_done"))
	assert.Contains(t, body, `data-opensheet-status-last-error="true"`)
	assert.Contains(t, body, "SHT014")
}

func TestOpensheet_ASuccessfulSyncClearsTheLastErrorFromTheStatus(t *testing.T) {
	t.Parallel()
	f := newOpensheetFixture(t)
	l := f.seedLink(t, []byte("sealed"))
	at := time.Now().UTC()
	_, err := f.store.SaveOutcome(f.tenantCtx(), f.OrgID, f.ProjectID, opensheetsync.Outcome{At: at, Err: "SHT014 unknown column note", LinkUpdatedAt: l.UpdatedAt})
	require.NoError(t, err)
	_, err = f.store.SaveOutcome(f.tenantCtx(), f.OrgID, f.ProjectID, opensheetsync.Outcome{At: at.Add(time.Second), LinkUpdatedAt: l.UpdatedAt})
	require.NoError(t, err)

	rec := f.do(t, f.mux, http.MethodGet, "/opensheet", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.NotContains(t, rec.Body.String(), `data-opensheet-status-last-error`)
	assert.NotContains(t, rec.Body.String(), "SHT014")
}
