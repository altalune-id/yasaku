package boot_test

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/boot"
	"altalune.id/yasaku/internal/onboard"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform/authn"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/user"
	"altalune.id/yasaku/internal/web"
)

type probeRoute struct {
	method string
	path   string
	form   url.Values
}

// NOTE: asserted complete against the registered mux in TestRoutes_ListMatchesTheMux.
func probeRoutes() []probeRoute {
	const (
		org   = "probe-org"
		proj  = "probe-project"
		base  = "/orgs/" + org
		pbase = base + "/projects/" + proj
	)
	id := uuid.NewString()
	return []probeRoute{
		{http.MethodGet, "/", nil},
		{http.MethodGet, "/login", nil},
		{http.MethodGet, "/admin-login", nil},
		{http.MethodGet, "/login/oidc", nil},
		{http.MethodGet, "/oauth/callback", nil},
		{http.MethodGet, "/onboarding", nil},
		{http.MethodGet, "/welcome", nil},
		{http.MethodGet, "/terms", nil},
		{http.MethodGet, "/privacy", nil},
		{http.MethodGet, "/orgs", nil},
		{http.MethodGet, "/orgs/new", nil},
		{http.MethodGet, base, nil},
		{http.MethodGet, base + "/members", nil},
		{http.MethodGet, base + "/invites", nil},
		{http.MethodGet, base + "/apikeys", nil},
		{http.MethodGet, base + "/projects", nil},
		{http.MethodGet, base + "/projects/new", nil},
		{http.MethodGet, pbase + "/overview", nil},
		{http.MethodGet, pbase + "/categories", nil},
		{http.MethodGet, pbase + "/apikeys", nil},
		{http.MethodGet, pbase + "/wallets", nil},
		{http.MethodGet, pbase + "/wallets/new", nil},
		{http.MethodGet, pbase + "/wallets/" + id, nil},
		{http.MethodGet, pbase + "/wallets/" + id + "/edit", nil},
		{http.MethodGet, pbase + "/wallets/" + id + "/adjust", nil},
		{http.MethodGet, pbase + "/transactions", nil},
		{http.MethodGet, pbase + "/transactions/new", nil},
		{http.MethodGet, pbase + "/transactions/" + id + "/edit", nil},
		{http.MethodGet, pbase + "/periods", nil},
		{http.MethodGet, pbase + "/periods/" + id + "/close", nil},
		{http.MethodGet, pbase + "/reports", nil},
		{http.MethodGet, pbase + "/settings", nil},
		{http.MethodGet, "/signup/complete", nil},
		{http.MethodGet, "/onboard", nil},
		{http.MethodGet, "/onboard/oidc", nil},
		{http.MethodGet, "/onboard/complete", nil},
		{http.MethodGet, "/invites/accept", nil},
		{http.MethodGet, "/settings/tokens", nil},
		{http.MethodGet, "/settings/tokens/projects", nil},

		{http.MethodPost, "/orgs", url.Values{"slug": {"probe-new-org"}, "name": {"Probe New Org"}}},
		{http.MethodPost, base + "/rename", url.Values{"name": {"Renamed"}}},
		{http.MethodPost, base + "/invites", url.Values{"email": {"probe@example.com"}, "role": {"member"}}},
		{http.MethodPost, base + "/invites/" + id + "/revoke", url.Values{}},
		{http.MethodPost, base + "/apikeys", url.Values{"name": {"Probe Key"}, "grant": {"all"}}},
		{http.MethodPost, base + "/apikeys/" + id + "/projects", url.Values{}},
		{http.MethodPost, base + "/apikeys/" + id + "/all-projects", url.Values{}},
		{http.MethodPost, base + "/apikeys/" + id + "/revoke", url.Values{}},
		{http.MethodPost, "/settings/tokens", url.Values{"org": {org}, "name": {"Probe Token"}, "grant": {"all"}, "expires_in": {"7"}}},
		{http.MethodPost, "/settings/tokens/" + org + "/" + id + "/projects", url.Values{}},
		{http.MethodPost, "/settings/tokens/" + org + "/" + id + "/all-projects", url.Values{}},
		{http.MethodPost, "/settings/tokens/" + org + "/" + id + "/revoke", url.Values{}},
		{http.MethodPost, base + "/members/" + id + "/remove", url.Values{}},
		{http.MethodPost, base + "/projects", url.Values{"slug": {"probe-new-project"}, "name": {"Probe New Project"}}},
		{http.MethodPost, pbase + "/rename", url.Values{"name": {"Renamed"}}},
		{http.MethodPost, pbase + "/categories", url.Values{"name": {"Probe Category"}, "kind": {"expense"}}},
		{http.MethodPost, pbase + "/categories/seed", url.Values{}},
		{http.MethodPost, pbase + "/categories/" + id + "/rename", url.Values{"name": {"Renamed"}}},
		{http.MethodPost, pbase + "/categories/" + id + "/archive", url.Values{}},
		{http.MethodPost, pbase + "/categories/" + id + "/unarchive", url.Values{}},
		{http.MethodPost, pbase + "/categories/" + id + "/delete", url.Values{}},
		{http.MethodPost, pbase + "/wallets", url.Values{
			"kind": {"cash"}, "name": {"Probe Wallet"}, "opening_balance": {"100000"},
		}},
		{http.MethodPost, pbase + "/wallets/" + id, url.Values{"kind": {"cash"}, "name": {"Renamed Wallet"}}},
		{http.MethodPost, pbase + "/wallets/" + id + "/archive", url.Values{}},
		{http.MethodPost, pbase + "/wallets/" + id + "/unarchive", url.Values{}},
		{http.MethodPost, pbase + "/wallets/" + id + "/delete", url.Values{}},
		{http.MethodPost, pbase + "/wallets/" + id + "/adjust", url.Values{"target": {"150000"}}},
		{http.MethodPost, pbase + "/transactions", url.Values{
			"kind": {"expense"}, "amount": {"25000"}, "wallet_id": {id}, "note": {"probe"},
		}},
		{http.MethodPost, pbase + "/transactions/suggest", url.Values{"kind": {"expense"}, "note": {"probe"}}},
		{http.MethodPost, pbase + "/transactions/" + id, url.Values{
			"kind": {"expense"}, "amount": {"25000"}, "wallet_id": {id}, "note": {"probe"},
		}},
		{http.MethodPost, pbase + "/transactions/" + id + "/delete", url.Values{}},
		{http.MethodPost, pbase + "/periods/" + id + "/close", url.Values{"end_date": {"2026-09-30"}}},
		{http.MethodPost, pbase + "/periods/" + id + "/reopen", url.Values{}},
		{http.MethodPost, pbase + "/periods/" + id + "/rename", url.Values{"name": {"Renamed Period"}}},
		{http.MethodPost, pbase + "/settings", url.Values{
			"timezone": {"Asia/Jakarta"}, "currency": {"IDR"}, "period_start_day": {"25"},
		}},
		{http.MethodGet, pbase + "/opensheet", nil},
		{http.MethodPost, pbase + "/opensheet", opensheetProbeForm()},
		{http.MethodPost, pbase + "/opensheet/test", opensheetProbeForm()},
		{http.MethodPost, pbase + "/opensheet/enabled", url.Values{"enabled": {"true"}}},
		{http.MethodPost, pbase + "/opensheet/sync", url.Values{}},
		{http.MethodPost, pbase + "/opensheet/delete", url.Values{}},
		{http.MethodPost, pbase + "/apikeys", url.Values{"name": {"Probe Key"}, "scopes": {authn.ScopeYasakuRead}}},
		{http.MethodPost, pbase + "/apikeys/" + id + "/revoke", url.Values{}},
		{http.MethodPost, "/onboarding", url.Values{"name": {"Probe"}}},
		{http.MethodPost, "/welcome", url.Values{"name": {"Probe"}}},
		{http.MethodPost, "/signup/complete", url.Values{
			"org_slug": {"probe-signup-org"}, "org_name": {"Probe Signup Org"},
			"project_slug": {"default"}, "project_name": {"Default"},
			"name": {"Probe"}, "accept_terms": {"1"},
		}},
		{http.MethodPost, "/login", url.Values{"email": {"probe@example.com"}, "password": {"probe-password"}}},
		{http.MethodPost, "/onboard/local", url.Values{
			"email": {"probe-onboard@example.com"}, "name": {"Probe"}, "password": {"probe-password"},
			"org_slug": {"probe-onboard-org"}, "org_name": {"Probe Onboard Org"},
			"project_slug": {"default"}, "project_name": {"Default"},
		}},
		{http.MethodPost, "/onboard/complete", url.Values{}},
		{http.MethodPost, "/logout", url.Values{}},
		{http.MethodPost, "/locale", url.Values{"locale": {"en-US"}, "redirect": {"/"}}},
	}
}

