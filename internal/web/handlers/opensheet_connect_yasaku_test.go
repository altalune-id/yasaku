package handlers_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/altalune-id/oidcrp"
	"github.com/altalune-id/oidcrp/oidcrptest"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/i18n"
	"altalune.id/yasaku/internal/opensheetsync"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform/audit"
	"altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/platform/sealer"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/testutil/fakes"
	"altalune.id/yasaku/internal/user"
	"altalune.id/yasaku/internal/web"
	"altalune.id/yasaku/internal/web/handlers"
)

const opensheetAudience = "https://opensheet.test/api"

type connectFixture struct {
	*opensheetFixture
	idp    *oidcrptest.IDP
	auth   *oidcrp.Client
	grants opensheetsync.GrantStore
	sealer sealer.Sealer
	audit  *audit.Recorder
	pool   db.Pool
	svc    *opensheetsync.Service
}

// NOTE: noRefreshAuth is the real client with the refresh token dropped, as an issuer that ignores offline_access answers.
type noRefreshAuth struct{ *oidcrp.Client }

func (a noRefreshAuth) ExchangeTokens(ctx context.Context, cb oidcrp.Callback, req oidcrp.AuthRequest) (*oidcrp.TokenSet, error) {
	set, err := a.Client.ExchangeTokens(ctx, cb, req)
	if set != nil {
		set.RefreshToken = ""
	}
	return set, err
}

func newConnectFixture(t *testing.T) *connectFixture {
	t.Helper()
	return newConnectFixtureWith(t, nil)
}

func newConnectFixtureWith(t *testing.T, wrap func(*oidcrp.Client) handlers.OpensheetAuth) *connectFixture {
	t.Helper()
	base := newOpensheetFixture(t)
	base.Loc = i18n.EnUS
	idp := oidcrptest.New(t)
	auth := oidcrp.New(oidcrp.Config{
		Issuer: idp.Issuer(), ClientID: "yasaku", RedirectURI: "http://localhost/opensheet/oauth/callback",
		Scopes:   []string{"sheets:read", "sheets:write", "spreadsheets:read", "spreadsheets:write", "sheets:admin", "credentials:read", "offline_access"},
		Resource: opensheetAudience, HTTPClient: idp.Client(),
	})
	pool := db.Pool{W: base.DB, R: base.DB}
	dbCfg := db.DBConfig{Driver: db.DriverSQLite, TablePrefix: base.Cfg.DB.TablePrefix}
	key, err := sealer.GenerateKey()
	require.NoError(t, err)
	sl, err := sealer.New(key)
	require.NoError(t, err)
	grants := opensheetsync.NewGrantStore(dbCfg, pool, nil)
	rec := audit.NewRecorder(dbCfg, pool, nil)
	mirror := opensheetsync.NewMirror(base.store, base.jobs, false, discardLogger(), passthroughUnexpected(), time.Now)
	svc := opensheetsync.NewService(base.store, discardLogger(), passthroughUnexpected(), opensheetsync.ServiceDeps{
		Mirror: mirror, Sealer: sl, Credentials: opensheetsync.NewCredentials(sl, grants, auth, discardLogger()),
		Endpoint:   opensheetsync.Endpoint{BaseURL: base.sheets.URL(), AllowPrivateHosts: true, Timeout: 2 * time.Second},
		UnitOfWork: func(ctx context.Context, fn func(context.Context) error) error { return db.RunInTx(ctx, pool, fn) },
		Audit:      rec,
	})
	var ha handlers.OpensheetAuth = auth
	if wrap != nil {
		ha = wrap(auth)
	}
	base.Deps.Cfg.Opensheet.Audience = opensheetAudience
	mux := http.NewServeMux()
	handlers.NewOpensheetHandler(base.Deps, svc, ha).Register(mux)
	base.mux = mux
	return &connectFixture{opensheetFixture: base, idp: idp, auth: auth, grants: grants, sealer: sl, audit: rec, pool: pool, svc: svc}
}

