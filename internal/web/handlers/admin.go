package handlers

import (
	"cmp"
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/i18n"
	"altalune.id/yasaku/internal/limits"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform/audit"
	"altalune.id/yasaku/internal/platform/authz"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/user"
	"altalune.id/yasaku/internal/web"
	"altalune.id/yasaku/internal/web/templates"
)

//nolint:gochecknoglobals // the embedded locale bundle, built once and read-only.
var mailBundle = sync.OnceValue(func() *i18n.Bundle { return i18n.NewEmbeddedBundle(i18n.EnUS) })

const (
	adminActivityPageSize = 50
	supportReasonMax      = 500
)

type adminDirectory interface {
	AdminListOrgs(ctx context.Context) ([]user.AdminOrg, error)
	AdminListUsers(ctx context.Context) ([]user.AdminUser, error)
	ByEmail(ctx context.Context, email string) (*user.User, error)
}

type adminAudit interface {
	Record(ctx context.Context, e audit.Event) error
	List(ctx context.Context, f audit.Filter) ([]audit.Entry, error)
}

type adminSuspender interface {
	Suspend(ctx context.Context, userID uuid.UUID) error
	Unsuspend(ctx context.Context, userID uuid.UUID) error
}

// SupportNotice is the email the owners of an org get when staff open a support visit.
type SupportNotice struct {
	OrgID   uuid.UUID `json:"org_id"`
	Subject string    `json:"subject"`
	Body    string    `json:"body"`
}

// AdminDeps is what /admin needs beyond Deps.
type AdminDeps struct {
	Directory  adminDirectory
	Audit      adminAudit
	UnitOfWork tenant.UnitOfWork
	Limits     limits.Store
	// Notify queues the owner email; it runs after the support.entered commit and its failure never undoes the visit.
	Notify    func(ctx context.Context, n SupportNotice) error
	Suspender adminSuspender
}

// AdminHandler owns /admin, the platform staff console.
type AdminHandler struct {
	Deps
	AdminDeps
}

// NewAdminHandler wires the handler.
func NewAdminHandler(d Deps, a AdminDeps) *AdminHandler {
	return &AdminHandler{Deps: d, AdminDeps: a}
}

// Register wires the /admin routes onto the mux.
func (h *AdminHandler) Register(mux web.Mux) {
	mux.HandleFunc("GET /admin", h.GetOrgs)
	mux.HandleFunc("GET /admin/users", h.GetUsers)
	mux.HandleFunc("GET /admin/orgs/{org}", h.GetOrg)
	mux.HandleFunc("POST /admin/orgs/{org}/assume", h.PostAssume)
	mux.HandleFunc("POST /admin/support/renew", h.PostRenew)
	mux.HandleFunc("POST /admin/support/exit", h.PostExit)
	mux.HandleFunc("GET /admin/limits", h.GetLimits)
	mux.HandleFunc("POST /admin/limits", h.PostPlanLimit)
	mux.HandleFunc("POST /admin/limits/overrides", h.PostOverride)
	mux.HandleFunc("POST /admin/limits/overrides/delete", h.PostDeleteOverride)
	mux.HandleFunc("GET /admin/activity", h.GetActivity)
	mux.HandleFunc("POST /admin/users/{user}/suspend", h.PostSuspend)
	mux.HandleFunc("POST /admin/users/{user}/unsuspend", h.PostUnsuspend)
}

// PostSuspend stops a user from signing in or acting anywhere.
func (h *AdminHandler) PostSuspend(w http.ResponseWriter, r *http.Request) {
	h.setSuspended(w, r, h.Suspender.Suspend)
}

// PostUnsuspend lets a suspended user back in.
func (h *AdminHandler) PostUnsuspend(w http.ResponseWriter, r *http.Request) {
	h.setSuspended(w, r, h.Suspender.Unsuspend)
}