func opensheetProbeForm() url.Values {
	return url.Values{
		"os_org": {"acme"}, "os_project": {"home"}, "api_key": {"osk_probe_0123456789abcd"},
		"transactions_sheet": {"yasaku-transactions"}, "wallets_sheet": {"yasaku-wallets"}, "categories_sheet": {"yasaku-categories"},
	}
}

func newScopeProbeServer(t *testing.T, mode config.Mode) (*boot.Server, *bytes.Buffer) {
	t.Helper()
	cfg := newSmokeCfg(t)
	cfg.Mode = mode
	// NOTE: a refused local port answers the Opensheet Test at once; the module must be mounted for its routes to be probed.
	cfg.Opensheet = config.OpensheetConfig{BaseURL: "http://127.0.0.1:9", AllowPrivateHosts: true}
	cfg.Security.EncryptionKey = strings.Repeat("ab", 32)
	if mode == config.ModeCloud {
		// NOTE: org creation and the signup flow are cloud-only capabilities.
		cfg.OIDC = config.OIDCConfig{
			Issuer:       stubIssuer(t),
			ClientID:     "probe-client",
			ClientSecret: "probe-secret",
		}
	}
	var logBuf bytes.Buffer
	log := boot.WithLogger(slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug})))

	// NOTE: OnboardingGate 303s every route to /onboard until the deployment is onboarded, so mark it onboarded first.
	seed, err := boot.BootServer(context.Background(), cfg, log)
	require.NoError(t, err)
	seedUser, err := seed.Users.Create(context.Background(), user.CreateRequest{
		Email: "probe-seed@example.com", Name: "Probe Seed", Source: user.SourceOIDC,
	})
	require.NoError(t, err)
	_, err = seed.Onboards.Complete(context.Background(), seedUser.ID, onboard.MethodCLIInit)
	require.NoError(t, err)
	require.NoError(t, seed.Close())

	srv, err := boot.BootServer(context.Background(), cfg, log)
	require.NoError(t, err)
	require.True(t, srv.Onboarded, "the probe server must be past the onboarding gate")
	t.Cleanup(func() { _ = srv.Close() })
	logBuf.Reset()
	return srv, &logBuf
}

