package handlers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/i18n"
	"altalune.id/yasaku/internal/invite"
	"altalune.id/yasaku/internal/limits"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform/authz"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/role"
	"altalune.id/yasaku/internal/testutil/fakes"
	"altalune.id/yasaku/internal/web/handlers"
)

type notSystemOrgs struct{}

func (notSystemOrgs) IsSystem(context.Context, uuid.UUID) (bool, error) { return false, nil }

type rolePerms struct{ store *fakes.Role }

func (p rolePerms) RoleName(ctx context.Context, id uuid.UUID) (string, error) {
	r, err := p.store.ByID(ctx, id)
	if err != nil {
		return "", err
	}
	return r.Name, nil
}

func (p rolePerms) PermissionsOf(ctx context.Context, _ string, id uuid.UUID) ([]authz.Permission, error) {
	return p.store.Permissions(ctx, id)
}

type roleFixture struct {
	*handlerFixture
	org   *org.Org
	owner uuid.UUID
	admin uuid.UUID
	roles *role.Service
	store *fakes.Role
	mux   *http.ServeMux
}

func newRoleFixture(t *testing.T, maxRoles int64) *roleFixture {
	t.Helper()
	f := newFixture(t)
	f.Cfg.Mode = config.ModeCloud
	f.Deps.Caps.InvitesEnabled = true
	ctx := context.Background()
	owner, admin := uuid.New(), uuid.New()
	o, err := f.Orgs.Create(ctx, org.CreateRequest{Slug: "acme", Name: "Acme", OwnerID: owner})
	require.NoError(t, err)
	require.NoError(t, f.OrgStore.SaveMembership(ctx, &org.Membership{OrgID: o.ID, UserID: admin, Role: org.RoleAdmin}))
	lim := fakes.NewLimits()
	require.NoError(t, lim.SetOverride(ctx, limits.Subject{Kind: limits.SubjectOrg, ID: o.ID}, limits.OrgCustomRoles, maxRoles, uuid.Nil))
	store := fakes.NewRole()
	roles := role.NewService(store, notSystemOrgs{}, discardLogger(), passthroughUnexpected(), fakes.UnitOfWork,
		role.WithLimits(limits.NewService(lim, config.ModeCloud), storeOrgNames{f.OrgStore}))
	f.Deps.Roles = roles
	mux := http.NewServeMux()
	handlers.NewRoleHandler(f.Deps, roles).Register(mux)
	handlers.NewOrgHandler(f.Deps, f.Orgs).Register(mux)
	send := invite.NewSendWorkflow(f.InvStore, nopMailer{}, "http://localhost", discardLogger(), passthroughUnexpected())
	invites := invite.NewService(f.InvStore, send, nil, true, discardLogger(), passthroughUnexpected(), invite.WithPermissions(rolePerms{store}), invite.WithRoleNames(rolePerms{store}))
	handlers.NewInviteHandler(f.Deps, f.Orgs, invites).Register(mux)
	return &roleFixture{handlerFixture: f, org: o, owner: owner, admin: admin, roles: roles, store: store, mux: mux}
}

func (rf *roleFixture) seed(t *testing.T, name string, perms ...authz.Permission) *role.Role {
	t.Helper()
	r, err := rf.roles.Create(fakes.Owner(context.Background(), tenant.Context{OrgID: rf.org.ID}), name, "", perms)
	require.NoError(t, err)
	return r
}

func (rf *roleFixture) do(t *testing.T, method, path string, form url.Values, as uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := rf.authedRequest(t, method, path, form.Encode(), session.Principal{UserID: as, ActiveOrgID: rf.org.ID})
	req = req.WithContext(i18n.TranslatorInto(req.Context(), i18n.NewEmbeddedBundle(i18n.EnUS).For(i18n.EnUS)))
	rf.mux.ServeHTTP(w, req)
	return w
}

