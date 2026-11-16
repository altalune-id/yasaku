package handlers_test

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/user"
	"altalune.id/yasaku/internal/web"
	"altalune.id/yasaku/internal/web/handlers"
)

func postTheme(t *testing.T, f *handlerFixture, r *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	handlers.NewThemeHandler(f.Deps, f.Users).Register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, r)
	return rec
}

func themeForm(body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/theme", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return r
}

func themeCookie(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == web.ThemeCookieName {
			return c.Value
		}
	}
	t.Fatalf("no %s cookie set", web.ThemeCookieName)
	return ""
}

func seedUser(t *testing.T, f *handlerFixture, email string) *user.User {
	t.Helper()
	u, err := user.New(email, "N", user.SourceLocal)
	require.NoError(t, err)
	require.NoError(t, f.UserStore.Save(context.Background(), u))
	return u
}

func TestTheme_SignedOutSetsCookieAndRedirects(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	fam := web.DefaultFamily()
	rec := postTheme(t, f, themeForm("family="+fam+"&mode=dark&redirect=/orgs?x=1"))
	assert.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, "/orgs?x=1", rec.Header().Get("Location"))
	assert.Equal(t, fam+"-dark", themeCookie(t, rec))
}

func TestTheme_JunkBecomesDefault(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	rec := postTheme(t, f, themeForm(`family=midnight&mode=auto&redirect=/`))
	assert.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, web.DefaultFamily()+"-system", themeCookie(t, rec))
}

func TestTheme_RejectsOffsiteRedirect(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	for _, target := range []string{"//evil.example", "https://evil.example/", "/\\evil.example", "javascript:alert(1)"} {
		rec := postTheme(t, f, themeForm("mode=light&redirect="+target))
		assert.Equal(t, "/", rec.Header().Get("Location"), target)
	}
}

func TestTheme_SignedInPersistsForThePrincipalOnly(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	me := seedUser(t, f, "me@example.com")
	other := seedUser(t, f, "other@example.com")
	fam := web.DefaultFamily()
	p := session.Principal{UserID: me.ID, Email: me.Email}
	r := f.authedRequest(t, http.MethodPost, "/theme", "family="+fam+"&mode=dark&redirect=/", p)

	rec := postTheme(t, f, r)
	require.Equal(t, http.StatusSeeOther, rec.Code)

	got, err := f.UserStore.ByID(context.Background(), me.ID)
	require.NoError(t, err)
	assert.Equal(t, fam, got.Theme)
	assert.Equal(t, web.ModeDark, got.ColorMode)

	untouched, err := f.UserStore.ByID(context.Background(), other.ID)
	require.NoError(t, err)
	assert.Empty(t, untouched.Theme)
	assert.Empty(t, untouched.ColorMode)

	sp, _, ok := f.Deps.LoadSession(r)
	require.True(t, ok)
	assert.Equal(t, fam, sp.Theme)
	assert.Equal(t, web.ModeDark, sp.ColorMode)
}

func TestTheme_HTMXRequestGetsFullPageRedirect(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	r := themeForm("family=forest&mode=system&redirect=/orgs")
	r.Header.Set("HX-Request", "true")
	rec := postTheme(t, f, r)
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, "/orgs", rec.Header().Get("HX-Redirect"))
	assert.Empty(t, rec.Header().Get("Location"))
	assert.Equal(t, "forest-system", themeCookie(t, rec))
}

func TestTheme_RouteIsPostOnly(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	rec := postTheme(t, f, httptest.NewRequest(http.MethodGet, "/theme", nil))
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestBase_ResolvesTheme(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	fam := web.DefaultFamily()
	withCookie := func(r *http.Request, v string) *http.Request {
		r.AddCookie(&http.Cookie{Name: web.ThemeCookieName, Value: v})
		return r
	}
	signedIn := func(mode string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		return r.WithContext(session.PrincipalInto(r.Context(), session.Principal{UserID: uuid.New(), Theme: fam, ColorMode: mode}))
	}
	for _, tc := range []struct {
		name   string
		r      *http.Request
		attr   string
		mode   string
		family string
	}{
		{"no cookie, signed out", httptest.NewRequest(http.MethodGet, "/", nil), "", web.ModeSystem, ""},
		{"cookie dark, signed out", withCookie(httptest.NewRequest(http.MethodGet, "/", nil), fam+"-dark"), fam + "-dark", web.ModeDark, ""},
		{"old palette cookie", withCookie(httptest.NewRequest(http.MethodGet, "/", nil), "slate"), "", web.ModeSystem, ""},
		{"account light beats cookie dark", withCookie(signedIn(web.ModeLight), fam+"-dark"), fam + "-light", web.ModeLight, ""},
		{"account system beats cookie dark", withCookie(signedIn(web.ModeSystem), fam+"-dark"), "", web.ModeSystem, ""},
		{"no account choice falls back to cookie", withCookie(signedIn(""), fam+"-dark"), fam + "-dark", web.ModeDark, ""},
		{"cookie system with another family", withCookie(httptest.NewRequest(http.MethodGet, "/", nil), "forest-system"), "", web.ModeSystem, "forest"},
		{"cookie light with another family", withCookie(httptest.NewRequest(http.MethodGet, "/", nil), "glacier-light"), "glacier-light", web.ModeLight, ""},
	} {
		d := f.Deps.Base(tc.r, "T")
		assert.Equal(t, tc.attr, d.ThemeAttr, tc.name)
		assert.Equal(t, tc.mode, d.Theme.Mode, tc.name)
		assert.Equal(t, tc.family, d.FamilyAttr, tc.name)
	}
}

func TestTheme_SignOutReturnsSignedOutPagesToSystem(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	me := seedUser(t, f, "dark@example.com")
	mux := http.NewServeMux()
	handlers.NewThemeHandler(f.Deps, f.Users).Register(mux)
	handlers.NewAuthHandler(f.Deps, newAuthSvc(t, f), f.Users, f.Orgs, f.Projects, nil, nil).Register(mux)
	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	site, err := url.Parse("http://example.com/")
	require.NoError(t, err)
	send := func(r *http.Request) *httptest.ResponseRecorder {
		for _, c := range jar.Cookies(site) {
			r.AddCookie(c)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, r)
		jar.SetCookies(site, rec.Result().Cookies())
		return rec
	}

	signedIn := f.authedRequest(t, http.MethodPost, "/theme", "family="+web.DefaultFamily()+"&mode=dark&redirect=/", session.Principal{UserID: me.ID, Email: me.Email})
	for _, c := range signedIn.Cookies() {
		jar.SetCookies(site, []*http.Cookie{c})
	}
	require.Equal(t, http.StatusSeeOther, send(signedIn).Code)

	require.Equal(t, http.StatusSeeOther, send(httptest.NewRequest(http.MethodPost, "/logout", nil)).Code)
	for _, c := range jar.Cookies(site) {
		assert.NotEqual(t, web.ThemeCookieName, c.Name, "sign-out must expire the theme cookie")
	}

	login := send(httptest.NewRequest(http.MethodGet, "/login", nil))
	require.Equal(t, http.StatusOK, login.Code)
	assert.NotContains(t, login.Body.String(), "data-theme=", "signed-out pages follow the OS")
}
