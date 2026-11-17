package handlers_test

import (
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/web/handlers"
)

func TestDeps_CanManage(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	o := &org.Org{ID: uuid.New(), Slug: "acme", Name: "Acme"}
	owner, member := uuid.New(), uuid.New()
	ctx := t.Context()
	require.NoError(t, f.OrgStore.Save(ctx, o))
	require.NoError(t, f.OrgStore.SaveMembership(ctx, &org.Membership{OrgID: o.ID, UserID: owner, Role: org.RoleOwner}))
	require.NoError(t, f.OrgStore.SaveMembership(ctx, &org.Membership{OrgID: o.ID, UserID: member, Role: org.RoleMember}))
	r := httptest.NewRequest("GET", "/", nil)

	assert.True(t, f.Deps.CanManage(handlers.NewProjectScope(r, o, owner)))
	assert.False(t, f.Deps.CanManage(handlers.NewProjectScope(r, o, member)))

	f.OrgStore.MembershipOfErr = &storeDownError{}
	assert.False(t, f.Deps.CanManage(handlers.NewProjectScope(r, o, owner)), "a failed role lookup must refuse")
}