func TestRoles_ListShowsBuiltinsAndCustom(t *testing.T) {
	t.Parallel()
	rf := newRoleFixture(t, 5)
	free := rf.seed(t, "Bookkeeper", authz.WalletsRead)
	held := rf.seed(t, "Auditor", authz.TransactionsRead)
	rf.store.Hold(held.ID)

	w := rf.do(t, http.MethodGet, "/orgs/acme/roles", nil, rf.owner)
	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, "Bookkeeper")
	assert.Contains(t, body, "Built-in")
	assert.Contains(t, body, `href="/orgs/acme/roles/new"`)
	assert.Contains(t, body, `data-dialog-open="role-delete-`+free.ID.String()+`"`, "an unheld role can be deleted")
	assert.NotContains(t, body, `data-dialog-open="role-delete-`+held.ID.String()+`"`, "a held role cannot be deleted")
	assert.Contains(t, body, `aria-describedby="role-held-`+held.ID.String()+`"`, "the disabled delete says why")
}

func TestRoles_AdminSeesListButCannotEdit(t *testing.T) {
	t.Parallel()
	rf := newRoleFixture(t, 5)
	rf.seed(t, "Bookkeeper", authz.WalletsRead)

	w := rf.do(t, http.MethodGet, "/orgs/acme/roles", nil, rf.admin)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "Bookkeeper")
	assert.NotContains(t, w.Body.String(), `/orgs/acme/roles/new`, "an admin lacks roles:write")
	assert.Equal(t, http.StatusForbidden, rf.do(t, http.MethodGet, "/orgs/acme/roles/new", nil, rf.admin).Code)
	assert.Equal(t, http.StatusForbidden, rf.do(t, http.MethodPost, "/orgs/acme/roles", url.Values{"name": {"X"}}, rf.admin).Code)
}

func TestRoles_CreateEditDelete(t *testing.T) {
	t.Parallel()
	rf := newRoleFixture(t, 5)

	w := rf.do(t, http.MethodGet, "/orgs/acme/roles/new?from=member", nil, rf.owner)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `value="wallets:write" checked`, "copy from member pre-checks its permissions")
	assert.NotContains(t, w.Body.String(), `value="members:write" checked`)

	w = rf.do(t, http.MethodPost, "/orgs/acme/roles", url.Values{"name": {"Bookkeeper"}, "perm": {"wallets:read", "categories:read"}}, rf.owner)
	require.Equal(t, http.StatusSeeOther, w.Code, w.Body.String())
	list, err := rf.roles.List(fakes.Owner(context.Background(), tenant.Context{OrgID: rf.org.ID}))
	require.NoError(t, err)
	require.Len(t, list, 1)
	id := list[0].ID.String()

	w = rf.do(t, http.MethodGet, "/orgs/acme/roles/"+id, nil, rf.owner)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `value="Bookkeeper"`)
	assert.Contains(t, w.Body.String(), `value="categories:read" checked`)

	w = rf.do(t, http.MethodPost, "/orgs/acme/roles/"+id, url.Values{"name": {"Accountant"}, "perm": {"wallets:read"}, "version": {"1"}}, rf.owner)
	require.Equal(t, http.StatusSeeOther, w.Code, w.Body.String())
	got, err := rf.roles.Get(fakes.Owner(context.Background(), tenant.Context{OrgID: rf.org.ID}), list[0].ID)
	require.NoError(t, err)
	assert.Equal(t, "Accountant", got.Name)

	w = rf.do(t, http.MethodPost, "/orgs/acme/roles/"+id+"/delete", nil, rf.owner)
	require.Equal(t, http.StatusSeeOther, w.Code, w.Body.String())
	_, err = rf.roles.Get(fakes.Owner(context.Background(), tenant.Context{OrgID: rf.org.ID}), list[0].ID)
	assert.True(t, role.IsNotFoundError(err))
}

