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

func TestInvites_MemberReadsTheList(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	owner, member := uuid.New(), uuid.New()
	o, err := f.Orgs.Create(context.Background(), org.CreateRequest{Slug: "acme", Name: "Acme", OwnerID: owner})
	require.NoError(t, err)
	require.NoError(t, f.OrgStore.SaveMembership(context.Background(), &org.Membership{OrgID: o.ID, UserID: member, Role: org.RoleMember}))
	mux := http.NewServeMux()
	handlers.NewInviteHandler(f.Deps, f.Orgs, f.Invites).Register(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, f.authedRequest(t, http.MethodGet, "/orgs/acme/invites", "", session.Principal{UserID: member, ActiveOrgID: o.ID}))
	assert.Equal(t, http.StatusOK, w.Code, "a member reads the invites page; only send and revoke need members:write")
}
