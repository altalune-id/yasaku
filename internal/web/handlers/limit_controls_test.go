package handlers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/limits"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/project"
	"altalune.id/yasaku/internal/testutil/fakes"
	"altalune.id/yasaku/internal/web/handlers"
)

const limitBlocked = "data-limit-blocked"

func get(t *testing.T, mux *http.ServeMux, f *handlerFixture, path string, p session.Principal) string {
	t.Helper()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, f.authedRequest(t, http.MethodGet, path, "", p))
	require.Equal(t, http.StatusOK, rec.Code, path)
	return rec.Body.String()
}

func TestProjectCreate_DisabledAtLimitEnabledBelow(t *testing.T) {
	t.Parallel()
	reason := "Acme can have 0 projects on the Free plan."
	for name, maxProjects := range map[string]int64{"at the limit": 0, "below": 5} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			f.Cfg.Mode = config.ModeCloud
			ctx := context.Background()
			uid := uuid.New()
			o, err := f.Orgs.Create(ctx, org.CreateRequest{Slug: "acme", Name: "Acme", OwnerID: uid})
			require.NoError(t, err)
			store := fakes.NewLimits()
			require.NoError(t, store.SetOverride(ctx, limits.Subject{Kind: limits.SubjectOrg, ID: o.ID}, limits.OrgProjects, maxProjects, uuid.Nil))
			f.Deps.Projects = project.NewService(f.ProjStore, discardLogger(), passthroughUnexpected(), fakes.UnitOfWork,
				project.WithLimits(limits.NewService(store, config.ModeCloud), storeOrgNames{f.OrgStore}))
			mux := http.NewServeMux()
			handlers.NewProjectHandler(f.Deps, f.Deps.Projects).Register(mux)
			p := session.Principal{UserID: uid, ActiveOrgID: o.ID}
			for _, path := range []string{"/orgs/acme/projects", "/orgs/acme/projects/new"} {
				body := get(t, mux, f, path, p)
				if maxProjects == 0 {
					assert.Contains(t, body, limitBlocked, path)
					assert.Contains(t, body, reason, path)
					continue
				}
				assert.NotContains(t, body, limitBlocked, path)
				assert.NotContains(t, body, reason, path)
			}
		})
	}
}

func TestInviteSend_DisabledAtLimitEnabledBelow(t *testing.T) {
	t.Parallel()
	reason := "Acme has 1 seat, and it is taken (0 pending)."
	for name, maxSeats := range map[string]int64{"at the limit": 1, "below": 5} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			f.Deps.Caps.InvitesEnabled = true
			f.Cfg.Mode = config.ModeCloud
			uid := uuid.New()
			o, err := f.Orgs.Create(context.Background(), org.CreateRequest{Slug: "acme", Name: "Acme", OwnerID: uid})
			require.NoError(t, err)
			mux := http.NewServeMux()
			handlers.NewInviteHandler(f.Deps, f.Orgs, invitesWithSeats(t, f, o.ID, maxSeats)).Register(mux)
			body := get(t, mux, f, "/orgs/acme/invites", session.Principal{UserID: uid, ActiveOrgID: o.ID})
			if maxSeats == 1 {
				assert.Contains(t, body, limitBlocked)
				assert.Contains(t, body, reason)
				return
			}
			assert.NotContains(t, body, limitBlocked)
			assert.NotContains(t, body, reason)
		})
	}
}

func TestOrgNew_DisabledAtLimit(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.Cfg.Mode = config.ModeCloud
	uid := uuid.New()
	_, err := f.Orgs.Create(context.Background(), org.CreateRequest{Slug: "acme", Name: "Acme", OwnerID: uid})
	require.NoError(t, err)
	orgs := org.NewService(f.OrgStore, f.Deps.Caps, discardLogger(), passthroughUnexpected(), fakes.UnitOfWork,
		org.WithLimits(limits.NewService(fakes.NewLimits(), config.ModeCloud)))
	mux := http.NewServeMux()
	handlers.NewOrgHandler(f.Deps, orgs).Register(mux)
	for _, path := range []string{"/orgs", "/orgs/new"} {
		body := get(t, mux, f, path, session.Principal{UserID: uid})
		assert.Contains(t, body, limitBlocked, path)
		assert.Contains(t, body, "You can create one organization", path)
	}
}