func probeCookie(t *testing.T, srv *boot.Server, p session.Principal) *http.Cookie {
	t.Helper()
	sid, err := web.NewSID()
	require.NoError(t, err)
	require.NoError(t, srv.Platform.Sessions.Save(context.Background(), sid, p, time.Now().Add(time.Hour)))
	return &http.Cookie{
		Name:  web.SessionCookieName,
		Value: web.SignCookie([]byte(srv.Cfg.HTTP.StateSecret), sid),
	}
}

func walkRoutes(t *testing.T, srv *boot.Server, logBuf *bytes.Buffer, p session.Principal, label string) {
	t.Helper()
	cookie := probeCookie(t, srv, p)
	for _, rt := range probeRoutes() {
		t.Run(label+" "+rt.method+" "+rt.path, func(t *testing.T) {
			logBuf.Reset()
			var req *http.Request
			if rt.form == nil {
				req = httptest.NewRequest(rt.method, rt.path, nil)
			} else {
				req = httptest.NewRequest(rt.method, rt.path, strings.NewReader(rt.form.Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			}
			req.AddCookie(cookie)
			rec := httptest.NewRecorder()
			srv.Web.ServeHTTP(rec, req)

			// SECURITY: a tenant-scope slip is a 500 with an opaque body, so the log is the only place it names itself.
			logged := logBuf.String()
			require.NotContains(t, logged, "tenant: missing context",
				"%s %s reached a tenant-scoped store without a scope", rt.method, rt.path)
			require.NotContains(t, logged, "tenant: context names no org",
				"%s %s reached a tenant-scoped store with a scope naming no org", rt.method, rt.path)
			require.Less(t, rec.Code, 500,
				"%s %s returned %d\nbody=%s\nlog=%s", rt.method, rt.path, rec.Code, rec.Body.String(), logged)
		})
	}
}

func TestRoutes_FreshUserWithoutAnOrgNeverHitsATenantScopeError(t *testing.T) {
	for _, mode := range []config.Mode{config.ModeSelfhosted, config.ModeCloud} {
		srv, logBuf := newScopeProbeServer(t, mode)
		// NOTE: a real row, not a bare uuid — sessions.user_id has a foreign key to users.
		fresh, err := srv.Users.Create(context.Background(), user.CreateRequest{
			Email: "probe-fresh@example.com", Name: "Probe Fresh", Source: user.SourceOIDC,
		})
		require.NoError(t, err)
		walkRoutes(t, srv, logBuf, session.Principal{UserID: fresh.ID}, string(mode)+" no-org")
	}
}

func TestRoutes_UserWithAnActiveOrgNeverHitsATenantScopeError(t *testing.T) {
	srv, logBuf := newScopeProbeServer(t, config.ModeCloud)
	owner, err := srv.Users.Create(context.Background(), user.CreateRequest{
		Email: "probe-owner@example.com", Name: "Probe Owner", Source: user.SourceOIDC,
	})
	require.NoError(t, err)
	o, err := srv.Orgs.Create(context.Background(), org.CreateRequest{
		Slug: "probe-org", Name: "Probe Org", OwnerID: owner.ID,
	})
	require.NoError(t, err, "org.Create must work with no ambient tenant scope — the signup case")

	// NOTE: without the project, every project-scoped probe short-circuits on the handler's own 404 and reaches no handler code.
	orgCtx := tenant.Into(context.Background(), tenant.Context{OrgID: o.ID, UserID: owner.ID})
	_, err = srv.Projects.Create(orgCtx, o.ID, "probe-project", "Probe Project")
	require.NoError(t, err)

	walkRoutes(t, srv, logBuf, session.Principal{UserID: owner.ID, ActiveOrgID: o.ID}, "cloud with-org")
}

// TestRoutes_ListMatchesTheMux asserts the probe table and the registered mux name the same routes, in both directions.
func TestRoutes_ListMatchesTheMux(t *testing.T) {
	srv, _ := newScopeProbeServer(t, config.ModeCloud)

	walked := map[string]bool{}
	for _, rt := range probeRoutes() {
		walked[rt.method+" "+templatize(rt.path)] = true
	}
	registered := map[string]bool{}
	for _, pat := range srv.Routes {
		registered[pat] = true
	}
	require.NotEmpty(t, registered, "the server registered no app routes")

	for pat := range registered {
		require.True(t, walked[pat],
			"%q is registered on the mux but no probe walks it — add it to probeRoutes in route_scope_test.go", pat)
	}
	for pat := range walked {
		require.True(t, registered[pat],
			"%q is probed but the mux does not register it — the handler was dropped from AppHandlers, or the probe is stale", pat)
	}
}

var reUUID = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

func templatize(path string) string {
	path = reUUID.ReplaceAllString(path, "{id}")
	path = strings.Replace(path, "/orgs/probe-org", "/orgs/{org}", 1)
	path = strings.Replace(path, "/settings/tokens/probe-org/", "/settings/tokens/{org}/", 1)
	path = strings.Replace(path, "/projects/probe-project", "/projects/{project}", 1)
	if strings.HasPrefix(path, "/orgs/{org}/members/{id}/") {
		path = strings.Replace(path, "/members/{id}/", "/members/{user}/", 1)
	}
	path = strings.Replace(path, "/deliveries/{id}", "/deliveries/{did}", 1)
	if path == "/" {
		return "/{$}"
	}
	return path
}

func stubIssuer(t *testing.T) string {
	t.Helper()
	mux := http.NewServeMux()
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"issuer": "` + ts.URL + `",
			"authorization_endpoint": "` + ts.URL + `/authorize",
			"token_endpoint": "` + ts.URL + `/token",
			"userinfo_endpoint": "` + ts.URL + `/userinfo",
			"jwks_uri": "` + ts.URL + `/jwks",
			"response_types_supported": ["code"],
			"subject_types_supported": ["public"],
			"id_token_signing_alg_values_supported": ["RS256"]
		}`))
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"keys":[]}`))
	})
	return ts.URL
}

