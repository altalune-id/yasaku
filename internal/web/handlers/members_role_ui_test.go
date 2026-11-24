package handlers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/i18n"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/web/handlers"
)

func TestMembers_RoleControlsByViewer(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	owner, admin, admin2, member := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	o, err := f.Orgs.Create(ctx, org.CreateRequest{Slug: "acme", Name: "Acme", OwnerID: owner})
	require.NoError(t, err)
	require.NoError(t, f.OrgStore.SaveMembership(ctx, &org.Membership{OrgID: o.ID, UserID: admin, Role: org.RoleAdmin}))
	require.NoError(t, f.OrgStore.SaveMembership(ctx, &org.Membership{OrgID: o.ID, UserID: member, Role: org.RoleMember}))
	require.NoError(t, f.OrgStore.SaveMembership(ctx, &org.Membership{OrgID: o.ID, UserID: admin2, Role: org.RoleAdmin}))
	mux := http.NewServeMux()
	handlers.NewOrgHandler(f.Deps, f.Orgs).Register(mux)
	get := func(as uuid.UUID) string {
		w := httptest.NewRecorder()
		req := f.authedRequest(t, http.MethodGet, "/orgs/acme/members", "", session.Principal{UserID: as, ActiveOrgID: o.ID})
		req = req.WithContext(i18n.TranslatorInto(req.Context(), i18n.NewEmbeddedBundle(i18n.EnUS).For(i18n.EnUS)))
		mux.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
		return w.Body.String()
	}
	rolePath := `action="/orgs/acme/members/` + member.String() + `/role"`

	asOwner := get(owner)
	assert.Contains(t, asOwner, rolePath, "an owner can change a member's role")
	assert.NotContains(t, asOwner, `<option value="owner"`, "owner is granted only through the Make owner dialog")
	assert.Contains(t, asOwner, `data-dialog-open="member-owner-`+member.String()+`"`, "Make owner opens a confirm")
	assert.Contains(t, asOwner, `id="member-owner-`+member.String()+`"`, "the confirm renders in the dialog host")
	assert.Contains(t, asOwner, `name="role" value="owner"`, "the confirm posts role=owner")

	asAdmin := get(admin)
	assert.Contains(t, asAdmin, rolePath, "an admin holds members:write")
	assert.NotContains(t, asAdmin, `data-dialog-open="member-owner-`, "an admin never sees Make owner")
	assert.NotContains(t, asAdmin, `id="member-owner-`)

	asMember := get(member)
	assert.NotContains(t, asMember, `/role"`, "a member sees no role controls")

	ownerRow := `action="/orgs/acme/members/` + owner.String() + `/role"`
	assert.NotContains(t, asAdmin, ownerRow, "an admin cannot cover an owner, so the owner row has no control")
	assert.NotContains(t, asAdmin, `<option value="owner"`, "an admin is offered only roles it covers")
	assert.Contains(t, asOwner, "Acme needs at least one owner. Make someone else owner first.", "the last owner's control says why it is locked")
	assert.Contains(t, asOwner, "demote yourself", "the Make owner confirm explains the transfer")

	require.NoError(t, f.OrgStore.SaveMembership(ctx, &org.Membership{OrgID: o.ID, UserID: admin, Role: org.RoleOwner}))
	twoOwners := get(owner)
	assert.NotContains(t, twoOwners, "Acme needs at least one owner.", "with a second owner nobody is locked")
	assert.Contains(t, twoOwners, ownerRow, "with a second owner the first may step down")
	assert.NotContains(t, get(admin2), ownerRow, "an admin gets no control on a row it cannot cover")
}
