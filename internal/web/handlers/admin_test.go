package handlers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/i18n"
	"altalune.id/yasaku/internal/limits"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/testutil/fakes"
	"altalune.id/yasaku/internal/user"
	"altalune.id/yasaku/internal/web/handlers"
)

type noticeSink struct {
	mu   sync.Mutex
	sent []handlers.SupportNotice
}

func (s *noticeSink) notify(_ context.Context, n handlers.SupportNotice) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent = append(s.sent, n)
	return nil
}

func (s *noticeSink) all() []handlers.SupportNotice {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]handlers.SupportNotice(nil), s.sent...)
}

type suspendCall struct {
	Caller, Target uuid.UUID
	Suspend        bool
}

type fakeSuspender struct {
	mu    sync.Mutex
	calls []suspendCall
	err   error
}

func (s *fakeSuspender) record(ctx context.Context, id uuid.UUID, suspend bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, suspendCall{Caller: session.PrincipalFrom(ctx).UserID, Target: id, Suspend: suspend})
	return s.err
}

func (s *fakeSuspender) Suspend(ctx context.Context, id uuid.UUID) error {
	return s.record(ctx, id, true)
}

func (s *fakeSuspender) Unsuspend(ctx context.Context, id uuid.UUID) error {
	return s.record(ctx, id, false)
}

type adminFixture struct {
	supportFixture
	suspender *fakeSuspender
	audit     *fakes.Audit
	limits    *fakes.Limits
	notices   *noticeSink
	mux       *http.ServeMux
	beta      *org.Org
}

func newAdminFixture(t *testing.T) adminFixture {
	t.Helper()
	sf := newSupportFixture(t)
	sf.Cfg.Compliance.ContactEmail = "help@example.com"
	beta, err := sf.Orgs.Create(context.Background(), org.CreateRequest{Slug: "beta", Name: "Beta", OwnerID: uuid.New()})
	require.NoError(t, err)
	f := adminFixture{supportFixture: sf, suspender: &fakeSuspender{}, audit: &fakes.Audit{}, limits: fakes.NewLimits(), notices: &noticeSink{}, mux: http.NewServeMux(), beta: beta}
	h := handlers.NewAdminHandler(sf.Deps, handlers.AdminDeps{
		Directory:  sf.UserStore,
		Audit:      f.audit,
		UnitOfWork: fakes.UnitOfWork,
		Limits:     f.limits,
		Notify:     f.notices.notify,
		Suspender:  f.suspender,
	})
	h.Register(f.mux)
	return f
}

func (f adminFixture) do(t *testing.T, method, target string, form url.Values, p session.Principal) (*httptest.ResponseRecorder, *http.Request) {
	t.Helper()
	body := ""
	if form != nil {
		body = form.Encode()
	}
	r := f.authedRequest(t, method, target, body, p)
	r = r.WithContext(i18n.TranslatorInto(r.Context(), i18n.NewEmbeddedBundle(i18n.EnUS).For(i18n.EnUS)))
	w := httptest.NewRecorder()
	f.mux.ServeHTTP(w, r)
	return w, r
}

func (f adminFixture) saved(t *testing.T, r *http.Request) session.Principal {
	t.Helper()
	p, _, ok := f.Deps.LoadSession(r)
	require.True(t, ok)
	return p
}

func (f adminFixture) staffP(s *session.Support) session.Principal {
	return session.Principal{UserID: f.staff, Email: "ops@example.com", Source: session.SourceOIDC, Support: s}
}

func adminRoutes() [][2]string {
	return [][2]string{
		{http.MethodGet, "/admin"},
		{http.MethodGet, "/admin/users"},
		{http.MethodGet, "/admin/orgs/acme"},
		{http.MethodPost, "/admin/orgs/acme/assume"},
		{http.MethodPost, "/admin/support/renew"},
		{http.MethodPost, "/admin/support/exit"},
		{http.MethodGet, "/admin/limits"},
		{http.MethodPost, "/admin/limits"},
		{http.MethodPost, "/admin/limits/overrides"},
		{http.MethodPost, "/admin/limits/overrides/delete"},
		{http.MethodGet, "/admin/activity"},
		{http.MethodPost, "/admin/users/" + uuid.NewString() + "/suspend"},
		{http.MethodPost, "/admin/users/" + uuid.NewString() + "/unsuspend"},
	}
}

