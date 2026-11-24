package handlers_test

import (
	"context"
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

func TestMembers_SetRoleRoute(t *testing.T) {
	f := newFixture(t)
	owner, admin, member := uuid.New(), uuid.New(), uuid.New()
	o, err := f.Orgs.Create(context.Background(), org.CreateRequest{Slug: "acme", Name: "Acme", OwnerID: owner})
	require.NoError(t, err)
	require.NoError(t, f.OrgStore.SaveMembership(context.Background(), &org.Membership{OrgID: o.ID, UserID: admin, Role: org.RoleAdmin}))
	require.NoError(t, f.OrgStore.SaveMembership(context.Background(), &org.Membership{OrgID: o.ID, UserID: member, Role: org.RoleMember}))
	mux := http.NewServeMux()
	handlers.NewOrgHandler(f.Deps, f.Orgs).Register(mux)
	post := func(as, target uuid.UUID, role string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, f.authedRequest(t, http.MethodPost, "/orgs/acme/members/"+target.String()+"/role", "role="+role, session.Principal{UserID: as, ActiveOrgID: o.ID}))
		return w
	}
	roleOf := func(id uuid.UUID) org.Role {
		m, err := f.OrgStore.MembershipOf(context.Background(), o.ID, id)
		require.NoError(t, err)
		return m.Role
	}

	assert.Equal(t, http.StatusForbidden, post(member, admin, "member").Code, "a member holds no members:write")
	w := post(admin, member, "owner")
	assert.Equal(t, http.StatusForbidden, w.Code, "only an owner makes an owner")
	assert.Contains(t, w.Body.String(), "role")
	assert.Equal(t, org.RoleMember, roleOf(member))

	w = post(owner, owner, "admin")
	assert.Equal(t, http.StatusConflict, w.Code, "the last owner cannot demote themselves")
	assert.Contains(t, w.Body.String(), "Acme needs at least one owner. Make someone else owner first.")

	assert.Equal(t, http.StatusSeeOther, post(owner, member, "owner").Code)
	assert.Equal(t, org.RoleOwner, roleOf(member))
	assert.Equal(t, http.StatusSeeOther, post(owner, owner, "admin").Code, "with a second owner the first may step down")
	assert.Equal(t, org.RoleAdmin, roleOf(owner))
}
