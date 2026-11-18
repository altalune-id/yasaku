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

// SECURITY: membership is org-wide, so a plain member reaches the project routes; the service gate must answer 403 and create nothing.
func TestProjectHandler_MemberCannotCreateAProject(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	owner, member := uuid.New(), uuid.New()
	o := f.seedOrg(t, "acme", owner)
	_, err := f.Orgs.AddMember(setTenant(t.Context(), o.ID, owner), o.ID, member, org.RoleMember)
	require.NoError(t, err)
	mux := http.NewServeMux()
	handlers.NewProjectHandler(f.Deps, f.Projects).Register(mux)
	as := func(uid uuid.UUID, method, target, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, f.authedRequest(t, method, target, body, session.Principal{UserID: uid, ActiveOrgID: o.ID}))
		return rec
	}

	assert.Equal(t, http.StatusForbidden, as(member, http.MethodGet, "/orgs/acme/projects/new", "").Code)
	assert.Equal(t, http.StatusForbidden, as(member, http.MethodPost, "/orgs/acme/projects", "slug=side&name=Side").Code)
	items, err := f.Projects.List(setTenant(t.Context(), o.ID, owner), o.ID)
	require.NoError(t, err)
	assert.Empty(t, items, "a refused create stores nothing")

	assert.Equal(t, http.StatusOK, as(owner, http.MethodGet, "/orgs/acme/projects/new", "").Code)
	assert.Equal(t, http.StatusSeeOther, as(owner, http.MethodPost, "/orgs/acme/projects", "slug=side&name=Side").Code)
}
