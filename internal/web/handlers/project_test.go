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
	handlers.NewProjectHandler(f.Deps, f.Projects).Register(mux)
	return f, mux, owner, o
}

func (f *handlerFixture) postRename(t *testing.T, mux *http.ServeMux, o *org.Org, uid uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, f.authedRequest(t, http.MethodPost, "/orgs/acme/projects/alpha/rename", "name=Renamed", session.Principal{UserID: uid, ActiveOrgID: o.ID}))
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
