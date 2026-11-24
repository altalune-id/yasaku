package handlers_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/i18n"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform/audit"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/web/handlers"
	"altalune.id/yasaku/schema"
)

type activityEntries []audit.Entry

func (a activityEntries) List(context.Context, audit.Filter) ([]audit.Entry, error) { return a, nil }

func getActivity(t *testing.T, f *handlerFixture, rec interface {
	List(context.Context, audit.Filter) ([]audit.Entry, error)
}, p session.Principal) *httptest.ResponseRecorder {
	t.Helper()
	h := handlers.NewActivityHandler(f.Deps, f.Orgs, rec)
	mux := http.NewServeMux()
	h.Register(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, f.authedRequest(t, http.MethodGet, "/orgs/acme/activity", "", p))
	return w
}

func TestActivity_ListsOwnOrgOnly(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	uid := uuid.New()
	o, err := f.Orgs.Create(context.Background(), org.CreateRequest{Slug: "acme", Name: "Acme", OwnerID: uid})
	require.NoError(t, err)

	conn, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "activity.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	cfg := config.Defaults()
	require.NoError(t, schema.MigrateUp(context.Background(), conn, cfg))
	pool := db.Pool{W: conn, R: conn}
	rec := audit.NewRecorder(db.DBConfig{Driver: db.DriverSQLite, TablePrefix: cfg.DB.TablePrefix}, pool, nil)

	record := func(orgID uuid.UUID, action string) {
		ctx := session.PrincipalInto(tenant.Into(context.Background(), tenant.Context{OrgID: orgID}),
			session.Principal{UserID: uid, Email: "owner@example.com"})
		require.NoError(t, db.RunInTx(ctx, pool, func(ctx context.Context) error {
			return rec.Record(ctx, audit.Event{Action: action, TargetKind: "invite", TargetID: "inv-1"})
		}))
	}
	record(o.ID, "invite.sent")
	record(uuid.New(), "apikey.created")

	w := getActivity(t, f, rec, session.Principal{UserID: uid, ActiveOrgID: o.ID})
	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, "invite.sent")
	assert.Contains(t, body, "owner@example.com")
	assert.NotContains(t, body, "apikey.created", "another org's events never render")
}

func TestActivity_StaffBadge(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	uid := uuid.New()
	o, err := f.Orgs.Create(context.Background(), org.CreateRequest{Slug: "acme", Name: "Acme", OwnerID: uid})
	require.NoError(t, err)
	now := time.Now()
	rows := activityEntries{
		{ActorKind: audit.ActorStaff, ActorEmail: "staff@altalune.id", Action: "support.entered", Reason: "ticket 42", OccurredAt: now},
		{ActorKind: audit.ActorUser, ActorEmail: "owner@example.com", Action: "org.renamed", OccurredAt: now.Add(-time.Minute)},
	}

	w := getActivity(t, f, rows, session.Principal{UserID: uid, ActiveOrgID: o.ID})
	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Equal(t, 1, strings.Count(body, "activity.staff_badge"), "only the staff row carries the badge")
	assert.Contains(t, body, "ticket 42")
}

func TestActivity_MemberRefused(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	owner, member := uuid.New(), uuid.New()
	o, err := f.Orgs.Create(ctx, org.CreateRequest{Slug: "acme", Name: "Acme", OwnerID: owner})
	require.NoError(t, err)
	require.NoError(t, f.OrgStore.SaveMembership(ctx, &org.Membership{OrgID: o.ID, UserID: member, Role: org.RoleMember}))
	rows := activityEntries{{ActorKind: audit.ActorUser, Action: "org.renamed", OccurredAt: time.Now()}}

	w := getActivity(t, f, rows, session.Principal{UserID: member, ActiveOrgID: o.ID})
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "Only an owner or admin can view activity.")
	assert.NotContains(t, w.Body.String(), "org.renamed")
}

func TestActivity_ReadableLabels(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	uid := uuid.New()
	o, err := f.Orgs.Create(context.Background(), org.CreateRequest{Slug: "acme", Name: "Acme", OwnerID: uid})
	require.NoError(t, err)
	inviteID, userID, orgID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	now := time.Now()
	rows := activityEntries{
		{ActorKind: audit.ActorUser, ActorEmail: "owner@example.com", Action: "invite.sent", TargetKind: "invite", TargetID: inviteID,
			Detail: map[string]string{"email": "new@example.com", "role": "member"}, OccurredAt: now},
		{ActorKind: audit.ActorUser, ActorEmail: "owner@example.com", Action: "member.removed", TargetKind: "user", TargetID: userID,
			Detail: map[string]string{"role": "member"}, OccurredAt: now},
		{ActorKind: audit.ActorSystem, Action: "limit.override_set", TargetKind: "org", TargetID: orgID,
			Detail: map[string]string{"key": "org.projects", "value": "1", "name": orgID}, OccurredAt: now},
		{ActorKind: audit.ActorUser, ActorEmail: "owner@example.com", Action: "future.thing", TargetKind: "widget", TargetID: "w-1", OccurredAt: now},
	}
	h := handlers.NewActivityHandler(f.Deps, f.Orgs, rows)
	mux := http.NewServeMux()
	h.Register(mux)
	req := f.authedRequest(t, http.MethodGet, "/orgs/acme/activity", "", session.Principal{UserID: uid, ActiveOrgID: o.ID})
	req = req.WithContext(i18n.TranslatorInto(req.Context(), i18n.NewEmbeddedBundle(i18n.EnUS).For(i18n.EnUS)))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	for _, want := range []string{"Invite sent", "new@example.com", "Member removed", "Limit override set", "org.projects: 1", "future.thing"} {
		assert.Contains(t, body, want)
	}
	for _, raw := range []string{inviteID, userID, orgID, "activity.action."} {
		assert.NotContains(t, body, raw, "owners never see raw ids or keys")
	}
}