func (f *connectFixture) send(t *testing.T, method, path string, p session.Principal, cookies []*http.Cookie, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	sid, err := web.NewSID()
	require.NoError(t, err)
	require.NoError(t, f.Sessions.Save(t.Context(), sid, p, time.Now().Add(web.SessionTTL)))
	var r *http.Request
	if form != nil {
		r = httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	r.AddCookie(&http.Cookie{Name: web.SessionCookieName, Value: web.SignCookie(f.Deps.Key(web.PurposeSession), sid)})
	for _, c := range cookies {
		r.AddCookie(c)
	}
	r = r.WithContext(f.withLocale(session.PrincipalInto(r.Context(), p)))
	rec := httptest.NewRecorder()
	f.mux.ServeHTTP(rec, r)
	return rec
}

func (f *connectFixture) start(t *testing.T, p session.Principal, projectSlug string) (string, []*http.Cookie) {
	t.Helper()
	rec := f.send(t, http.MethodPost, "/orgs/"+walletFixtureOrgSlug+"/projects/"+projectSlug+"/opensheet/connect", p, nil, url.Values{})
	require.Equal(t, http.StatusFound, rec.Code, rec.Body.String())
	return rec.Header().Get("Location"), rec.Result().Cookies()
}

func (f *connectFixture) callback(t *testing.T, p session.Principal, q url.Values, cookies []*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	return f.send(t, http.MethodGet, "/opensheet/oauth/callback?"+q.Encode(), p, cookies, nil)
}

func (f *connectFixture) connect(t *testing.T) {
	t.Helper()
	target, cookies := f.start(t, f.Principal, walletFixtureProjectSlug)
	rec := f.callback(t, f.Principal, f.idp.Authorize(target), cookies)
	require.Equal(t, http.StatusSeeOther, rec.Code, rec.Body.String())
	require.Equal(t, f.path("/opensheet"), rec.Header().Get("Location"))
}

func (f *connectFixture) grantOf(t *testing.T, projectID uuid.UUID) (*opensheetsync.Grant, bool) {
	t.Helper()
	g, err := f.grants.GrantByProject(f.tenantCtx(), f.OrgID, projectID)
	if opensheetsync.IsGrantNotFoundError(err) {
		return nil, false
	}
	require.NoError(t, err)
	return g, true
}

func (f *connectFixture) actions(t *testing.T) []string {
	t.Helper()
	entries, err := f.audit.List(f.tenantCtx(), audit.Filter{})
	require.NoError(t, err)
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Action)
	}
	return out
}

func (f *connectFixture) addOpensheetProject() {
	f.sheets.AddProject(fakes.OpensheetProject{ID: "osp-1", OrgID: "oso-1", OrgSlug: "acme", Slug: "home", Name: "Home"},
		fakes.OpensheetCredential{ID: "cred-1", Name: "Ana", Kind: "google_oauth", Email: "ana@gmail.com"})
}

func (f *connectFixture) seedGrant(t *testing.T, projectID uuid.UUID, status opensheetsync.GrantStatus) {
	t.Helper()
	g, err := opensheetsync.NewGrant(f.sealer, f.OrgID, projectID, f.Principal.UserID, oidcrp.TokenSet{
		AccessToken: "access-1", RefreshToken: "refresh-1", ExpiresAt: time.Now().Add(time.Hour),
	}, time.Now())
	require.NoError(t, err)
	g.Status = status
	require.NoError(t, f.grants.SaveGrant(f.tenantCtx(), g))
}

func (f *connectFixture) seedGrantLink(t *testing.T) {
	t.Helper()
	now := time.Now().UTC()
	l := opensheetsync.NewLink(f.OrgID, f.ProjectID, f.Principal.UserID, uuid.Nil, now)
	l.Credential = opensheetsync.CredentialGrant
	l.Configure(opensheetsync.Settings{OSOrg: "acme", OSProject: "home", Sheets: opensheetsync.ProvisionSlugs(f.ProjectID)}, nil, "", now)
	require.NoError(t, l.Enable(now))
	require.NoError(t, f.store.SaveLink(f.tenantCtx(), l))
}

func (f *connectFixture) en(key string, args ...any) string {
	return html.EscapeString(f.Deps.I18n.For(i18n.EnUS).T(key, args...))
}

func (f *connectFixture) users() *user.Service {
	return user.NewService(user.NewStore(db.DBConfig{Driver: db.DriverSQLite, TablePrefix: f.Cfg.DB.TablePrefix}, f.pool), user.GenesisConfig{}, discardLogger(), passthroughUnexpected())
}

func (f *connectFixture) member(t *testing.T, email string, role org.Role) session.Principal {
	t.Helper()
	u, err := f.users().Create(t.Context(), user.CreateRequest{Email: email, Name: email, Source: user.SourceLocal})
	require.NoError(t, err)
	_, err = f.Orgs.AddMember(f.tenantCtx(), f.OrgID, u.ID, role)
	require.NoError(t, err)
	return session.Principal{UserID: u.ID, ActiveOrgID: f.OrgID, ActiveProjectID: f.ProjectID, IssuedAt: time.Now().UTC()}
}

