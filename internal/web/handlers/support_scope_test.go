package handlers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform/authz"
	"altalune.id/yasaku/internal/platform/capabilities"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/testutil/fakes"
)

type supportRoles struct{ org.Counter }

func (supportRoles) CustomPermissions(context.Context, uuid.UUID) ([]authz.Permission, error) {
	return nil, nil
}

type supportFixture struct {
	*handlerFixture
	staff, outsider uuid.UUID
	acme            *org.Org
}

func newSupportFixture(t *testing.T) supportFixture {
	t.Helper()
	f := newFixture(t)
	caps := capabilities.Capabilities{SupportAccess: true}
	f.Deps.Orgs = org.NewService(f.OrgStore, caps, discardLogger(), passthroughUnexpected(), fakes.UnitOfWork)
	f.Deps.Authz = authz.NewResolver(supportRoles{org.NewCounter(f.OrgStore, caps)})

	ctx := context.Background()
	staff, outsider := uuid.New(), uuid.New()
	sys := &org.Org{ID: uuid.New(), Slug: "ops", Name: "Ops", System: true, OwnerID: staff, CreatedAt: time.Now()}
	require.NoError(t, f.OrgStore.Save(ctx, sys))
	require.NoError(t, f.OrgStore.SaveMembership(ctx, &org.Membership{OrgID: sys.ID, UserID: staff, Role: org.RoleOwner}))
	acme, err := f.Orgs.Create(ctx, org.CreateRequest{Slug: "acme", Name: "Acme", OwnerID: uuid.New()})
	require.NoError(t, err)
	return supportFixture{handlerFixture: f, staff: staff, outsider: outsider, acme: acme}
}

func (f supportFixture) live(orgID uuid.UUID) *session.Support {
	return &session.Support{OrgID: orgID, Reason: "ticket 42", Until: time.Now().Add(time.Hour), EventID: uuid.New()}
}

func TestOrgScopeFor_StaffNeedsLiveSession(t *testing.T) {
	f := newSupportFixture(t)

	refused := map[string]session.Principal{
		"no session":          {UserID: f.staff, Source: session.SourceOIDC},
		"session for another": {UserID: f.staff, Source: session.SourceOIDC, Support: f.live(uuid.New())},
		"forged by non-staff": {UserID: f.outsider, Source: session.SourceOIDC, Support: f.live(f.acme.ID)},
	}
	for name, p := range refused {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			_, _, ok := f.Deps.OrgScopeFor(w, f.authedRequest(t, http.MethodGet, "/orgs/acme", "", p), p, "acme")
			assert.False(t, ok)
			assert.Equal(t, http.StatusNotFound, w.Code, "a non-member gets the same 404 as a bad slug")
		})
	}

	t.Run("expired session exits to /admin", func(t *testing.T) {
		p := session.Principal{UserID: f.staff, Source: session.SourceOIDC,
			Support: &session.Support{OrgID: f.acme.ID, Until: time.Now().Add(-time.Minute)}}
		r := f.authedRequest(t, http.MethodGet, "/orgs/acme", "", p)
		w := httptest.NewRecorder()
		_, _, ok := f.Deps.OrgScopeFor(w, r, p, "acme")
		assert.False(t, ok)
		assert.Equal(t, http.StatusSeeOther, w.Code)
		assert.Equal(t, "/admin", w.Header().Get("Location"))
		saved, _, ok := f.Deps.LoadSession(r)
		require.True(t, ok)
		assert.Nil(t, saved.Support, "expiry clears the support session")
	})

	t.Run("live session admits read-only", func(t *testing.T) {
		p := session.Principal{UserID: f.staff, Source: session.SourceOIDC, Support: f.live(f.acme.ID)}
		o, r, ok := f.Deps.OrgScopeFor(httptest.NewRecorder(), f.authedRequest(t, http.MethodGet, "/orgs/acme", "", p), p, "acme")
		require.True(t, ok)
		assert.Equal(t, f.acme.ID, o.ID)
		tc, err := tenant.From(r.Context())
		require.NoError(t, err)
		assert.True(t, tc.ReadOnly, "every transaction in a support scope is READ ONLY")
		require.NoError(t, authz.Can(r.Context(), authz.WalletsRead))
		assert.True(t, authz.IsDeniedError(authz.Can(r.Context(), authz.WalletsWrite)))
	})

	t.Run("a member keeps a writable scope", func(t *testing.T) {
		owner := session.Principal{UserID: f.acme.OwnerID, Source: session.SourceOIDC}
		_, r, ok := f.Deps.OrgScopeFor(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/orgs/acme", nil), owner, "acme")
		require.True(t, ok)
		tc, err := tenant.From(r.Context())
		require.NoError(t, err)
		assert.False(t, tc.ReadOnly)
	})
}

func TestSupportScope_PostRefused(t *testing.T) {
	f := newSupportFixture(t)
	p := session.Principal{UserID: f.staff, Source: session.SourceOIDC, Support: f.live(f.acme.ID)}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			w := httptest.NewRecorder()
			_, _, ok := f.Deps.OrgScopeFor(w, f.authedRequest(t, method, "/orgs/acme", "", p), p, "acme")
			assert.False(t, ok)
			assert.Equal(t, http.StatusForbidden, w.Code)
			assert.True(t, strings.Contains(w.Body.String(), "SUP004"), "the refusal carries its code")
		})
	}
	w := httptest.NewRecorder()
	_, _, ok := f.Deps.OrgScopeFor(w, f.authedRequest(t, http.MethodHead, "/orgs/acme", "", p), p, "acme")
	assert.True(t, ok, "HEAD reads")
}

func TestRequireStaff(t *testing.T) {
	f := newSupportFixture(t)
	for name, tc := range map[string]struct {
		user uuid.UUID
		ok   bool
	}{"staff": {f.staff, true}, "non-staff": {f.outsider, false}} {
		t.Run(name, func(t *testing.T) {
			p := session.Principal{UserID: tc.user, Source: session.SourceOIDC}
			w := httptest.NewRecorder()
			got, sid, ok := f.Deps.RequireStaff(w, f.authedRequest(t, http.MethodGet, "/admin", "", p))
			assert.Equal(t, tc.ok, ok)
			if !tc.ok {
				assert.Equal(t, http.StatusNotFound, w.Code, "non-staff learn nothing about /admin")
				return
			}
			assert.Equal(t, tc.user, got.UserID)
			assert.NotEmpty(t, sid)
		})
	}
}
