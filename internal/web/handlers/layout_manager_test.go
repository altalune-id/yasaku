package handlers_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform/session"
)

func TestLayoutForOrg_ActiveOrgCarriesWhetherTheCallerManages(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	owner, member := uuid.New(), uuid.New()
	o := f.seedOrg(t, "acme", owner)
	_, err := f.Orgs.AddMember(setTenant(t.Context(), o.ID, owner), o.ID, member, org.RoleMember)
	require.NoError(t, err)

	layout := func(uid uuid.UUID) bool {
		r := f.authedRequest(t, http.MethodGet, "/orgs/acme/projects", "", session.Principal{UserID: uid, ActiveOrgID: o.ID})
		d := f.Deps.LayoutForOrg(r, "t", "acme", "projects")
		require.NotNil(t, d.ActiveOrg)
		return d.ActiveOrg.CanManage
	}
	assert.True(t, layout(owner))
	assert.False(t, layout(member))
}
