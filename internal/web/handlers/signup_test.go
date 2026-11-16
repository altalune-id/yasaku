package handlers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/user"
	"altalune.id/yasaku/internal/web/handlers"
)

func orgCreate(slug, name string, owner uuid.UUID) org.CreateRequest {
	return org.CreateRequest{Slug: slug, Name: name, OwnerID: owner}
}

func TestSignupHandler_GetSignup_RedirectsUnauth(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.Cfg.Mode = config.ModeCloud
	h := handlers.NewSignupHandler(f.Deps, f.Users, f.Orgs, f.Projects)
	mux := http.NewServeMux()
	h.Register(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/signup/complete", nil))
	assert.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, "/login", rec.Header().Get("Location"))
}

func TestSignupHandler_GetSignup_NotFoundOutsideCloud(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.Cfg.Mode = config.ModeSelfhosted
	h := handlers.NewSignupHandler(f.Deps, f.Users, f.Orgs, f.Projects)
	mux := http.NewServeMux()
	h.Register(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, f.authedRequest(t, http.MethodGet, "/signup/complete", "", session.Principal{UserID: uuid.New()}))
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestSignupHandler_GetSignup_RedirectsWhenAlreadyHasMembership(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.Cfg.Mode = config.ModeCloud
	ctx := context.Background()
	u, err := f.Users.Create(ctx, user.CreateRequest{Email: "alice@example.com", Name: "Alice", Source: user.SourceOIDC})
	require.NoError(t, err)
	_, err = f.Orgs.Create(ctx, orgCreate("acme", "Acme", u.ID))
	require.NoError(t, err)

	h := handlers.NewSignupHandler(f.Deps, f.Users, f.Orgs, f.Projects)
	mux := http.NewServeMux()
	h.Register(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, f.authedRequest(t, http.MethodGet, "/signup/complete", "", session.Principal{UserID: u.ID, Email: u.Email, Name: u.Name}))
	assert.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, "/", rec.Header().Get("Location"))
}

func TestSignupHandler_GetSignup_Renders(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.Cfg.Mode = config.ModeCloud
	ctx := context.Background()
	u, err := f.Users.Create(ctx, user.CreateRequest{Email: "alice@example.com", Name: "Alice", Source: user.SourceOIDC})
	require.NoError(t, err)

	h := handlers.NewSignupHandler(f.Deps, f.Users, f.Orgs, f.Projects)
	mux := http.NewServeMux()
	h.Register(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, f.authedRequest(t, http.MethodGet, "/signup/complete", "", session.Principal{UserID: u.ID, Email: u.Email, Name: u.Name}))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `action="/signup/complete"`)
	assert.NotContains(t, rec.Body.String(), `name="org_slug"`)
	assert.NotContains(t, rec.Body.String(), `name="org_name"`)
	assert.NotContains(t, rec.Body.String(), `name="name"`, "a user with a display name gets no name step")
}

func (f *handlerFixture) postSignup(t *testing.T, body string, p session.Principal) *httptest.ResponseRecorder {
	t.Helper()
	f.Cfg.Mode = config.ModeCloud
	mux := http.NewServeMux()
	handlers.NewSignupHandler(f.Deps, f.Users, f.Orgs, f.Projects).Register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, f.authedRequest(t, http.MethodPost, "/signup/complete", body, p))
	return rec
}

func TestSignupHandler_PostSignup_MissingDisplayNameRerenders(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	u, err := f.Users.Create(t.Context(), user.CreateRequest{Email: "alice@example.com", Name: "x", Source: user.SourceOIDC})
	require.NoError(t, err)

	rec := f.postSignup(t, "name=", session.Principal{UserID: u.ID, Email: u.Email})

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), "signup.error.name_required")
	orgs, err := f.Orgs.List(t.Context(), u.ID)
	require.NoError(t, err)
	assert.Empty(t, orgs)
}