// TestRoutes_NonMemberCannotReachAnotherOrg locks the membership gate: the slug is attacker-supplied and RLS cannot gate it.
func TestRoutes_NonMemberCannotReachAnotherOrg(t *testing.T) {
	srv, _ := newScopeProbeServer(t, config.ModeCloud)

	owner, err := srv.Users.Create(context.Background(), user.CreateRequest{
		Email: "probe-owner@example.com", Name: "Probe Owner", Source: user.SourceOIDC,
	})
	require.NoError(t, err)
	o, err := srv.Orgs.Create(context.Background(), org.CreateRequest{
		Slug: "private-org", Name: "Private Org", OwnerID: owner.ID,
	})
	require.NoError(t, err)

	outsider, err := srv.Users.Create(context.Background(), user.CreateRequest{
		Email: "probe-outsider@example.com", Name: "Probe Outsider", Source: user.SourceOIDC,
	})
	require.NoError(t, err)
	ownOrg, err := srv.Orgs.Create(context.Background(), org.CreateRequest{
		Slug: "outsider-org", Name: "Outsider Org", OwnerID: outsider.ID,
	})
	require.NoError(t, err)

	cookie := probeCookie(t, srv, session.Principal{UserID: outsider.ID, ActiveOrgID: ownOrg.ID})
	for _, path := range []string{"/orgs/" + o.Slug + "/members", "/orgs/" + o.Slug + "/invites"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		srv.Web.ServeHTTP(rec, req)
		require.Equal(t, http.StatusNotFound, rec.Code,
			"a non-member must not read %s; body=%s", path, rec.Body.String())
		require.NotContains(t, rec.Body.String(), "probe-owner@example.com",
			"%s leaked a member of an org the caller does not belong to", path)
	}
}

