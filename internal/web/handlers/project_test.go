package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/i18n"
	"altalune.id/yasaku/internal/ledger"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/testutil/fakes"
	"altalune.id/yasaku/internal/web/handlers"
)

func renameFixture(t *testing.T) (*handlerFixture, *http.ServeMux, uuid.UUID, *org.Org) {
	t.Helper()
	f := newFixture(t)
	owner := uuid.New()
	o := f.seedOrg(t, "acme", owner)
	_, err := f.Projects.Create(setTenant(t.Context(), o.ID, owner), o.ID, "alpha", "Alpha")
	require.NoError(t, err)
	mux := http.NewServeMux()
	handlers.NewSettingsHandler(f.Deps, f.Projects, ledger.NewService(fakes.NewLedger(), discardLogger(), passthroughUnexpected()), nil).Register(mux)
	return f, mux, owner, o
}

func (f *handlerFixture) postRename(t *testing.T, mux *http.ServeMux, o *org.Org, uid uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()
	return f.postRenameTo(t, mux, o, uid, "alpha", "name=Renamed")
}

func (f *handlerFixture) postRenameTo(t *testing.T, mux *http.ServeMux, o *org.Org, uid uuid.UUID, slug, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, f.authedRequest(t, http.MethodPost, "/orgs/"+o.Slug+"/projects/"+slug+"/rename", body, session.Principal{UserID: uid, ActiveOrgID: o.ID}))
	return rec
}

func (f *handlerFixture) getSettings(t *testing.T, mux *http.ServeMux, o *org.Org, uid uuid.UUID, slug string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, f.authedRequest(t, http.MethodGet, "/orgs/"+o.Slug+"/projects/"+slug+"/settings", "", session.Principal{UserID: uid, ActiveOrgID: o.ID}))
	return rec
}

func (f *handlerFixture) projectName(t *testing.T, o *org.Org, uid uuid.UUID) string {
	t.Helper()
	p, err := f.Projects.BySlug(setTenant(t.Context(), o.ID, uid), o.ID, "alpha")
	require.NoError(t, err)
	return p.Name
}

func TestProjectHandler_PostRename_ManagerRenames(t *testing.T) {
	t.Parallel()
	f, mux, owner, o := renameFixture(t)

	rec := f.postRename(t, mux, o, owner)

	assert.Equal(t, http.StatusSeeOther, rec.Code, rec.Body.String())
	assert.Equal(t, "/orgs/acme/projects/alpha/settings?saved=name", rec.Header().Get("Location"))
	assert.Equal(t, "Renamed", f.projectName(t, o, owner))
}

func TestProjectHandler_PostRename_MemberIsForbidden(t *testing.T) {
	t.Parallel()
	f, mux, owner, o := renameFixture(t)
	member := uuid.New()
	_, err := f.Orgs.AddMember(setTenant(t.Context(), o.ID, owner), o.ID, member, org.RoleMember)
	require.NoError(t, err)

	rec := f.postRename(t, mux, o, member)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, "Alpha", f.projectName(t, o, owner))
}

func TestProjectHandler_PostRename_RefusalIsLocalized(t *testing.T) {
	t.Parallel()
	f, mux, owner, o := renameFixture(t)
	member := uuid.New()
	_, err := f.Orgs.AddMember(setTenant(t.Context(), o.ID, owner), o.ID, member, org.RoleMember)
	require.NoError(t, err)
	r := f.authedRequest(t, http.MethodPost, "/orgs/"+o.Slug+"/projects/alpha/rename", "name=Renamed", session.Principal{UserID: member, ActiveOrgID: o.ID})
	tr := i18n.NewEmbeddedBundle(i18n.EnUS).For(i18n.IdID)
	r = r.WithContext(i18n.TranslatorInto(r.Context(), tr))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, r)

	require.Equal(t, http.StatusForbidden, rec.Code)
	assert.Contains(t, rec.Body.String(), tr.T("settings.name_forbidden_title"))
	assert.Contains(t, rec.Body.String(), tr.T("settings.name_member"))
	assert.NotContains(t, rec.Body.String(), "You must be an admin or owner")
}