func TestAdmin_NonStaff404(t *testing.T) {
	f := newAdminFixture(t)
	form := url.Values{"reason": {"ticket"}, "key": {"org.projects"}, "value": {"1"}, "subject": {"acme"}}
	for _, rt := range adminRoutes() {
		t.Run(rt[0]+" "+rt[1], func(t *testing.T) {
			outsider := session.Principal{UserID: f.outsider, Source: session.SourceOIDC}
			recs, sent, calls := len(f.audit.Records()), len(f.notices.all()), len(f.suspender.calls)
			w, _ := f.do(t, rt[0], rt[1], form, outsider)
			assert.Equal(t, http.StatusNotFound, w.Code, "non-staff learn nothing about /admin")
			assert.Len(t, f.audit.Records(), recs, "a refused caller writes nothing")
			assert.Len(t, f.notices.all(), sent)
			assert.Len(t, f.suspender.calls, calls)

			w, _ = f.do(t, rt[0], rt[1], form, f.staffP(nil))
			assert.NotEqual(t, http.StatusNotFound, w.Code, "staff reach the route")
		})
	}
}

func TestAdmin_StaffPagesRender(t *testing.T) {
	f := newAdminFixture(t)
	for _, path := range []string{"/admin", "/admin/users", "/admin/orgs/acme", "/admin/limits", "/admin/activity"} {
		w, _ := f.do(t, http.MethodGet, path, nil, f.staffP(nil))
		assert.Equal(t, http.StatusOK, w.Code, "%s: %s", path, w.Body.String())
	}
	w, _ := f.do(t, http.MethodGet, "/admin/orgs/acme", nil, f.staffP(nil))
	assert.Contains(t, w.Body.String(), `action="/admin/orgs/acme/assume"`)
	assert.Contains(t, w.Body.String(), "Access ends in 60 minutes")
}