// TestRoutes_InlineFormErrorCarriesTheRequestID keeps a failed form submit traceable to its log line.
func TestRoutes_InlineFormErrorCarriesTheRequestID(t *testing.T) {
	srv, _ := newScopeProbeServer(t, config.ModeCloud)

	owner, err := srv.Users.Create(context.Background(), user.CreateRequest{
		Email: "probe-dup@example.com", Name: "Probe Dup", Source: user.SourceOIDC,
	})
	require.NoError(t, err)
	taken, err := srv.Orgs.Create(context.Background(), org.CreateRequest{
		Slug: "taken-slug", Name: "Taken", OwnerID: owner.ID,
	})
	require.NoError(t, err)

	form := url.Values{"slug": {taken.Slug}, "name": {"Duplicate"}}
	req := httptest.NewRequest(http.MethodPost, "/orgs", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(probeCookie(t, srv, session.Principal{UserID: owner.ID, ActiveOrgID: taken.ID}))
	rec := httptest.NewRecorder()
	srv.Web.ServeHTTP(rec, req)

	body := rec.Body.String()
	require.Contains(t, body, "Reference:", "an inline form error must name the request id")
	rid := rec.Header().Get("X-Request-Id")
	require.NotEmpty(t, rid, "the request id header must be set")
	require.Contains(t, body, rid, "the id in the page must be the one in the header and the log")
}

// TestOrgSwitcher_PinnedOrgIsNotAlsoOfferedToSwitchTo covers the pages that pin the switcher to the org in the URL.
func TestOrgSwitcher_PinnedOrgIsNotAlsoOfferedToSwitchTo(t *testing.T) {
	srv, _ := newScopeProbeServer(t, config.ModeCloud)

	owner, err := srv.Users.Create(context.Background(), user.CreateRequest{
		Email: "probe-two-orgs@example.com", Name: "Probe", Source: user.SourceOIDC,
	})
	require.NoError(t, err)
	active, err := srv.Orgs.Create(context.Background(), org.CreateRequest{
		Slug: "org-active", Name: "Org Active", OwnerID: owner.ID,
	})
	require.NoError(t, err)
	visited, err := srv.Orgs.Create(context.Background(), org.CreateRequest{
		Slug: "org-visited", Name: "Org Visited", OwnerID: owner.ID,
	})
	require.NoError(t, err)

	cookie := probeCookie(t, srv, session.Principal{UserID: owner.ID, ActiveOrgID: active.ID})
	for _, path := range []string{"/orgs/" + visited.Slug + "/members", "/orgs/" + visited.Slug + "/invites"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		srv.Web.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, path)

		panel := orgSwitcherPanel(t, rec.Body.String())
		require.Equal(t, 1, strings.Count(panel, visited.Name),
			"%s: the pinned org must appear once, as Current — not also under Switch\n%s", path, panel)
		require.Contains(t, panel, active.Name,
			"%s: the org the session is active in must stay reachable under Switch", path)
	}
}

func orgSwitcherPanel(t *testing.T, body string) string {
	t.Helper()
	i := strings.Index(body, "data-switcher")
	require.GreaterOrEqual(t, i, 0, "no org switcher rendered")
	rest := body[i:]
	open := strings.Index(rest, "</summary>")
	require.GreaterOrEqual(t, open, 0, "org switcher has no summary")
	rest = rest[open+len("</summary>"):]
	end := strings.Index(rest, "</details>")
	require.GreaterOrEqual(t, end, 0, "org switcher never closed")
	return rest[:end]
}