type storeDownError struct{}

func (*storeDownError) Error() string { return "store down" }

func TestProjectHandler_PostRename_RoleLookupFailureIsForbidden(t *testing.T) {
	t.Parallel()
	f, mux, owner, o := renameFixture(t)
	before := f.OrgStore.MembershipOfCalls()
	f.OrgStore.MembershipOfFailAfter = before + 1
	f.OrgStore.MembershipOfErr = &storeDownError{}

	rec := f.postRename(t, mux, o, owner)
	f.OrgStore.MembershipOfErr = nil

	require.Equal(t, before+2, f.OrgStore.MembershipOfCalls(), "expected the scope gate then IsManager to read membership; a changed call order makes this test meaningless")
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, "Alpha", f.projectName(t, o, owner))
}

func TestSettings_RenameErrorRerendersSettingsWithAFieldError(t *testing.T) {
	t.Parallel()
	f, mux, owner, o := renameFixture(t)

	rec := f.postRenameTo(t, mux, o, owner, "alpha", "name=+++")

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, `id="settings-name"`, "the error renders the settings page, not an error page")
	assert.Contains(t, body, `aria-invalid="true"`)
	assert.Contains(t, body, "settings.name_error")
	assert.Equal(t, "Alpha", f.projectName(t, o, owner))
}

func TestSettings_GeneralCardShowsTheProjectIDWithCopy(t *testing.T) {
	t.Parallel()
	f, mux, owner, o := renameFixture(t)

	rec := f.getSettings(t, mux, o, owner, "alpha")

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, `id="settings-project-id"`)
	assert.Contains(t, body, `data-copy="alpha"`)
	p, err := f.Projects.BySlug(setTenant(t.Context(), o.ID, owner), o.ID, "alpha")
	require.NoError(t, err)
	assert.Contains(t, body, `data-copy="`+p.ID.String()+`"`, "the internal ids sit under Advanced")
	assert.Contains(t, body, `action="/orgs/acme/projects/alpha/rename"`)
}

func TestSettings_MemberSeesTheNameReadOnly(t *testing.T) {
	t.Parallel()
	f, mux, owner, o := renameFixture(t)
	member := uuid.New()
	_, err := f.Orgs.AddMember(setTenant(t.Context(), o.ID, owner), o.ID, member, org.RoleMember)
	require.NoError(t, err)

	rec := f.getSettings(t, mux, o, member, "alpha")

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "settings.name_member")
	assert.NotContains(t, body, `action="/orgs/acme/projects/alpha/rename"`)
}

func TestSettings_SystemProjectIsLocked(t *testing.T) {
	t.Parallel()
	f, mux, owner, o := renameFixture(t)
	_, err := f.Projects.BootstrapSystem(setTenant(t.Context(), o.ID, owner), o.ID, "main", "Main")
	require.NoError(t, err)

	page := f.getSettings(t, mux, o, owner, "main")
	require.Equal(t, http.StatusOK, page.Code)
	assert.Contains(t, page.Body.String(), "settings.name_locked")
	assert.NotContains(t, page.Body.String(), `action="/orgs/acme/projects/main/rename"`)

	rec := f.postRenameTo(t, mux, o, owner, "main", "name=Other")
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), `id="settings-name"`)
	assert.Contains(t, rec.Body.String(), "settings.name_locked")
}

func TestSettings_SavedNameShowsASuccessAlert(t *testing.T) {
	t.Parallel()
	f, mux, owner, o := renameFixture(t)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, f.authedRequest(t, http.MethodGet, "/orgs/acme/projects/alpha/settings?saved=name", "", session.Principal{UserID: owner, ActiveOrgID: o.ID}))

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "settings.name_saved")
}