// SECURITY: the service checks staff again from the principal on ctx and refuses to suspend staff or the caller.
func (h *AdminHandler) setSuspended(w http.ResponseWriter, r *http.Request, apply func(ctx context.Context, userID uuid.UUID) error) {
	p, _, ok := h.RequireStaff(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(r.PathValue("user"))
	if err != nil {
		h.ErrorPage(w, r, http.StatusNotFound, pageTitle(r, "error_title.not_found"), "", err)
		return
	}
	err = apply(session.PrincipalInto(r.Context(), p), id)
	switch {
	case err == nil:
		http.Redirect(w, r, web.Path(h.Cfg.HTTP.BasePath, "/admin/users"), http.StatusSeeOther)
	case user.IsStaffProtectedError(err):
		h.ErrorPage(w, r, http.StatusConflict, pageTitle(r, "error_title.not_allowed"), (&user.StaffProtectedError{}).ToAppError().Message(), err)
	case user.IsNotFoundError(err):
		h.ErrorPage(w, r, http.StatusNotFound, pageTitle(r, "error_title.not_found"), "", err)
	default:
		h.failed(w, r, "suspend", err)
	}
}

func (h *AdminHandler) layout(r *http.Request) web.LayoutData {
	return h.Layout(r, pageTitle(r, "admin.title"), web.ActiveNav{})
}

func (h *AdminHandler) failed(w http.ResponseWriter, r *http.Request, msg string, err error) {
	h.LogErr("web admin: "+msg, err)
	h.ErrorPage(w, r, http.StatusInternalServerError, pageTitle(r, "error_title.lookup_failed"), "", err)
}

// GetOrgs lists every org with its counts.
func (h *AdminHandler) GetOrgs(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.RequireStaff(w, r); !ok {
		return
	}
	orgs, err := h.Directory.AdminListOrgs(r.Context())
	if err != nil {
		h.failed(w, r, "list orgs", err)
		return
	}
	rows := make([]templates.AdminOrgRow, 0, len(orgs))
	for _, o := range orgs {
		rows = append(rows, templates.AdminOrgRow{Slug: o.Slug, Name: o.Name, Plan: o.Plan, Members: o.MemberCount,
			Projects: o.ProjectCount, CreatedAt: o.CreatedAt, LastActive: o.LastActive})
	}
	Render(w, r, templates.AdminOrgsLayout(h.layout(r), rows))
}

// GetUsers lists every user with sign-in and suspension state.
func (h *AdminHandler) GetUsers(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.RequireStaff(w, r); !ok {
		return
	}
	users, err := h.Directory.AdminListUsers(r.Context())
	if err != nil {
		h.failed(w, r, "list users", err)
		return
	}
	rows := make([]templates.AdminUserRow, 0, len(users))
	for _, u := range users {
		rows = append(rows, templates.AdminUserRow{ID: u.ID.String(), Email: u.Email, Name: u.Name, CreatedAt: u.CreatedAt,
			LastSignIn: u.LastSignInAt, Suspended: u.SuspendedAt, Orgs: u.OrgCount})
	}
	Render(w, r, templates.AdminUsersLayout(h.layout(r), rows))
}

// GetOrg shows one org's projects and the Assume dialog.
func (h *AdminHandler) GetOrg(w http.ResponseWriter, r *http.Request) {
	p, _, ok := h.RequireStaff(w, r)
	if !ok {
		return
	}
	o, ok := h.adminOrg(w, r)
	if !ok {
		return
	}
	// SECURITY: a read-only scope, so listing project names can never write to the org.
	ctx := tenant.Into(r.Context(), tenant.Context{OrgID: o.ID, UserID: p.UserID, ReadOnly: true})
	projects, err := h.Projects.List(ctx, o.ID)
	if err != nil {
		h.failed(w, r, "list projects", err)
		return
	}
	v := templates.AdminOrgView{Slug: o.Slug, Name: o.Name}
	for _, pr := range projects {
		v.Projects = append(v.Projects, pr.Name)
	}
	Render(w, r, templates.AdminOrgLayout(h.layout(r), v))
}

func (h *AdminHandler) adminOrg(w http.ResponseWriter, r *http.Request) (*org.Org, bool) {
	o, err := h.Orgs.BySlug(r.Context(), r.PathValue("org"))
	if err != nil {
		h.ErrorPage(w, r, http.StatusNotFound, pageTitle(r, "error_title.organization_not_found"), "", err)
		return nil, false
	}
	return o, true
}

//i18n:use authz.support_reason_required

// PostAssume opens a read-only support visit to the org, ending any open one first.
func (h *AdminHandler) PostAssume(w http.ResponseWriter, r *http.Request) {
	p, sid, ok := h.RequireStaff(w, r)
	if !ok {
		return
	}
	o, ok := h.adminOrg(w, r)
	if !ok {
		return
	}
	member, err := h.isMember(r, p, o)
	if err != nil {
		h.failed(w, r, "check membership", err)
		return
	}
	if member {
		http.Redirect(w, r, web.Path(h.Cfg.HTTP.BasePath, "/orgs/"+o.Slug), http.StatusSeeOther)
		return
	}
	reason := strings.TrimSpace(r.PostFormValue("reason"))
	if reason == "" {
		h.ErrorPage(w, r, http.StatusBadRequest, pageTitle(r, "error_title.bad_request"),
			i18n.TranslatorFrom(r.Context()).T("authz.support_reason_required"), &authz.SupportReasonRequiredError{})
		return
	}
	reason = truncateRunes(reason, supportReasonMax)
	if err := h.exitVisit(r.Context(), p); err != nil {
		h.failed(w, r, "exit previous visit", err)
		return
	}
	now := time.Now()
	visit := &session.Support{OrgID: o.ID, Reason: reason, Until: now.Add(session.SupportTTL), EventID: uuid.New()}
	p.Support = visit
	notice := h.supportNotice(o, p, now)
	err = h.inOrg(r.Context(), p, o.ID, func(ctx context.Context) error {
		if err := h.Audit.Record(ctx, audit.Event{Action: "support.entered", TargetKind: "org", TargetID: o.ID.String(), Reason: reason}); err != nil {
			return err
		}
		db.AfterCommit(ctx, func() { h.notify(context.WithoutCancel(r.Context()), notice) })
		return nil
	})
	if err != nil {
		h.failed(w, r, "record support visit", err)
		return
	}
	if err := h.UpdateSession(r, sid, p); err != nil {
		h.failed(w, r, "save support visit", err)
		return
	}
	http.Redirect(w, r, web.Path(h.Cfg.HTTP.BasePath, "/orgs/"+o.Slug), http.StatusSeeOther)
}

func (h *AdminHandler) isMember(r *http.Request, p session.Principal, o *org.Org) (bool, error) {
	_, err := h.Orgs.MembershipOf(orgScopedRequest(r, p, o).Context(), o.ID, p.UserID)
	if err == nil {
		return true, nil
	}
	if org.IsMembershipMissingError(err) || org.IsNotFoundError(err) {
		return false, nil
	}
	return false, err
}

func (h *AdminHandler) notify(ctx context.Context, n SupportNotice) {
	if h.Notify == nil {
		return
	}
	if err := h.Notify(ctx, n); err != nil {
		h.LogErr("web admin: queue support notice", err)
	}
}

//i18n:use support.mail_subject
//i18n:use support.mail_body

func (h *AdminHandler) supportNotice(o *org.Org, p session.Principal, at time.Time) SupportNotice {
	tr := mailBundle().For(i18n.EnUS)
	activity := strings.TrimRight(h.Cfg.HTTP.BaseURL, "/") + web.Path(h.Cfg.HTTP.BasePath, "/orgs/"+o.Slug+"/activity")
	return SupportNotice{
		OrgID:   o.ID,
		Subject: tr.T("support.mail_subject", "Org", o.Name),
		Body: tr.T("support.mail_body", "Staff", p.Email, "Org", o.Name, "At", at.UTC().Format("2006-01-02 15:04 UTC"),
			"Reason", p.Support.Reason, "Activity", activity, "Contact", h.Cfg.Compliance.ContactEmail),
	}
}

// SECURITY: a normal, writable tenant transaction on the target org, with the support principal on ctx so the actor records as staff.
func (h *AdminHandler) inOrg(ctx context.Context, p session.Principal, orgID uuid.UUID, fn func(ctx context.Context) error) error {
	ctx = session.PrincipalInto(tenant.Into(ctx, tenant.Context{OrgID: orgID, UserID: p.UserID}), p)
	return h.UnitOfWork(ctx, fn)
}

func (h *AdminHandler) exitVisit(ctx context.Context, p session.Principal) error {
	if p.Support == nil {
		return nil
	}
	return h.inOrg(ctx, p, p.Support.OrgID, func(ctx context.Context) error {
		return h.Audit.Record(ctx, audit.Event{Action: "support.exited", TargetKind: "org", TargetID: p.Support.OrgID.String(), Reason: p.Support.Reason})
	})
}

// PostRenew extends the open visit by another SupportTTL.
func (h *AdminHandler) PostRenew(w http.ResponseWriter, r *http.Request) {
	p, sid, ok := h.RequireStaff(w, r)
	if !ok {
		return
	}
	now := time.Now()
	if p.Support == nil || !p.Support.Live(p.Support.OrgID, now) {
		h.endSupport(w, r)
		return
	}
	p.Support.Until = now.Add(session.SupportTTL)
	if err := h.UpdateSession(r, sid, p); err != nil {
		h.failed(w, r, "renew support visit", err)
		return
	}
	dest := web.Path(h.Cfg.HTTP.BasePath, cmp.Or(SanitizeReturnTo(r.PostFormValue("return_to")), "/admin"))
	http.Redirect(w, r, dest, http.StatusSeeOther) //nolint:gosec // G710: SanitizeReturnTo keeps only a same-origin path, else /admin
}

// PostExit ends the open visit and records it.
func (h *AdminHandler) PostExit(w http.ResponseWriter, r *http.Request) {
	p, sid, ok := h.RequireStaff(w, r)
	if !ok {
		return
	}
	if err := h.exitVisit(r.Context(), p); err != nil {
		h.failed(w, r, "exit support visit", err)
		return
	}
	p.Support = nil
	if err := h.UpdateSession(r, sid, p); err != nil {
		h.failed(w, r, "save support exit", err)
		return
	}
	http.Redirect(w, r, web.Path(h.Cfg.HTTP.BasePath, "/admin"), http.StatusSeeOther)
}

// GetActivity lists the system org's events: limit changes and other operator actions.
func (h *AdminHandler) GetActivity(w http.ResponseWriter, r *http.Request) {
	p, _, ok := h.RequireStaff(w, r)
	if !ok {
		return
	}
	sys, err := h.Orgs.SystemOrg(r.Context())
	if err != nil {
		h.failed(w, r, "system org", err)
		return
	}
	ctx := tenant.Into(r.Context(), tenant.Context{OrgID: sys.ID, UserID: p.UserID, ReadOnly: true})
	entries, err := h.Audit.List(ctx, audit.Filter{Limit: adminActivityPageSize})
	if err != nil {
		h.failed(w, r, "list activity", err)
		return
	}
	v := templates.ActivityView{Rows: activityRows(i18n.TranslatorFrom(r.Context()), entries)}
	Render(w, r, templates.AdminActivityLayout(h.layout(r), v))
}

// GetLimits shows the plan limits and every override.
func (h *AdminHandler) GetLimits(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.RequireStaff(w, r); !ok {
		return
	}
	ctx := r.Context()
	plan, err := h.Limits.ListPlan(ctx, limits.DefaultPlan)
	if err != nil {
		h.failed(w, r, "list plan limits", err)
		return
	}
	overrides, err := h.Limits.ListOverrides(ctx)
	if err != nil {
		h.failed(w, r, "list overrides", err)
		return
	}
	tr := i18n.TranslatorFrom(ctx)
	v := templates.AdminLimitsView{Plan: limits.DefaultPlan}
	for _, def := range limits.Catalogue() {
		row := templates.AdminLimitRow{Key: string(def.Key), Subject: string(def.Subject), Default: formatLimit(tr, h.defaultFor(def))}
		if i := slices.IndexFunc(plan, func(e limits.PlanEntry) bool { return e.Key == def.Key }); i >= 0 {
			row.Value = formatLimit(tr, plan[i].Value)
		}
		v.Rows = append(v.Rows, row)
	}
	for _, o := range overrides {
		v.Overrides = append(v.Overrides, templates.AdminOverrideRow{Key: string(o.Key), Kind: string(o.Subject.Kind),
			ID: o.Subject.ID.String(), Subject: h.subjectName(ctx, o.Subject), Value: formatLimit(tr, o.Value)})
	}
	Render(w, r, templates.AdminLimitsLayout(h.layout(r), v))
}

func (h *AdminHandler) defaultFor(def limits.Def) int64 {
	if h.Cfg.Mode == config.ModeCloud {
		return def.Cloud
	}
	return def.SelfHosted
}

func (h *AdminHandler) subjectName(ctx context.Context, sub limits.Subject) string {
	if sub.Kind == limits.SubjectOrg {
		if o, err := h.Orgs.ByID(ctx, sub.ID); err == nil {
			return o.Slug
		}
	}
	return sub.ID.String()
}

//i18n:use admin_limits.unlimited

func formatLimit(tr *i18n.Translator, v int64) string {
	if v == limits.Unlimited {
		return tr.T("admin_limits.unlimited")
	}
	return strconv.FormatInt(v, 10)
}

//i18n:use admin_limits.error_value
//i18n:use admin_limits.error_key
//i18n:use admin_limits.error_subject

func parseLimit(raw string) (int64, bool) {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "unlimited" {
		return limits.Unlimited, true
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	return v, err == nil && v >= 0
}

func (h *AdminHandler) badLimit(w http.ResponseWriter, r *http.Request, key string, args ...any) {
	h.ErrorPage(w, r, http.StatusBadRequest, pageTitle(r, "error_title.bad_request"), i18n.TranslatorFrom(r.Context()).T(key, args...))
}

func (h *AdminHandler) limitDef(w http.ResponseWriter, r *http.Request) (limits.Def, bool) {
	key := r.PostFormValue("key")
	def, ok := limits.Lookup(limits.Key(key))
	if !ok {
		h.badLimit(w, r, "admin_limits.error_key", "Key", key)
	}
	return def, ok
}

// PostPlanLimit sets the default plan's value for a key, or clears it when the value is empty.
func (h *AdminHandler) PostPlanLimit(w http.ResponseWriter, r *http.Request) {
	p, _, ok := h.RequireStaff(w, r)
	if !ok {
		return
	}
	def, ok := h.limitDef(w, r)
	if !ok {
		return
	}
	raw := strings.TrimSpace(r.PostFormValue("value"))
	detail := map[string]string{"plan": limits.DefaultPlan, "key": string(def.Key), "value": "default"}
	write := func(ctx context.Context) error { return h.Limits.ClearPlan(ctx, limits.DefaultPlan, def.Key) }
	if raw != "" {
		v, ok := parseLimit(raw)
		if !ok {
			h.badLimit(w, r, "admin_limits.error_value")
			return
		}
		detail["value"] = strconv.FormatInt(v, 10)
		write = func(ctx context.Context) error {
			return h.Limits.SetPlan(ctx, limits.DefaultPlan, def.Key, v, p.UserID)
		}
	}
	h.auditedLimit(w, r, p, audit.Event{Action: "limit.plan_set", TargetKind: "plan", TargetID: limits.DefaultPlan, Detail: detail}, write)
}

// PostOverride gives one org, user or project its own value for a key.
func (h *AdminHandler) PostOverride(w http.ResponseWriter, r *http.Request) {
	p, _, ok := h.RequireStaff(w, r)
	if !ok {
		return
	}
	def, ok := h.limitDef(w, r)
	if !ok {
		return
	}
	raw := strings.TrimSpace(r.PostFormValue("subject"))
	sub, name, ok := h.resolveSubject(r.Context(), def.Subject, raw)
	if !ok {
		h.badLimit(w, r, "admin_limits.error_subject", "Kind", string(def.Subject), "Subject", raw)
		return
	}
	v, ok := parseLimit(r.PostFormValue("value"))
	if !ok {
		h.badLimit(w, r, "admin_limits.error_value")
		return
	}
	e := audit.Event{Action: "limit.override_set", TargetKind: string(sub.Kind), TargetID: sub.ID.String(),
		Detail: map[string]string{"name": name, "subject_kind": string(sub.Kind), "subject_id": sub.ID.String(), "key": string(def.Key), "value": strconv.FormatInt(v, 10)}}
	h.auditedLimit(w, r, p, e, func(ctx context.Context) error { return h.Limits.SetOverride(ctx, sub, def.Key, v, p.UserID) })
}

// NOTE: a project override names the project by id and is not checked against the projects table; look it up if staff mistype ids.
func (h *AdminHandler) resolveSubject(ctx context.Context, kind limits.SubjectKind, raw string) (limits.Subject, string, bool) {
	switch kind {
	case limits.SubjectOrg:
		o, err := h.Orgs.BySlug(ctx, raw)
		if err != nil {
			return limits.Subject{}, "", false
		}
		return limits.Subject{Kind: kind, ID: o.ID}, o.Name, true
	case limits.SubjectUser:
		u, err := h.Directory.ByEmail(ctx, strings.ToLower(raw))
		if err != nil {
			return limits.Subject{}, "", false
		}
		return limits.Subject{Kind: kind, ID: u.ID}, u.Email, true
	case limits.SubjectProject:
		id, err := uuid.Parse(raw)
		if err != nil {
			return limits.Subject{}, "", false
		}
		return limits.Subject{Kind: kind, ID: id}, id.String(), true
	}
	return limits.Subject{}, "", false
}

// PostDeleteOverride removes one override, reverting the subject to its plan.
func (h *AdminHandler) PostDeleteOverride(w http.ResponseWriter, r *http.Request) {
	p, _, ok := h.RequireStaff(w, r)
	if !ok {
		return
	}
	def, ok := h.limitDef(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(r.PostFormValue("id"))
	if err != nil || limits.SubjectKind(r.PostFormValue("kind")) != def.Subject {
		h.badLimit(w, r, "admin_limits.error_subject", "Kind", string(def.Subject), "Subject", r.PostFormValue("id"))
		return
	}
	sub := limits.Subject{Kind: def.Subject, ID: id}
	e := audit.Event{Action: "limit.override_deleted", TargetKind: string(sub.Kind), TargetID: sub.ID.String(),
		Detail: map[string]string{"subject_kind": string(sub.Kind), "subject_id": sub.ID.String(), "key": string(def.Key)}}
	h.auditedLimit(w, r, p, e, func(ctx context.Context) error { return h.Limits.DeleteOverride(ctx, sub, def.Key) })
}

func (h *AdminHandler) auditedLimit(w http.ResponseWriter, r *http.Request, p session.Principal, e audit.Event, write func(ctx context.Context) error) {
	sys, err := h.Orgs.SystemOrg(r.Context())
	if err != nil {
		h.failed(w, r, "system org", err)
		return
	}
	err = h.inOrg(r.Context(), session.Principal{UserID: p.UserID, Email: p.Email, Source: p.Source}, sys.ID, func(ctx context.Context) error {
		if err := write(ctx); err != nil {
			return err
		}
		return h.Audit.Record(ctx, e)
	})
	if err != nil {
		h.failed(w, r, "change limit", err)
		return
	}
	http.Redirect(w, r, web.Path(h.Cfg.HTTP.BasePath, "/admin/limits"), http.StatusSeeOther)
}

func (d Deps) applySupport(ctx context.Context, base *web.LayoutData, p session.Principal, pinned uuid.UUID) {
	s := p.Support
	if s == nil || !s.Live(s.OrgID, time.Now()) || d.Orgs == nil {
		return
	}
	o, err := d.Orgs.ByID(ctx, s.OrgID)
	if err != nil {
		d.LogErr("layout: support org", err)
		return
	}
	base.Support = &web.SupportBanner{OrgName: o.Name, OrgSlug: o.Slug, Reason: s.Reason, Until: s.Until}
	if pinned == o.ID {
		base.ActiveOrg = &web.ActiveOrg{ID: o.ID.String(), Slug: o.Slug, Name: o.Name}
	}
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