func TestConnect_StartsWithResourceAndScopes(t *testing.T) {
	t.Parallel()
	f := newConnectFixture(t)
	target, cookies := f.start(t, f.Principal, walletFixtureProjectSlug)
	u, err := url.Parse(target)
	require.NoError(t, err)
	assert.Equal(t, opensheetAudience, u.Query().Get("resource"))
	assert.Equal(t, "sheets:read sheets:write spreadsheets:read spreadsheets:write sheets:admin credentials:read offline_access", u.Query().Get("scope"))
	require.NotEmpty(t, cookies)
	assert.Equal(t, int((10 * time.Minute).Seconds()), cookies[0].MaxAge)
}

func TestConnect_NonManagerRefused(t *testing.T) {
	t.Parallel()
	f := newConnectFixture(t)
	p := f.member(t, "member@yasaku.test", org.RoleMember)
	rec := f.send(t, http.MethodPost, f.path("/opensheet/connect"), p, nil, url.Values{})
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestConnect_SavesGrantThenPickerProvisions(t *testing.T) {
	t.Parallel()
	f := newConnectFixture(t)
	f.addOpensheetProject()
	f.connect(t)
	g, ok := f.grantOf(t, f.ProjectID)
	require.True(t, ok, "the callback saves the grant")
	assert.Equal(t, f.Principal.UserID, g.ConnectedBy)

	page := f.send(t, http.MethodGet, f.path("/opensheet"), f.Principal, nil, nil)
	require.Equal(t, http.StatusOK, page.Code)
	body := page.Body.String()
	assert.Contains(t, body, f.en("opensheet.picker.title"))
	assert.Contains(t, body, `value="osp-1" selected`, "a single project is preselected")
	assert.Contains(t, body, `value="cred-1" selected`, "a single Google account is preselected")
	assert.Contains(t, body, f.en("opensheet.picker.copy_note"))

	rec := f.send(t, http.MethodPost, f.path("/opensheet/provision"), f.Principal, nil, url.Values{"os_project_id": {"osp-1"}, "credential_id": {"cred-1"}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	l, err := f.store.LinkByProject(f.tenantCtx(), f.OrgID, f.ProjectID)
	require.NoError(t, err)
	assert.Equal(t, opensheetsync.CredentialGrant, l.Credential)
	assert.True(t, l.Enabled)
	assert.Equal(t, "acme", l.OSOrg)
	assert.Equal(t, "home", l.OSProject)
	assert.Empty(t, l.APIKeySealed)

	prov := f.sheets.Provisions()
	require.Len(t, prov, 1)
	assert.Equal(t, "yasaku:"+f.ProjectID.String(), prov[0].IdempotencyKey)
	assert.Equal(t, "Yasaku Project · Yasaku records", prov[0].Title)
	assert.Equal(t, "cred-1", prov[0].CredentialID)
	assert.Equal(t, "Yasaku Project · Yasaku records", l.SheetTitle)
	assert.Equal(t, "gf-yasaku:"+f.ProjectID.String(), l.GoogleFileID)

	page = f.send(t, http.MethodGet, f.path("/opensheet"), f.Principal, nil, nil)
	body = page.Body.String()
	assert.Contains(t, body, f.en("opensheet.linked_sheet", "Title", "Yasaku Project · Yasaku records"))
	assert.Contains(t, body, `href="https://docs.google.com/spreadsheets/d/gf-yasaku:`+f.ProjectID.String()+`" target="_blank" rel="noopener"`)
	assert.NotContains(t, body, `id="opensheet-form"`, "a grant link hides the API-key setup")
}

func TestKeySaveOntoGrantLinkRefused(t *testing.T) {
	t.Parallel()
	f := newConnectFixture(t)
	f.publishAll(true)
	f.seedGrantLink(t)
	f.seedGrant(t, f.ProjectID, opensheetsync.GrantOK)
	rec := f.send(t, http.MethodPost, f.path("/opensheet"), f.Principal, nil, opensheetForm(opensheetTestKey))
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	l, err := f.store.LinkByProject(f.tenantCtx(), f.OrgID, f.ProjectID)
	require.NoError(t, err)
	assert.Equal(t, opensheetsync.CredentialGrant, l.Credential)
	assert.Empty(t, l.APIKeySealed, "no key lands on a grant link")
}

func TestReconnect_RevokesPriorGrantAfterCommit(t *testing.T) {
	t.Parallel()
	f := newConnectFixture(t)
	f.seedGrantLink(t)
	f.seedGrant(t, f.ProjectID, opensheetsync.GrantReconnectNeeded)
	f.connect(t)
	assert.Equal(t, 1, f.idp.Revocations(), "the replaced refresh token is revoked at the upstream IdP")
	g, ok := f.grantOf(t, f.ProjectID)
	require.True(t, ok)
	rt, err := g.RefreshToken(f.sealer)
	require.NoError(t, err)
	assert.NotEqual(t, "refresh-1", rt, "the new grant replaced the old one")
}

func TestConnect_NoRefreshTokenRefused(t *testing.T) {
	t.Parallel()
	f := newConnectFixtureWith(t, func(c *oidcrp.Client) handlers.OpensheetAuth { return noRefreshAuth{c} })
	target, cookies := f.start(t, f.Principal, walletFixtureProjectSlug)
	rec := f.callback(t, f.Principal, f.idp.Authorize(target), cookies)
	require.Equal(t, http.StatusSeeOther, rec.Code, rec.Body.String())
	_, ok := f.grantOf(t, f.ProjectID)
	assert.False(t, ok, "a token response without a refresh token saves nothing")

	page := f.send(t, http.MethodGet, rec.Header().Get("Location"), f.Principal, nil, nil)
	require.Equal(t, http.StatusOK, page.Code)
	assert.Contains(t, page.Body.String(), f.en("opensheet.connect.no_refresh"))
	assert.Contains(t, page.Body.String(), "OSL018")
	assert.Contains(t, page.Body.String(), f.en("opensheet.connect.again"))
}

func TestConnect_StateBoundToProject(t *testing.T) {
	t.Parallel()
	f := newConnectFixture(t)
	p := f.member(t, "other@yasaku.test", org.RoleOwner)
	target, cookies := f.start(t, f.Principal, walletFixtureProjectSlug)
	rec := f.callback(t, p, f.idp.Authorize(target), cookies)
	assert.Equal(t, http.StatusBadRequest, rec.Code, "a state minted for another person is refused")
	_, ok := f.grantOf(t, f.ProjectID)
	assert.False(t, ok)
}

func TestConnect_StateReplayedForAnotherProjectRefused(t *testing.T) {
	t.Parallel()
	f := newConnectFixture(t)
	second, err := f.Projects.Create(f.tenantCtx(), f.OrgID, "second", "Second")
	require.NoError(t, err)
	targetA, _ := f.start(t, f.Principal, walletFixtureProjectSlug)
	_, cookiesB := f.start(t, f.Principal, "second")
	rec := f.callback(t, f.Principal, f.idp.Authorize(targetA), cookiesB)
	assert.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Contains(t, rec.Header().Get("Location"), "connect=failed")
	_, okA := f.grantOf(t, f.ProjectID)
	_, okB := f.grantOf(t, second.ID)
	assert.False(t, okA)
	assert.False(t, okB, "project A's authorization response never lands on project B")
}

func TestConnect_WrongIssuerRefused(t *testing.T) {
	t.Parallel()
	f := newConnectFixture(t)
	target, cookies := f.start(t, f.Principal, walletFixtureProjectSlug)
	q := f.idp.Authorize(target)
	q.Set("iss", "https://evil.example")
	rec := f.callback(t, f.Principal, q, cookies)
	assert.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Contains(t, rec.Header().Get("Location"), "connect=failed")
	_, ok := f.grantOf(t, f.ProjectID)
	assert.False(t, ok, "a mixed-up authorization response saves nothing")
}

func TestConnect_ConsentDeniedChangesNothing(t *testing.T) {
	t.Parallel()
	f := newConnectFixture(t)
	target, cookies := f.start(t, f.Principal, walletFixtureProjectSlug)
	rec := f.callback(t, f.Principal, f.idp.Deny(target), cookies)
	require.Equal(t, http.StatusSeeOther, rec.Code)
	page := f.send(t, http.MethodGet, rec.Header().Get("Location"), f.Principal, nil, nil)
	assert.Contains(t, page.Body.String(), f.en("opensheet.connect.cancelled"))
	_, ok := f.grantOf(t, f.ProjectID)
	assert.False(t, ok)
}

func TestConnect_Audited(t *testing.T) {
	t.Parallel()
	f := newConnectFixture(t)
	f.addOpensheetProject()
	f.connect(t)
	rec := f.send(t, http.MethodPost, f.path("/opensheet/provision"), f.Principal, nil, url.Values{"os_project_id": {"osp-1"}, "credential_id": {"cred-1"}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, []string{"opensheet.connected"}, f.actions(t))

	rec = f.send(t, http.MethodPost, f.path("/opensheet/delete"), f.Principal, nil, url.Values{})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, []string{"opensheet.disconnected", "opensheet.connected"}, f.actions(t))
}

func TestPicker_UnknownOpensheetPersonShowsSetup(t *testing.T) {
	t.Parallel()
	f := newConnectFixture(t)
	f.addOpensheetProject()
	f.sheets.RefusePerson()
	f.seedGrant(t, f.ProjectID, opensheetsync.GrantOK)
	page := f.send(t, http.MethodGet, f.path("/opensheet"), f.Principal, nil, nil)
	require.Equal(t, http.StatusOK, page.Code)
	body := page.Body.String()
	assert.Contains(t, body, f.en("opensheet.setup.title"))
	assert.Contains(t, body, f.en("opensheet.setup.body"))
	assert.Contains(t, body, `href="`+f.sheets.URL()+`" target="_blank" rel="noopener"`)
	assert.NotContains(t, body, f.en("opensheet.picker.title"))
}

func TestPicker_NoGoogleCredentialShowsSetup(t *testing.T) {
	t.Parallel()
	f := newConnectFixture(t)
	f.sheets.AddProject(fakes.OpensheetProject{ID: "osp-1", OrgSlug: "acme", Slug: "home", Name: "Home"})
	f.seedGrant(t, f.ProjectID, opensheetsync.GrantOK)
	page := f.send(t, http.MethodGet, f.path("/opensheet"), f.Principal, nil, nil)
	assert.Contains(t, page.Body.String(), f.en("opensheet.setup.title"))
}

func TestPicker_ForeignCredentialRefused(t *testing.T) {
	t.Parallel()
	f := newConnectFixture(t)
	f.addOpensheetProject()
	f.sheets.AddProject(fakes.OpensheetProject{ID: "osp-2", OrgSlug: "beta", Slug: "work", Name: "Work"},
		fakes.OpensheetCredential{ID: "cred-2", Name: "Bo", Kind: "google_oauth", Email: "bo@gmail.com"})
	f.seedGrant(t, f.ProjectID, opensheetsync.GrantOK)
	rec := f.send(t, http.MethodPost, f.path("/opensheet/provision"), f.Principal, nil, url.Values{"os_project_id": {"osp-1"}, "credential_id": {"cred-2"}})
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Empty(t, f.sheets.Provisions())
}

func TestProvisionSlugsArePerProject(t *testing.T) {
	t.Parallel()
	f := newConnectFixture(t)
	f.addOpensheetProject()
	ctx := t.Context()
	users := f.users()
	var scopes []context.Context
	for _, slug := range []string{"ana-org", "bo-org"} {
		u, err := users.Create(ctx, user.CreateRequest{Email: slug + "@yasaku.test", Name: slug, Source: user.SourceLocal})
		require.NoError(t, err)
		o, err := f.Orgs.Create(ctx, org.CreateRequest{Slug: slug, Name: slug, OwnerID: u.ID})
		require.NoError(t, err)
		p, err := f.Projects.Create(fakes.Owner(ctx, tenant.Context{OrgID: o.ID, UserID: u.ID}), o.ID, "personal", "Personal")
		require.NoError(t, err)
		sc := fakes.Owner(ctx, tenant.Context{OrgID: o.ID, ProjectID: p.ID, UserID: u.ID})
		_, err = f.svc.Connect(sc, oidcrp.TokenSet{AccessToken: "access-" + slug, RefreshToken: "refresh-" + slug, ExpiresAt: time.Now().Add(time.Hour), Scopes: opensheetsync.GrantScopes()})
		require.NoError(t, err)
		_, err = f.svc.Provision(sc, opensheetsync.Pick{OSProjectID: "osp-1", CredentialID: "cred-1", ProjectName: "Personal"})
		require.NoError(t, err)
		scopes = append(scopes, sc)
	}
	prov := f.sheets.Provisions()
	require.Len(t, prov, 2)
	seen := map[string]bool{}
	for i, p := range prov {
		tc, err := tenant.From(scopes[i])
		require.NoError(t, err)
		short := strings.ReplaceAll(tc.ProjectID.String(), "-", "")[20:]
		require.Len(t, p.Tabs, 3)
		for _, tab := range p.Tabs {
			assert.True(t, strings.HasPrefix(tab.Slug, "yasaku-"+short+"-"), tab.Slug)
			assert.False(t, seen[tab.Slug], "slug %s is used by two yasaku projects", tab.Slug)
			seen[tab.Slug] = true
		}
		assert.Equal(t, "yasaku:"+tc.ProjectID.String(), p.IdempotencyKey)
	}
}

func TestOpensheetPage_ReconnectNeededState(t *testing.T) {
	t.Parallel()
	f := newConnectFixture(t)
	f.seedGrantLink(t)
	f.seedGrant(t, f.ProjectID, opensheetsync.GrantReconnectNeeded)
	page := f.send(t, http.MethodGet, f.path("/opensheet"), f.Principal, nil, nil)
	require.Equal(t, http.StatusOK, page.Code)
	body := page.Body.String()
	assert.Contains(t, body, f.en("opensheet.reconnect.title"))
	assert.Contains(t, body, f.en("opensheet.reconnect.body"))
	assert.Contains(t, body, `action="`+f.path("/opensheet/connect")+`"`, "Reconnect starts the connect flow again")
	assert.NotContains(t, body, f.en("opensheet.reconnect.member_left"))
}

func TestOpensheetPage_MemberLeftState(t *testing.T) {
	t.Parallel()
	f := newConnectFixture(t)
	f.seedGrantLink(t)
	page := f.send(t, http.MethodGet, f.path("/opensheet"), f.Principal, nil, nil)
	require.Equal(t, http.StatusOK, page.Code)
	assert.Contains(t, page.Body.String(), f.en("opensheet.reconnect.member_left"))
}

func TestReconnect_ReplacesGrantAndKeepsLink(t *testing.T) {
	t.Parallel()
	f := newConnectFixture(t)
	f.seedGrantLink(t)
	f.seedGrant(t, f.ProjectID, opensheetsync.GrantReconnectNeeded)
	f.connect(t)
	g, ok := f.grantOf(t, f.ProjectID)
	require.True(t, ok)
	assert.Equal(t, opensheetsync.GrantOK, g.Status)
	page := f.send(t, http.MethodGet, f.path("/opensheet"), f.Principal, nil, nil)
	assert.NotContains(t, page.Body.String(), f.en("opensheet.reconnect.title"))
	assert.NotContains(t, page.Body.String(), f.en("opensheet.picker.title"), "a reconnect keeps the linked Sheet")
	assert.Equal(t, []string{"opensheet.connected"}, f.actions(t))
}

func TestDisconnect_RevokesAndKeepsSheet(t *testing.T) {
	t.Parallel()
	f := newConnectFixture(t)
	f.addOpensheetProject()
	f.connect(t)
	rec := f.send(t, http.MethodPost, f.path("/opensheet/provision"), f.Principal, nil, url.Values{"os_project_id": {"osp-1"}, "credential_id": {"cred-1"}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	before := len(f.sheets.Requests())

	rec = f.send(t, http.MethodPost, f.path("/opensheet/delete"), f.Principal, nil, url.Values{})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	_, ok := f.grantOf(t, f.ProjectID)
	assert.False(t, ok, "the grant is deleted")
	_, err := f.store.LinkByProject(f.tenantCtx(), f.OrgID, f.ProjectID)
	assert.True(t, opensheetsync.IsLinkNotFoundError(err))
	assert.Equal(t, 1, f.idp.Revocations(), "the refresh token is revoked at the upstream IdP")
	for _, r := range f.sheets.Requests()[before:] {
		assert.NotEqual(t, http.MethodDelete, r.Method, "the Sheet's rows stay")
	}
	assert.Equal(t, before, len(f.sheets.Requests()), "disconnect never calls OpenSheet")
}

func TestOpensheetPage_ConnectHiddenWithoutAudience(t *testing.T) {
	t.Parallel()
	f := newOpensheetFixture(t)
	page := f.do(t, f.mux, http.MethodGet, "/opensheet", nil)
	require.Equal(t, http.StatusOK, page.Code)
	assert.NotContains(t, page.Body.String(), "/opensheet/connect")
}

func TestOpensheetPage_NotConnectedLeadsWithConnect(t *testing.T) {
	t.Parallel()
	f := newConnectFixture(t)
	page := f.send(t, http.MethodGet, f.path("/opensheet"), f.Principal, nil, nil)
	require.Equal(t, http.StatusOK, page.Code)
	body := page.Body.String()
	assert.Contains(t, body, f.en("opensheet.connect.heading"))
	assert.Contains(t, body, `action="`+f.path("/opensheet/connect")+`" hx-post="`+f.path("/opensheet/connect")+`"`, "Connect is a POST form")
	assert.Contains(t, body, f.en("opensheet.connect.api_key_instead"))
}

// NOTE: countingAuth is the real client that counts code redemptions, so a test can prove a refused callback never redeems.
type countingAuth struct {
	*oidcrp.Client
	exchanges *int
}

func (a countingAuth) ExchangeTokens(ctx context.Context, cb oidcrp.Callback, req oidcrp.AuthRequest) (*oidcrp.TokenSet, error) {
	*a.exchanges++
	return a.Client.ExchangeTokens(ctx, cb, req)
}

// NOTE: narrowAuth is the real client reporting a granted scope without sheets:admin, as an issuer that grants less than asked.
type narrowAuth struct{ *oidcrp.Client }

func (a narrowAuth) ExchangeTokens(ctx context.Context, cb oidcrp.Callback, req oidcrp.AuthRequest) (*oidcrp.TokenSet, error) {
	set, err := a.Client.ExchangeTokens(ctx, cb, req)
	if set != nil {
		set.Scopes = slices.DeleteFunc(slices.Clone(set.Scopes), func(sc string) bool { return sc == "sheets:admin" })
	}
	return set, err
}

func TestProvisionSlugs_SameMillisecondProjectsDiffer(t *testing.T) {
	t.Parallel()
	a := uuid.MustParse("019a1b2c-3d4e-7000-8000-000000000001")
	b := uuid.MustParse("019a1b2c-3d4e-7000-8000-000000000002")
	assert.NotEqual(t, opensheetsync.ProvisionSlugs(a), opensheetsync.ProvisionSlugs(b), "UUIDv7s minted in one millisecond share their first 12 hex")
	assert.Equal(t, "yasaku-000000000001-wallets", opensheetsync.ProvisionSlugs(a).Wallets)
}

func TestCallback_NonManagerNeverRedeems(t *testing.T) {
	t.Parallel()
	n := 0
	f := newConnectFixtureWith(t, func(c *oidcrp.Client) handlers.OpensheetAuth { return countingAuth{Client: c, exchanges: &n} })
	p := f.member(t, "second@yasaku.test", org.RoleOwner)
	target, cookies := f.start(t, p, walletFixtureProjectSlug)
	require.NoError(t, f.Orgs.SetRole(f.tenantCtx(), f.OrgID, p.UserID, string(org.RoleMember), uuid.Nil))
	rec := f.callback(t, p, f.idp.Authorize(target), cookies)
	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	assert.Zero(t, n, "a person who lost ledger:settings never redeems the code")
	_, ok := f.grantOf(t, f.ProjectID)
	assert.False(t, ok)
}

func TestCallback_MissingScopeRefusedAndRevoked(t *testing.T) {
	t.Parallel()
	f := newConnectFixtureWith(t, func(c *oidcrp.Client) handlers.OpensheetAuth { return narrowAuth{c} })
	target, cookies := f.start(t, f.Principal, walletFixtureProjectSlug)
	rec := f.callback(t, f.Principal, f.idp.Authorize(target), cookies)
	require.Equal(t, http.StatusSeeOther, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Header().Get("Location"), "connect=no_refresh")
	_, ok := f.grantOf(t, f.ProjectID)
	assert.False(t, ok, "a grant missing a required scope is not saved")
	assert.Equal(t, 1, f.idp.Revocations(), "the just-issued refresh token is revoked")
}

func TestConnect_StateBoundToProjectID(t *testing.T) {
	t.Parallel()
	f := newConnectFixture(t)
	second, err := f.Projects.Create(f.tenantCtx(), f.OrgID, "second", "Second")
	require.NoError(t, err)
	target, cookies := f.start(t, f.Principal, walletFixtureProjectSlug)
	var forged []*http.Cookie
	for _, c := range cookies {
		if c.Name != "yasaku_opensheet_state" {
			forged = append(forged, c)
			continue
		}
		key := f.Deps.Key(web.PurposeOpensheetState)
		value, err := web.VerifyCookie(key, c.Value)
		require.NoError(t, err)
		raw, err := base64.RawURLEncoding.DecodeString(value)
		require.NoError(t, err)
		var st map[string]any
		require.NoError(t, json.Unmarshal(raw, &st))
		st["ps"] = "second"
		raw, err = json.Marshal(st)
		require.NoError(t, err)
		forged = append(forged, &http.Cookie{Name: c.Name, Value: web.SignCookie(key, base64.RawURLEncoding.EncodeToString(raw))})
	}
	rec := f.callback(t, f.Principal, f.idp.Authorize(target), forged)
	assert.Equal(t, http.StatusBadRequest, rec.Code, "a state minted for project A never lands on project B, even under B's slug")
	_, okA := f.grantOf(t, f.ProjectID)
	_, okB := f.grantOf(t, second.ID)
	assert.False(t, okA)
	assert.False(t, okB)
}

func TestConnect_KeyLinkRefused(t *testing.T) {
	t.Parallel()
	f := newConnectFixture(t)
	f.seedLink(t, []byte("sealed"))
	rec := f.send(t, http.MethodPost, f.path("/opensheet/connect"), f.Principal, nil, url.Values{})
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	page := f.send(t, http.MethodGet, f.path("/opensheet")+"?connect=failed", f.Principal, nil, nil)
	assert.NotContains(t, page.Body.String(), f.path("/opensheet/connect"), "a key link offers no Connect")
}

func TestConnect_OutcomeBannerAddsNoSecondButton(t *testing.T) {
	t.Parallel()
	f := newConnectFixture(t)
	page := f.send(t, http.MethodGet, f.path("/opensheet")+"?connect=failed", f.Principal, nil, nil)
	body := page.Body.String()
	assert.Equal(t, 1, strings.Count(body, `action="`+f.path("/opensheet/connect")+`"`), "one Connect button")
	assert.Contains(t, body, f.en("opensheet.connect.again"), "the card's button reads Connect again after an outcome")
}

func TestConnect_FailedOutcomeSitsInTheCardWithAFix(t *testing.T) {
	t.Parallel()
	f := newConnectFixture(t)
	page := f.send(t, http.MethodGet, f.path("/opensheet")+"?connect=failed", f.Principal, nil, nil)
	body := page.Body.String()
	card := strings.Index(body, `data-opensheet-connect="true"`)
	outcome := strings.Index(body, `data-opensheet-connect-outcome="opensheet.connect.failed"`)
	require.Positive(t, card)
	assert.Greater(t, outcome, card, "the outcome renders inside the Connect card, not above the page heading")
	assert.Contains(t, body, f.en("opensheet.connect.failed"))
	assert.Contains(t, f.en("opensheet.connect.failed"), "Try again in a minute")
	assert.Contains(t, body, `hx-status:5xx="target:#opensheet-connect-error swap:innerHTML"`, "a failed start lands in the card too")
}

func TestConnect_CardHeadingDiffersFromPageHeading(t *testing.T) {
	t.Parallel()
	f := newConnectFixture(t)
	body := f.send(t, http.MethodGet, f.path("/opensheet"), f.Principal, nil, nil).Body.String()
	assert.Contains(t, body, `<h2 id="gs-connect-h" class="sr-only">`+f.en("opensheet.connect.card_heading")+`</h2>`)
	assert.NotEqual(t, f.en("opensheet.connect.heading"), f.en("opensheet.connect.card_heading"))
}

func TestConnect_UnreachableIdPAnswersTheCardBanner(t *testing.T) {
	t.Parallel()
	f := newConnectFixture(t)
	f.idp.Close()
	sid, err := web.NewSID()
	require.NoError(t, err)
	require.NoError(t, f.Sessions.Save(t.Context(), sid, f.Principal, time.Now().Add(web.SessionTTL)))
	r := httptest.NewRequest(http.MethodPost, f.path("/opensheet/connect"), strings.NewReader(""))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("HX-Request", "true")
	r.AddCookie(&http.Cookie{Name: web.SessionCookieName, Value: web.SignCookie(f.Deps.Key(web.PurposeSession), sid)})
	r = r.WithContext(f.withLocale(session.PrincipalInto(r.Context(), f.Principal)))
	rec := httptest.NewRecorder()
	f.mux.ServeHTTP(rec, r)
	assert.Equal(t, http.StatusBadGateway, rec.Code)
	assert.Contains(t, rec.Body.String(), f.en("opensheet.connect.failed"))
	assert.NotContains(t, rec.Body.String(), f.en("error_title.connect_failed"), "htmx gets the card's banner, not the error page fragment")
}

func TestPicker_SetupNeededCanCancel(t *testing.T) {
	t.Parallel()
	f := newConnectFixture(t)
	f.sheets.RefusePerson()
	f.seedGrant(t, f.ProjectID, opensheetsync.GrantOK)
	body := f.send(t, http.MethodGet, f.path("/opensheet"), f.Principal, nil, nil).Body.String()
	setup := strings.Index(body, `data-opensheet-setup-needed="true"`)
	cancel := strings.Index(body, `data-opensheet-cancel-connect="true"`)
	require.Positive(t, setup)
	assert.Greater(t, cancel, setup, "the setup-needed card offers Cancel")
	rec := f.send(t, http.MethodPost, f.path("/opensheet/delete"), f.Principal, nil, url.Values{})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	_, ok := f.grantOf(t, f.ProjectID)
	assert.False(t, ok, "Cancel drops the grant")
	assert.Equal(t, 1, f.idp.Revocations())
	assert.Contains(t, rec.Body.String(), `data-opensheet-connect="true"`, "back to not connected")
}
