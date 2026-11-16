package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/project"
	"altalune.id/yasaku/internal/web/handlers"
)

func (f *handlerFixture) seedProject(t *testing.T, o *org.Org, owner uuid.UUID, slug string) *project.Project {
	t.Helper()
	p, err := f.Projects.Create(setTenant(t.Context(), o.ID, owner), o.ID, slug, slug)
	require.NoError(t, err)
	return p
}

func (f *handlerFixture) landing(t *testing.T, p session.Principal) string {
	t.Helper()
	mux := http.NewServeMux()
	handlers.NewHomeHandler(f.Deps, f.Orgs, f.Projects).Register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, f.authedRequest(t, http.MethodGet, "/", "", p))
	require.Equal(t, http.StatusSeeOther, rec.Code)
	return rec.Header().Get("Location")
}

func TestHome_GetRoot_LandsOnTheLastProject(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	u := uuid.New()
	o := f.seedOrg(t, "acme", u)
	f.seedProject(t, o, u, "alpha")
	beta := f.seedProject(t, o, u, "beta")

	got := f.landing(t, session.Principal{UserID: u, ActiveOrgID: o.ID, ActiveProjectID: beta.ID})

	assert.Equal(t, "/orgs/acme/projects/beta/overview", got)
}

func TestHome_GetRoot_NeverLandsInAnOrgTheUserLeft(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	owner, u := uuid.New(), uuid.New()
	left := f.seedOrg(t, "left", owner)
	gone := f.seedProject(t, left, owner, "gone")
	_, err := f.Orgs.AddMember(setTenant(t.Context(), left.ID, owner), left.ID, u, org.RoleMember)
	require.NoError(t, err)
	require.NoError(t, f.Orgs.RemoveMember(setTenant(t.Context(), left.ID, owner), left.ID, u))
	home := f.seedOrg(t, "home", u)
	f.seedProject(t, home, u, "mine")

	got := f.landing(t, session.Principal{UserID: u, ActiveOrgID: left.ID, ActiveProjectID: gone.ID})

	assert.Equal(t, "/orgs/home/projects/mine/overview", got)
}

func TestHome_GetRoot_IgnoresAProjectFromAnotherTenant(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	stranger, u := uuid.New(), uuid.New()
	other := f.seedOrg(t, "other", stranger)
	secret := f.seedProject(t, other, stranger, "secret")
	home := f.seedOrg(t, "home", u)
	f.seedProject(t, home, u, "one")
	f.seedProject(t, home, u, "two")

	got := f.landing(t, session.Principal{UserID: u, ActiveOrgID: home.ID, ActiveProjectID: secret.ID})

	assert.Equal(t, "/orgs", got)
}

func TestHome_GetRoot_LandsOnTheOnlyProject(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	u := uuid.New()
	o := f.seedOrg(t, "acme", u)
	f.seedProject(t, o, u, "alpha")

	got := f.landing(t, session.Principal{UserID: u})

	assert.Equal(t, "/orgs/acme/projects/alpha/overview", got)
}

func TestHome_GetRoot_FallsBackToTheOrgList(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	u := uuid.New()
	a := f.seedOrg(t, "acme", u)
	f.seedProject(t, a, u, "alpha")
	b := f.seedOrg(t, "bolt", u)
	f.seedProject(t, b, u, "beta")

	got := f.landing(t, session.Principal{UserID: u, ActiveOrgID: a.ID})

	assert.Equal(t, "/orgs", got)
}

func (f *handlerFixture) switchTo(t *testing.T, slug string, p session.Principal) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	handlers.NewHomeHandler(f.Deps, f.Orgs, f.Projects).Register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, f.authedRequest(t, http.MethodGet, "/orgs/"+slug+"/switch", "", p))
	return rec
}

func TestHome_SwitchOrg_LandsOnTheOnlyProject(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	u := uuid.New()
	a := f.seedOrg(t, "acme", u)
	alpha := f.seedProject(t, a, u, "alpha")
	b := f.seedOrg(t, "bolt", u)
	f.seedProject(t, b, u, "beta")

	rec := f.switchTo(t, "bolt", session.Principal{UserID: u, ActiveOrgID: a.ID, ActiveProjectID: alpha.ID})

	require.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, "/orgs/bolt/projects/beta/overview", rec.Header().Get("Location"))
}

func TestHome_SwitchOrg_LandsOnTheLastProject(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	u := uuid.New()
	b := f.seedOrg(t, "bolt", u)
	f.seedProject(t, b, u, "one")
	two := f.seedProject(t, b, u, "two")

	rec := f.switchTo(t, "bolt", session.Principal{UserID: u, ActiveOrgID: b.ID, ActiveProjectID: two.ID})

	require.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, "/orgs/bolt/projects/two/overview", rec.Header().Get("Location"))
}

func TestHome_SwitchOrg_WithSeveralProjectsAndNoLastGoesToTheOrg(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	u := uuid.New()
	a := f.seedOrg(t, "acme", u)
	b := f.seedOrg(t, "bolt", u)
	f.seedProject(t, b, u, "one")
	f.seedProject(t, b, u, "two")

	rec := f.switchTo(t, "bolt", session.Principal{UserID: u, ActiveOrgID: a.ID})

	require.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, "/orgs/bolt", rec.Header().Get("Location"))
}

func TestHome_SwitchOrg_IgnoresAProjectFromAnotherTenant(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	stranger, u := uuid.New(), uuid.New()
	other := f.seedOrg(t, "other", stranger)
	secret := f.seedProject(t, other, stranger, "secret")
	b := f.seedOrg(t, "bolt", u)
	f.seedProject(t, b, u, "one")
	f.seedProject(t, b, u, "two")

	rec := f.switchTo(t, "bolt", session.Principal{UserID: u, ActiveOrgID: b.ID, ActiveProjectID: secret.ID})

	require.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, "/orgs/bolt", rec.Header().Get("Location"))
}

func TestHome_SwitchOrg_RefusesAnOrgTheCallerIsNotIn(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	stranger, u := uuid.New(), uuid.New()
	other := f.seedOrg(t, "other", stranger)
	f.seedProject(t, other, stranger, "secret")
	f.seedOrg(t, "home", u)

	rec := f.switchTo(t, "other", session.Principal{UserID: u})

	assert.NotContains(t, rec.Header().Get("Location"), "/orgs/other")
	assert.NotEqual(t, http.StatusSeeOther, rec.Code)
}