func TestOrgNew_EnabledBelowLimit(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.Cfg.Mode = config.ModeCloud
	orgs := org.NewService(f.OrgStore, f.Deps.Caps, discardLogger(), passthroughUnexpected(), fakes.UnitOfWork,
		org.WithLimits(limits.NewService(fakes.NewLimits(), config.ModeCloud)))
	mux := http.NewServeMux()
	handlers.NewOrgHandler(f.Deps, orgs).Register(mux)
	for _, path := range []string{"/orgs", "/orgs/new"} {
		body := get(t, mux, f, path, session.Principal{UserID: uuid.New()})
		assert.NotContains(t, body, limitBlocked, path)
		assert.NotContains(t, body, "You can create one organization", path)
	}
}

func TestLayoutOrgCreate_DisabledAtLimitEnabledBelow(t *testing.T) {
	t.Parallel()
	const reason = "You can create one organization on the Free plan."
	const link = `href="/orgs/new"`
	for name, atLimit := range map[string]bool{"at the limit": true, "below": false} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			f.Cfg.Mode = config.ModeCloud
			ctx := context.Background()
			uid, other := uuid.New(), uuid.New()
			creator := other
			if atLimit {
				creator = uid
			}
			o, err := f.Orgs.Create(ctx, org.CreateRequest{Slug: "acme", Name: "Acme", OwnerID: creator})
			require.NoError(t, err)
			if !atLimit {
				_, err = f.Orgs.AddMember(ctx, o.ID, uid, org.RoleMember)
				require.NoError(t, err)
			}
			f.Deps.Orgs = org.NewService(f.OrgStore, f.Deps.Caps, discardLogger(), passthroughUnexpected(), fakes.UnitOfWork,
				org.WithLimits(limits.NewService(fakes.NewLimits(), config.ModeCloud)))
			mux := http.NewServeMux()
			handlers.NewProjectHandler(f.Deps, f.Deps.Projects).Register(mux)
			body := get(t, mux, f, "/orgs/acme/projects", session.Principal{UserID: uid, ActiveOrgID: o.ID})
			if atLimit {
				assert.NotContains(t, body, link)
				assert.Contains(t, body, limitBlocked)
				assert.Contains(t, body, reason)
				return
			}
			assert.Contains(t, body, link)
			assert.NotContains(t, body, reason)
		})
	}
}

func TestLayoutOrgCreate_HiddenWhenCreationOff(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.Deps.Caps.OrgCreation = false
	mux := http.NewServeMux()
	handlers.NewOrgHandler(f.Deps, f.Deps.Orgs).Register(mux)
	body := get(t, mux, f, "/orgs", session.Principal{UserID: uuid.New()})
	assert.NotContains(t, body, `href="/orgs/new"`)
}

func TestLayoutOrgCreate_SidebarDisabledWithoutActiveOrg(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.Cfg.Mode = config.ModeCloud
	ctx := context.Background()
	uid := uuid.New()
	store := fakes.NewLimits()
	require.NoError(t, store.SetOverride(ctx, limits.Subject{Kind: limits.SubjectUser, ID: uid}, limits.UserOrgsCreated, 0, uuid.Nil))
	f.Deps.Orgs = org.NewService(f.OrgStore, f.Deps.Caps, discardLogger(), passthroughUnexpected(), fakes.UnitOfWork,
		org.WithLimits(limits.NewService(store, config.ModeCloud)))
	mux := http.NewServeMux()
	handlers.NewOrgHandler(f.Deps, f.Deps.Orgs).Register(mux)
	body := get(t, mux, f, "/orgs", session.Principal{UserID: uid})
	assert.NotContains(t, body, `href="/orgs/new"`)
	assert.Contains(t, body, "nav.create_organization")
	assert.Contains(t, body, limitBlocked)
}
