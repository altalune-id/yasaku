package handlers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/invite"
	"altalune.id/yasaku/internal/limits"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/project"
	"altalune.id/yasaku/internal/testutil/fakes"
	"altalune.id/yasaku/internal/web/handlers"
)

func TestProjectCreate_AtLimitShowsLimitCopy(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.Cfg.Mode = config.ModeCloud
	f.Cfg.Compliance.ContactEmail = "help@example.com"
	ctx := context.Background()
	uid := uuid.New()
	o, err := f.Orgs.Create(ctx, org.CreateRequest{Slug: "acme", Name: "Acme", OwnerID: uid})
	require.NoError(t, err)

	store := fakes.NewLimits()
	require.NoError(t, store.SetOverride(ctx, limits.Subject{Kind: limits.SubjectOrg, ID: o.ID}, limits.OrgProjects, 0, uuid.Nil))
	f.Deps.Projects = project.NewService(f.ProjStore, discardLogger(), passthroughUnexpected(), fakes.UnitOfWork,
		project.WithLimits(limits.NewService(store, config.ModeCloud), storeOrgNames{f.OrgStore}))

	h := handlers.NewProjectHandler(f.Deps, f.Deps.Projects)
	mux := http.NewServeMux()
	h.Register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, f.authedRequest(t, http.MethodPost, "/orgs/acme/projects", "slug=home&name=Home", session.Principal{UserID: uid, ActiveOrgID: o.ID}))

	body := rec.Body.String()
	assert.Contains(t, body, "Acme can have 0 projects on the Free plan. Rename a project or wipe its data to reuse it. Need more? Tell us at help@example.com.")
	assert.Contains(t, body, "LIM001")
}

type seatCounter struct{ members int64 }

func (c seatCounter) CountMembers(context.Context, uuid.UUID) (int64, error)    { return c.members, nil }
func (c seatCounter) CountOrgsJoined(context.Context, uuid.UUID) (int64, error) { return 0, nil }
func (c seatCounter) IsStaff(context.Context, uuid.UUID) (bool, error)          { return false, nil }
func (c seatCounter) OrgName(context.Context, uuid.UUID) (string, error)        { return "Acme", nil }

type storeOrgNames struct{ store *fakes.Org }

func (n storeOrgNames) OrgName(ctx context.Context, orgID uuid.UUID) (string, error) {
	o, err := n.store.ByID(ctx, orgID)
	if err != nil {
		return "", err
	}
	return o.Name, nil
}

func invitesWithSeats(t *testing.T, f *handlerFixture, orgID uuid.UUID, maxSeats int64) *invite.Service {
	t.Helper()
	store := fakes.NewLimits()
	if maxSeats != limits.Unlimited {
		require.NoError(t, store.SetOverride(context.Background(), limits.Subject{Kind: limits.SubjectOrg, ID: orgID}, limits.OrgMembers, maxSeats, uuid.Nil))
	}
	mode := config.ModeCloud
	if maxSeats == limits.Unlimited {
		mode = config.ModeSelfhosted
	}
	send := invite.NewSendWorkflow(f.InvStore, nopMailer{}, "http://localhost", discardLogger(), passthroughUnexpected(),
		invite.WithSendUnitOfWork(fakes.UnitOfWork), invite.WithSendLimits(limits.NewService(store, mode), seatCounter{members: 1}))
	return invite.NewService(f.InvStore, send, nil, true, discardLogger(), passthroughUnexpected())
}

func TestInvites_SeatCounterAndRevokeLink(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.Deps.Caps.InvitesEnabled = true
	uid := uuid.New()
	o, err := f.Orgs.Create(context.Background(), org.CreateRequest{Slug: "acme", Name: "Acme", OwnerID: uid})
	require.NoError(t, err)
	invites := invitesWithSeats(t, f, o.ID, 5)

	h := handlers.NewInviteHandler(f.Deps, f.Orgs, invites)
	mux := http.NewServeMux()
	h.Register(mux)
	p := session.Principal{UserID: uid, ActiveOrgID: o.ID}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, f.authedRequest(t, http.MethodPost, "/orgs/acme/invites", "email=b@c.co&role=member", p))
	require.Equal(t, http.StatusSeeOther, rec.Code)

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, f.authedRequest(t, http.MethodGet, "/orgs/acme/invites", "", p))
	body := rec.Body.String()
	assert.Contains(t, body, `data-seats="2/5/1"`)
	assert.Contains(t, body, "limits.seats")
	assert.Regexp(t, `action="/orgs/acme/invites/[0-9a-f-]+/revoke"`, body)
}

func TestInvites_NoCounterWhenUnlimited(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.Deps.Caps.InvitesEnabled = true
	uid := uuid.New()
	o, err := f.Orgs.Create(context.Background(), org.CreateRequest{Slug: "acme", Name: "Acme", OwnerID: uid})
	require.NoError(t, err)

	h := handlers.NewInviteHandler(f.Deps, f.Orgs, invitesWithSeats(t, f, o.ID, limits.Unlimited))
	mux := http.NewServeMux()
	h.Register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, f.authedRequest(t, http.MethodGet, "/orgs/acme/invites", "", session.Principal{UserID: uid, ActiveOrgID: o.ID}))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.NotContains(t, rec.Body.String(), "data-seats")
}

func TestLayout_SignInNoticeShownOnceThenCleared(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	uid := uuid.New()
	notice := "Couldn't join Acme: You're already in 5 organizations, the most allowed."
	h := handlers.NewOrgHandler(f.Deps, f.Orgs)
	mux := http.NewServeMux()
	h.Register(mux)

	r := f.authedRequest(t, http.MethodGet, "/orgs", "", session.Principal{UserID: uid, Notice: notice})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, r)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Couldn&#39;t join Acme")

	p, _, ok := f.Deps.LoadSession(r)
	require.True(t, ok)
	assert.Empty(t, p.Notice)
}
