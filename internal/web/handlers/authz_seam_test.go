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
	"altalune.id/yasaku/internal/platform/authz"
	"altalune.id/yasaku/internal/platform/capabilities"
	"altalune.id/yasaku/internal/platform/session"
)

type countingRoles struct {
	store org.Store
	calls int
}

func (c *countingRoles) RoleOf(ctx context.Context, orgID, userID uuid.UUID) (string, uuid.UUID, string, error) {
	c.calls++
	return org.NewCounter(c.store, capabilities.Capabilities{}).RoleOf(ctx, orgID, userID)
}

func (*countingRoles) CustomPermissions(context.Context, uuid.UUID) ([]authz.Permission, error) {
	return nil, nil
}
func (*countingRoles) IsStaff(context.Context, uuid.UUID) (bool, error) { return false, nil }

func TestResolve_OnceAtSeam(t *testing.T) {
	f := newFixture(t)
	owner := uuid.New()
	_, err := f.Orgs.Create(context.Background(), org.CreateRequest{Slug: "acme", Name: "Acme", OwnerID: owner})
	require.NoError(t, err)
	roles := &countingRoles{store: f.OrgStore}
	f.Deps.Authz = authz.NewResolver(roles)

	p := session.Principal{UserID: owner, Source: session.SourceOIDC}
	_, r, ok := f.Deps.OrgScopeFor(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/orgs/acme", nil), p, "acme")
	require.True(t, ok)
	held := session.PrincipalFrom(r.Context()).Held
	assert.Contains(t, held, string(authz.RolesWrite), "the owner's held set rides the scoped request")
	assert.Equal(t, 1, roles.calls, "the seam asks the role source once per request")
	require.NoError(t, authz.Can(r.Context(), authz.MembersWrite))
	assert.Equal(t, 1, roles.calls, "Can never asks again")
}
