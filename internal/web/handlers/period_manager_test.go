package handlers_test

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/testutil/fakes"
	"altalune.id/yasaku/internal/user"
)

func (f *periodFixture) addMember(t *testing.T) uuid.UUID {
	t.Helper()
	u, err := f.Users.Create(t.Context(), user.CreateRequest{Email: "member@yasaku.test", Name: "Member", Source: user.SourceLocal})
	require.NoError(t, err)
	_, err = f.Orgs.AddMember(fakes.Owner(t.Context(), tenant.Context{OrgID: f.Org.ID, UserID: f.User.ID}), f.Org.ID, u.ID, org.RoleMember)
	require.NoError(t, err)
	return u.ID
}

func (f *periodFixture) as(userID uuid.UUID) {
	f.Principal = session.Principal{UserID: userID, ActiveOrgID: f.Org.ID, ActiveProjectID: f.Project.ID, IssuedAt: time.Now().UTC()}
}

// SECURITY: membership is org-wide, so a plain member reaches these routes; the service gate must turn them away with a 403.
func TestPeriods_MemberCannotCloseOrReopen(t *testing.T) {
	f := newPeriodFixture(t, defaultCaps())
	cur := f.seedBooks(t)
	closePath := f.path("/periods/" + cur.ID.String() + "/close")
	reopenPath := f.path("/periods/" + cur.ID.String() + "/reopen")
	member := f.addMember(t)
	f.as(member)

	page := f.do(t, http.MethodGet, f.path("/periods"), nil)
	require.Equal(t, http.StatusOK, page.Code)
	assert.NotContains(t, page.Body.String(), closePath, "a member is offered no close")

	rec := f.do(t, http.MethodPost, closePath, url.Values{"end_date": {f.today(t).String()}})
	assert.Equal(t, http.StatusForbidden, rec.Code)
	got, err := f.Periods.ByID(f.scoped(t), cur.ID)
	require.NoError(t, err)
	assert.False(t, got.IsLocked(), "a refused close leaves the period open")

	f.as(f.User.ID)
	rec = f.do(t, http.MethodPost, closePath, url.Values{"end_date": {f.today(t).String()}})
	require.Equal(t, http.StatusSeeOther, rec.Code)

	f.as(member)
	page = f.do(t, http.MethodGet, f.path("/periods"), nil)
	require.Equal(t, http.StatusOK, page.Code)
	assert.NotContains(t, page.Body.String(), reopenPath, "a member is offered no reopen")

	rec = f.do(t, http.MethodPost, reopenPath, nil)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	got, err = f.Periods.ByID(f.scoped(t), cur.ID)
	require.NoError(t, err)
	assert.True(t, got.IsLocked(), "a refused reopen leaves the period closed")
}

func TestPeriods_MemberCannotRename(t *testing.T) {
	f := newPeriodFixture(t, defaultCaps())
	cur := f.seedBooks(t)
	member := f.addMember(t)
	f.as(member)

	rec := f.do(t, http.MethodPost, f.path("/periods/"+cur.ID.String()+"/rename"), url.Values{"name": {"Mine"}})
	assert.Equal(t, http.StatusForbidden, rec.Code)
	got, err := f.Periods.ByID(f.scoped(t), cur.ID)
	require.NoError(t, err)
	assert.Equal(t, cur.Name, got.Name)

	f.as(f.User.ID)
	rec = f.do(t, http.MethodPost, f.path("/periods/"+cur.ID.String()+"/rename"), url.Values{"name": {"Ours"}})
	require.Equal(t, http.StatusSeeOther, rec.Code)
}