func TestSignupHandler_PostSignup_NamesTheWorkspaceAfterTheUserWithRandomSlugs(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	u, err := f.Users.Create(t.Context(), user.CreateRequest{Email: "alice@example.com", Name: "Alice", Source: user.SourceOIDC})
	require.NoError(t, err)

	body := "org_name=Evil&org_slug=acme&project_name=Main&project_slug=main"
	rec := f.postSignup(t, body, session.Principal{UserID: u.ID, Email: u.Email, Name: u.Name})

	require.Equal(t, http.StatusSeeOther, rec.Code, rec.Body.String())
	orgs, err := f.Orgs.List(t.Context(), u.ID)
	require.NoError(t, err)
	require.Len(t, orgs, 1)
	o := orgs[0]
	assert.Equal(t, "Alice", o.Name)
	assert.Regexp(t, generatedSlugShape, o.Slug, "the org slug is random, never posted or derived from the name")
	assert.Equal(t, u.ID, o.OwnerID)
	projects, err := f.Projects.List(setTenant(t.Context(), o.ID, u.ID), o.ID)
	require.NoError(t, err)
	require.Len(t, projects, 1)
	assert.Equal(t, "Alice", projects[0].Name)
	assert.Regexp(t, generatedSlugShape, projects[0].Slug)
	assert.Equal(t, "/orgs/"+o.Slug+"/projects/"+projects[0].Slug+"/overview", rec.Header().Get("Location"))
}

func TestSignupHandler_PostSignup_AskedDisplayNameNamesTheWorkspace(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	u, err := f.Users.Create(t.Context(), user.CreateRequest{Email: "bo@example.com", Name: "x", Source: user.SourceOIDC})
	require.NoError(t, err)

	rec := f.postSignup(t, "name=Bo+Lee", session.Principal{UserID: u.ID, Email: u.Email})

	require.Equal(t, http.StatusSeeOther, rec.Code, rec.Body.String())
	orgs, err := f.Orgs.List(t.Context(), u.ID)
	require.NoError(t, err)
	require.Len(t, orgs, 1)
	assert.Equal(t, "Bo Lee", orgs[0].Name)
	got, err := f.Users.ByID(t.Context(), u.ID)
	require.NoError(t, err)
	assert.Equal(t, "Bo Lee", got.Name)
}

func TestSignupHandler_UnusableIdPNameAsksForADisplayName(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.Cfg.Mode = config.ModeCloud
	u, err := f.Users.Create(t.Context(), user.CreateRequest{Email: "long@example.com", Name: "x", Source: user.SourceOIDC})
	require.NoError(t, err)
	p := session.Principal{UserID: u.ID, Email: u.Email, Name: strings.Repeat("a", 201)}
	mux := http.NewServeMux()
	handlers.NewSignupHandler(f.Deps, f.Users, f.Orgs, f.Projects).Register(mux)

	page := httptest.NewRecorder()
	mux.ServeHTTP(page, f.authedRequest(t, http.MethodGet, "/signup/complete", "", p))
	require.Equal(t, http.StatusOK, page.Code)
	assert.Contains(t, page.Body.String(), `name="name"`, "an IdP name the workspace cannot use must not dead-end the form")

	rec := f.postSignup(t, "name=Bo", p)
	require.Equal(t, http.StatusSeeOther, rec.Code, rec.Body.String())
	orgs, err := f.Orgs.List(t.Context(), u.ID)
	require.NoError(t, err)
	require.Len(t, orgs, 1)
	assert.Equal(t, "Bo", orgs[0].Name)
}

func TestSignupHandler_KnownNameIgnoresAPostedName(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	u, err := f.Users.Create(t.Context(), user.CreateRequest{Email: "alice@example.com", Name: "Alice", Source: user.SourceOIDC})
	require.NoError(t, err)

	rec := f.postSignup(t, "name=Other", session.Principal{UserID: u.ID, Email: u.Email, Name: u.Name})

	require.Equal(t, http.StatusSeeOther, rec.Code, rec.Body.String())
	orgs, err := f.Orgs.List(t.Context(), u.ID)
	require.NoError(t, err)
	require.Len(t, orgs, 1)
	assert.Equal(t, "Alice", orgs[0].Name)
	got, err := f.Users.ByID(t.Context(), u.ID)
	require.NoError(t, err)
	assert.Equal(t, "Alice", got.Name)
}