func TestRoles_RefusalsRendered(t *testing.T) {
	t.Parallel()
	rf := newRoleFixture(t, 5)
	held := rf.seed(t, "Auditor", authz.TransactionsRead)
	rf.store.Hold(held.ID)

	cases := map[string]struct {
		path   string
		form   url.Values
		status int
		code   string
	}{
		"empty name":        {"/orgs/acme/roles", url.Values{"perm": {"wallets:read"}}, http.StatusUnprocessableEntity, "ROL007"},
		"duplicate name":    {"/orgs/acme/roles", url.Values{"name": {"auditor"}}, http.StatusConflict, "ROL008"},
		"unknown perm":      {"/orgs/acme/roles", url.Values{"name": {"X"}, "perm": {"wallets:*"}}, http.StatusUnprocessableEntity, "ROL005"},
		"delete held":       {"/orgs/acme/roles/" + held.ID.String() + "/delete", nil, http.StatusConflict, "ROL003"},
		"stale edit":        {"/orgs/acme/roles/" + held.ID.String(), url.Values{"name": {"Auditor"}, "version": {"9"}}, http.StatusConflict, "ROL009"},
		"missing role edit": {"/orgs/acme/roles/" + uuid.NewString(), url.Values{"name": {"Y"}}, http.StatusNotFound, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			w := rf.do(t, http.MethodPost, tc.path, tc.form, rf.owner)
			assert.Equal(t, tc.status, w.Code)
			if tc.code != "" {
				assert.Contains(t, w.Body.String(), "<code class=\"font-mono\">"+tc.code+"</code>")
			}
		})
	}
}

func TestRoles_CreateDisabledAtLimit(t *testing.T) {
	t.Parallel()
	for name, maxRoles := range map[string]int64{"at the limit": 1, "below": 5} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			rf := newRoleFixture(t, maxRoles)
			rf.seed(t, "Bookkeeper", authz.WalletsRead)
			for _, path := range []string{"/orgs/acme/roles", "/orgs/acme/roles/new"} {
				body := rf.do(t, http.MethodGet, path, nil, rf.owner).Body.String()
				if maxRoles == 1 {
					assert.Contains(t, body, limitBlocked, path)
					assert.Contains(t, body, "Acme can have 1 custom role. Delete it to add another.", path)
					continue
				}
				assert.NotContains(t, body, limitBlocked, path)
				assert.NotContains(t, body, "custom role.", path)
			}
		})
	}
}

func TestRolePickers_OfferCoveredCustomRoles(t *testing.T) {
	t.Parallel()
	rf := newRoleFixture(t, 5)
	member := uuid.New()
	require.NoError(t, rf.OrgStore.SaveMembership(context.Background(), &org.Membership{OrgID: rf.org.ID, UserID: member, Role: org.RoleMember}))
	plain := rf.seed(t, "Bookkeeper", authz.WalletsRead)
	powerful := rf.seed(t, "Role keeper", authz.RolesWrite)
	plainOpt := `<option value="` + plain.ID.String() + `"`
	powerfulOpt := `<option value="` + powerful.ID.String() + `"`

	for _, path := range []string{"/orgs/acme/members", "/orgs/acme/invites"} {
		asOwner := rf.do(t, http.MethodGet, path, nil, rf.owner).Body.String()
		assert.Contains(t, asOwner, plainOpt, path)
		assert.Contains(t, asOwner, powerfulOpt, path)
		asAdmin := rf.do(t, http.MethodGet, path, nil, rf.admin).Body.String()
		assert.Contains(t, asAdmin, plainOpt, path)
		assert.NotContains(t, asAdmin, powerfulOpt, "an admin is offered only roles it covers: "+path)
	}
}

func TestInviteSend_CustomRoleEscalationRendered(t *testing.T) {
	t.Parallel()
	rf := newRoleFixture(t, 5)
	powerful := rf.seed(t, "Role keeper", authz.RolesWrite)

	w := rf.do(t, http.MethodPost, "/orgs/acme/invites", url.Values{"email": {"new@example.com"}, "role": {powerful.ID.String()}}, rf.admin)
	body := w.Body.String()
	assert.Contains(t, body, "<code class=\"font-mono\">ROL004</code>")
	assert.Contains(t, body, "Role keeper")
	assert.False(t, strings.Contains(body, "new@example.com</p>"), "no invite was sent")
}