// TestMembersPage_RemoveButtonMatchesTheServiceGate keeps the rendered button in step with org.RemovalRefusal.
func TestMembersPage_RemoveButtonMatchesTheServiceGate(t *testing.T) {
	srv, _ := newScopeProbeServer(t, config.ModeCloud)
	ctx := context.Background()

	viewer, err := srv.Users.Create(ctx, user.CreateRequest{
		Email: "gate-viewer@example.com", Name: "Gate Viewer", Source: user.SourceOIDC,
	})
	require.NoError(t, err)
	o, err := srv.Orgs.Create(ctx, org.CreateRequest{Slug: "gate-org", Name: "Gate Org", OwnerID: viewer.ID})
	require.NoError(t, err)

	scoped := tenant.WithOrg(ctx, o.ID)
	coOwner, err := srv.Users.Create(ctx, user.CreateRequest{
		Email: "gate-coowner@example.com", Name: "Gate CoOwner", Source: user.SourceOIDC,
	})
	require.NoError(t, err)
	_, err = srv.Orgs.AddMember(scoped, o.ID, coOwner.ID, org.RoleOwner)
	require.NoError(t, err)

	plain, err := srv.Users.Create(ctx, user.CreateRequest{
		Email: "gate-plain@example.com", Name: "Gate Plain", Source: user.SourceOIDC,
	})
	require.NoError(t, err)
	_, err = srv.Orgs.AddMember(scoped, o.ID, plain.ID, org.RoleMember)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/orgs/"+o.Slug+"/members", nil)
	req.AddCookie(probeCookie(t, srv, session.Principal{UserID: viewer.ID, ActiveOrgID: o.ID}))
	rec := httptest.NewRecorder()
	srv.Web.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body := rec.Body.String()

	removeForm := func(u uuid.UUID) string {
		return "/orgs/" + o.Slug + "/members/" + u.String() + "/remove"
	}
	require.Contains(t, body, removeForm(plain.ID), "a plain member must be removable")
	require.NotContains(t, body, removeForm(viewer.ID), "the signed-in member must not offer to remove themselves")
	require.Contains(t, body, removeForm(coOwner.ID), "an owner viewing the page may remove a co-owner")

	for _, u := range []uuid.UUID{viewer.ID, coOwner.ID, plain.ID} {
		m, mErr := srv.Orgs.MembershipOf(scoped, o.ID, u)
		require.NoError(t, mErr)
		viewerM, vErr := srv.Orgs.MembershipOf(scoped, o.ID, viewer.ID)
		require.NoError(t, vErr)
		allowed := org.RemovalRefusal(o.ID, viewer.ID, u, viewerM.Role, m.Role, m.System) == nil
		require.Equal(t, allowed, strings.Contains(body, removeForm(u)),
			"button visibility for %s disagrees with org.RemovalRefusal", u)
	}
}

// TestRoutes_InlineFormErrorCarriesTheErrorCode pairs the quotable code with the request id on a failed submit.
func TestRoutes_InlineFormErrorCarriesTheErrorCode(t *testing.T) {
	srv, _ := newScopeProbeServer(t, config.ModeCloud)

	owner, err := srv.Users.Create(context.Background(), user.CreateRequest{
		Email: "probe-code@example.com", Name: "Probe Code", Source: user.SourceOIDC,
	})
	require.NoError(t, err)
	taken, err := srv.Orgs.Create(context.Background(), org.CreateRequest{
		Slug: "code-taken", Name: "Taken", OwnerID: owner.ID,
	})
	require.NoError(t, err)

	form := url.Values{"slug": {taken.Slug}, "name": {"Duplicate"}}
	req := httptest.NewRequest(http.MethodPost, "/orgs", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(probeCookie(t, srv, session.Principal{UserID: owner.ID, ActiveOrgID: taken.ID}))
	rec := httptest.NewRecorder()
	srv.Web.ServeHTTP(rec, req)

	body := rec.Body.String()
	require.Contains(t, body, apperror.CodeOrgAlreadyExists,
		"a duplicate slug must name its code (%s) so the user can quote it", apperror.CodeOrgAlreadyExists)
	require.Contains(t, body, "Reference:", "the request id must stay alongside the code")
}