func TestAssume_RequiresReason(t *testing.T) {
	f := newAdminFixture(t)
	w, r := f.do(t, http.MethodPost, "/admin/orgs/acme/assume", url.Values{"reason": {"   "}}, f.staffP(nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "SUP001")
	assert.Nil(t, f.saved(t, r).Support, "no reason, no visit")
	assert.Empty(t, f.audit.Records())
	assert.Empty(t, f.notices.all())
}

func TestAssume_WritesEventAndQueuesOwnerMail(t *testing.T) {
	f := newAdminFixture(t)
	before := time.Now()
	w, r := f.do(t, http.MethodPost, "/admin/orgs/acme/assume", url.Values{"reason": {"ticket 42"}}, f.staffP(nil))
	require.Equal(t, http.StatusSeeOther, w.Code, w.Body.String())
	assert.Equal(t, "/orgs/acme", w.Header().Get("Location"))

	s := f.saved(t, r).Support
	require.NotNil(t, s)
	assert.Equal(t, f.acme.ID, s.OrgID)
	assert.Equal(t, "ticket 42", s.Reason)
	assert.WithinDuration(t, before.Add(session.SupportTTL), s.Until, 5*time.Second)
	assert.NotEqual(t, uuid.Nil, s.EventID)

	recs := f.audit.Records()
	require.Len(t, recs, 1)
	assert.Equal(t, "support.entered", recs[0].Action)
	assert.Equal(t, f.acme.ID, recs[0].OrgID, "recorded under the target org")
	assert.Equal(t, "ticket 42", recs[0].Reason)
	assert.True(t, recs[0].InTx)

	sent := f.notices.all()
	require.Len(t, sent, 1, "one owner notice, queued after the commit")
	n := sent[0]
	assert.Equal(t, f.acme.ID, n.OrgID)
	assert.Equal(t, "Altalune support viewed Acme", n.Subject)
	assert.Contains(t, n.Body, "ops@example.com")
	assert.Contains(t, n.Body, "ticket 42")
	assert.Contains(t, n.Body, "http://localhost/orgs/acme/activity")
	assert.Contains(t, n.Body, "Questions? help@example.com")
}

func TestAssume_MailFailureKeepsTheVisit(t *testing.T) {
	f := newAdminFixture(t)
	h := handlers.NewAdminHandler(f.Deps, handlers.AdminDeps{
		Directory: f.UserStore, Audit: f.audit, UnitOfWork: fakes.UnitOfWork, Limits: f.limits,
		Notify: func(context.Context, handlers.SupportNotice) error { return assert.AnError },
	})
	mux := http.NewServeMux()
	h.Register(mux)
	r := f.authedRequest(t, http.MethodPost, "/admin/orgs/acme/assume", url.Values{"reason": {"ticket"}}.Encode(), f.staffP(nil))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	assert.Equal(t, http.StatusSeeOther, w.Code)
	assert.NotNil(t, f.saved(t, r).Support)
}

func TestAssume_SwitchingOrgExitsPrevious(t *testing.T) {
	f := newAdminFixture(t)
	w, r := f.do(t, http.MethodPost, "/admin/orgs/beta/assume", url.Values{"reason": {"second"}}, f.staffP(f.live(f.acme.ID)))
	require.Equal(t, http.StatusSeeOther, w.Code, w.Body.String())

	recs := f.audit.Records()
	require.Len(t, recs, 2)
	assert.Equal(t, "support.exited", recs[0].Action)
	assert.Equal(t, f.acme.ID, recs[0].OrgID)
	assert.Equal(t, "support.entered", recs[1].Action)
	assert.Equal(t, f.beta.ID, recs[1].OrgID)
	assert.Equal(t, f.beta.ID, f.saved(t, r).Support.OrgID)
}

// TestAssume_MemberGoesStraightIn proves a staff member of the target org opens no support visit: a visit would make their own org read-only.
func TestAssume_MemberGoesStraightIn(t *testing.T) {
	f := newAdminFixture(t)
	_, err := f.Orgs.AddMember(context.Background(), f.beta.ID, f.staff, org.RoleMember)
	require.NoError(t, err)
	w, r := f.do(t, http.MethodPost, "/admin/orgs/beta/assume", url.Values{"reason": {"ticket"}}, f.staffP(nil))
	require.Equal(t, http.StatusSeeOther, w.Code, w.Body.String())
	assert.Equal(t, "/orgs/beta", w.Header().Get("Location"))
	assert.Nil(t, f.saved(t, r).Support, "no visit opened")
	assert.Empty(t, f.audit.Records(), "no support.entered event")
	assert.Empty(t, f.notices.all(), "no owner notice")
}

func TestRenew(t *testing.T) {
	f := newAdminFixture(t)
	s := f.live(f.acme.ID)
	s.Until = time.Now().Add(5 * time.Minute)
	w, r := f.do(t, http.MethodPost, "/admin/support/renew", url.Values{"return_to": {"/orgs/acme/members"}}, f.staffP(s))
	require.Equal(t, http.StatusSeeOther, w.Code, w.Body.String())
	assert.Equal(t, "/orgs/acme/members", w.Header().Get("Location"))
	got := f.saved(t, r).Support
	require.NotNil(t, got)
	assert.WithinDuration(t, time.Now().Add(session.SupportTTL), got.Until, 5*time.Second)
	assert.Equal(t, s.EventID, got.EventID, "renew extends the same visit")
	assert.Equal(t, s.Reason, got.Reason)

	t.Run("an off-site return_to falls back to /admin", func(t *testing.T) {
		for _, bad := range []string{"https://evil.example/x", "//evil.example/x", "/\\evil.example/x", "javascript:alert(1)"} {
			w, _ := f.do(t, http.MethodPost, "/admin/support/renew", url.Values{"return_to": {bad}}, f.staffP(f.live(f.acme.ID)))
			require.Equal(t, http.StatusSeeOther, w.Code, bad)
			assert.Equal(t, "/admin", w.Header().Get("Location"), bad)
		}
	})

	t.Run("an expired visit cannot be renewed", func(t *testing.T) {
		old := f.live(f.acme.ID)
		old.Until = time.Now().Add(-time.Minute)
		w, r := f.do(t, http.MethodPost, "/admin/support/renew", url.Values{}, f.staffP(old))
		assert.Equal(t, http.StatusSeeOther, w.Code)
		assert.Equal(t, "/admin", w.Header().Get("Location"))
		assert.Nil(t, f.saved(t, r).Support)
	})
}

func TestExit(t *testing.T) {
	f := newAdminFixture(t)
	w, r := f.do(t, http.MethodPost, "/admin/support/exit", url.Values{}, f.staffP(f.live(f.acme.ID)))
	require.Equal(t, http.StatusSeeOther, w.Code, w.Body.String())
	assert.Equal(t, "/admin", w.Header().Get("Location"))
	assert.Nil(t, f.saved(t, r).Support)
	recs := f.audit.Records()
	require.Len(t, recs, 1)
	assert.Equal(t, "support.exited", recs[0].Action)
	assert.Equal(t, f.acme.ID, recs[0].OrgID)
}

func TestAdminLimits_SetClearDelete(t *testing.T) {
	f := newAdminFixture(t)
	ctx := context.Background()
	staff := f.staffP(nil)
	sys, err := f.Orgs.SystemOrg(ctx)
	require.NoError(t, err)

	w, _ := f.do(t, http.MethodPost, "/admin/limits", url.Values{"key": {"org.projects"}, "value": {"7"}}, staff)
	require.Equal(t, http.StatusSeeOther, w.Code, w.Body.String())
	plan, err := f.limits.ListPlan(ctx, limits.DefaultPlan)
	require.NoError(t, err)
	assert.Equal(t, []limits.PlanEntry{{Plan: limits.DefaultPlan, Key: limits.OrgProjects, Value: 7}}, plan)

	w, _ = f.do(t, http.MethodPost, "/admin/limits", url.Values{"key": {"org.projects"}, "value": {""}}, staff)
	require.Equal(t, http.StatusSeeOther, w.Code, w.Body.String())
	plan, err = f.limits.ListPlan(ctx, limits.DefaultPlan)
	require.NoError(t, err)
	assert.Empty(t, plan, "an empty value clears the plan row")

	w, _ = f.do(t, http.MethodPost, "/admin/limits/overrides", url.Values{"key": {"org.projects"}, "subject": {"acme"}, "value": {"unlimited"}}, staff)
	require.Equal(t, http.StatusSeeOther, w.Code, w.Body.String())
	ovs, err := f.limits.ListOverrides(ctx)
	require.NoError(t, err)
	assert.Equal(t, []limits.Override{{Subject: limits.Subject{Kind: limits.SubjectOrg, ID: f.acme.ID}, Key: limits.OrgProjects, Value: limits.Unlimited}}, ovs)

	w, _ = f.do(t, http.MethodPost, "/admin/limits/overrides/delete",
		url.Values{"key": {"org.projects"}, "kind": {"org"}, "id": {f.acme.ID.String()}}, staff)
	require.Equal(t, http.StatusSeeOther, w.Code, w.Body.String())
	ovs, err = f.limits.ListOverrides(ctx)
	require.NoError(t, err)
	assert.Empty(t, ovs)

	assert.Equal(t, []string{"limit.plan_set", "limit.plan_set", "limit.override_set", "limit.override_deleted"}, f.audit.Actions())
	for _, rec := range f.audit.Records() {
		assert.Equal(t, sys.ID, rec.OrgID, "limit changes are recorded under the system org")
	}

	t.Run("refusals", func(t *testing.T) {
		for name, form := range map[string]url.Values{
			"unknown key":      {"key": {"no.such"}, "value": {"1"}},
			"bad value":        {"key": {"org.projects"}, "value": {"lots"}},
			"negative value":   {"key": {"org.projects"}, "value": {"-5"}},
			"unknown org slug": {"key": {"org.projects"}, "subject": {"nope"}, "value": {"1"}},
		} {
			path := "/admin/limits"
			if form.Has("subject") {
				path = "/admin/limits/overrides"
			}
			w, _ := f.do(t, http.MethodPost, path, form, staff)
			assert.Equal(t, http.StatusBadRequest, w.Code, name)
		}
		assert.Len(t, f.audit.Records(), 4, "a refused change records nothing")
	})
}

func TestAdmin_ActivityListsSystemOrgEvents(t *testing.T) {
	f := newAdminFixture(t)
	staff := f.staffP(nil)
	w, _ := f.do(t, http.MethodPost, "/admin/limits", url.Values{"key": {"org.members"}, "value": {"9"}}, staff)
	require.Equal(t, http.StatusSeeOther, w.Code)
	w, _ = f.do(t, http.MethodGet, "/admin/activity", nil, staff)
	require.Equal(t, http.StatusOK, w.Code)
	assert.True(t, strings.Contains(w.Body.String(), "Plan limit set"), w.Body.String())
}

func TestAdmin_SuspendAndUnsuspend(t *testing.T) {
	f := newAdminFixture(t)
	target := uuid.New()
	w, _ := f.do(t, http.MethodPost, "/admin/users/"+target.String()+"/suspend", url.Values{}, f.staffP(nil))
	require.Equal(t, http.StatusSeeOther, w.Code, w.Body.String())
	assert.Equal(t, "/admin/users", w.Header().Get("Location"))
	w, _ = f.do(t, http.MethodPost, "/admin/users/"+target.String()+"/unsuspend", url.Values{}, f.staffP(nil))
	require.Equal(t, http.StatusSeeOther, w.Code, w.Body.String())
	assert.Equal(t, []suspendCall{{f.staff, target, true}, {f.staff, target, false}}, f.suspender.calls,
		"the service sees the staff caller, which it checks again")

	t.Run("staff are protected", func(t *testing.T) {
		f.suspender.err = &user.StaffProtectedError{}
		w, _ := f.do(t, http.MethodPost, "/admin/users/"+f.staff.String()+"/suspend", url.Values{}, f.staffP(nil))
		assert.Equal(t, http.StatusConflict, w.Code)
		assert.Contains(t, w.Body.String(), "USR009")
	})

	t.Run("bad id", func(t *testing.T) {
		w, _ := f.do(t, http.MethodPost, "/admin/users/nope/suspend", url.Values{}, f.staffP(nil))
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}
