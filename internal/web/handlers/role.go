package handlers

import (
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/i18n"
	"altalune.id/yasaku/internal/platform/authz"
	"altalune.id/yasaku/internal/role"
	"altalune.id/yasaku/internal/web"
	"altalune.id/yasaku/internal/web/templates"
)

//i18n:use roles.error_not_manager
const rolesNotManager = "Only an owner or admin can manage roles."

// RoleHandler owns the /orgs/{org}/roles routes.
type RoleHandler struct{ Deps }

// NewRoleHandler wires the handler.
func NewRoleHandler(d Deps, roles *role.Service) *RoleHandler {
	d.Roles = roles
	return &RoleHandler{Deps: d}
}

// GetList renders /orgs/{org}/roles.
func (h *RoleHandler) GetList(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.RequireOrg(w, r)
	if !ok {
		return
	}
	h.renderList(w, sc, http.StatusOK, "", "")
}

func (h *RoleHandler) renderList(w http.ResponseWriter, sc OrgScope, status int, errMsg, errCode string) {
	o, r := sc.org, sc.req
	if !holds(r, authz.MembersWrite) {
		h.forbidNonManager(w, r, "roles.error_not_manager", rolesNotManager)
		return
	}
	items, err := h.Roles.List(r.Context())
	if err != nil {
		h.LogErr("web role: list", err)
		h.ErrorPage(w, r, http.StatusInternalServerError, pageTitle(r, "error_title.list_failed"), "Could not load roles.", err)
		return
	}
	held, err := h.Roles.HeldBy(r.Context())
	if err != nil {
		h.LogErr("web role: held by", err)
		h.ErrorPage(w, r, http.StatusInternalServerError, pageTitle(r, "error_title.list_failed"), "Could not load roles.", err)
		return
	}
	allowed, err := h.Roles.Allowed(r.Context())
	if err != nil {
		h.LogErr("web role: allowed", err)
		h.ErrorPage(w, r, http.StatusInternalServerError, pageTitle(r, "error_title.list_failed"), "Could not load roles.", err)
		return
	}
	v := templates.RolesView{OrgSlug: o.Slug, CanEdit: allowed && holds(r, authz.RolesWrite), StaffOrg: !allowed, Error: errMsg, ErrorCode: errCode}
	for _, name := range []string{"owner", "admin", "member"} {
		v.Builtins = append(v.Builtins, templates.BuiltinRoleRow{Name: name, Perms: len(authz.Builtin(name))})
	}
	for _, it := range items {
		v.Roles = append(v.Roles, templates.RoleRow{
			ID: it.ID.String(), Name: it.Name, Description: it.Description, Perms: len(it.Permissions), Held: held[it.ID] > 0,
		})
	}
	if v.CanEdit {
		v.CreateBlocked, _ = h.limitBlocked(r, h.Roles.CreateAllowed(r.Context()), "web role: create allowed")
	}
	RenderStatus(w, r, status, templates.RolesLayout(h.LayoutForOrg(r, pageTitle(r, "roles.title"), o.Slug, "roles"), v))
}

// GetNew renders /orgs/{org}/roles/new.
func (h *RoleHandler) GetNew(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.requireRolesWrite(w, r)
	if !ok {
		return
	}
	r = sc.req
	v := templates.RoleEditView{OrgSlug: sc.org.Slug}
	h.applyCopy(r, &v, nil)
	v.Error, v.ErrorCode = h.limitBlocked(r, h.Roles.CreateAllowed(r.Context()), "web role: create allowed")
	v.Blocked = v.Error != ""
	h.renderEdit(w, sc, http.StatusOK, v)
}

// GetEdit renders /orgs/{org}/roles/{id}.
func (h *RoleHandler) GetEdit(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.requireRolesWrite(w, r)
	if !ok {
		return
	}
	r = sc.req
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		h.ErrorPage(w, r, http.StatusBadRequest, pageTitle(r, "error_title.bad_id"), "Malformed role id.")
		return
	}
	it, err := h.Roles.Get(r.Context(), id)
	if err != nil {
		h.refuseRole(w, sc, templates.RoleEditView{OrgSlug: sc.org.Slug}, err)
		return
	}
	v := templates.RoleEditView{OrgSlug: sc.org.Slug, ID: it.ID.String(), Name: it.Name, Description: it.Description, Version: it.Version}
	h.applyCopy(r, &v, it.Permissions)
	h.renderEdit(w, sc, http.StatusOK, v)
}

// PostCreate handles POST /orgs/{org}/roles.
func (h *RoleHandler) PostCreate(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.requireRolesWrite(w, r)
	if !ok {
		return
	}
	v, perms, ok := h.parseForm(w, sc)
	if !ok {
		return
	}
	if _, err := h.Roles.Create(sc.req.Context(), v.Name, v.Description, perms); err != nil {
		h.refuseRole(w, sc, v, err)
		return
	}
	h.redirectList(w, sc)
}

// PostUpdate handles POST /orgs/{org}/roles/{id}.
func (h *RoleHandler) PostUpdate(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.requireRolesWrite(w, r)
	if !ok {
		return
	}
	r = sc.req
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		h.ErrorPage(w, r, http.StatusBadRequest, pageTitle(r, "error_title.bad_id"), "Malformed role id.")
		return
	}
	v, perms, ok := h.parseForm(w, sc)
	if !ok {
		return
	}
	v.ID = id.String()
	if _, err := h.Roles.Update(r.Context(), id, v.Name, v.Description, perms, v.Version); err != nil {
		h.refuseRole(w, sc, v, err)
		return
	}
	h.redirectList(w, sc)
}

// PostDelete handles POST /orgs/{org}/roles/{id}/delete.
func (h *RoleHandler) PostDelete(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.requireRolesWrite(w, r)
	if !ok {
		return
	}
	r = sc.req
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		h.ErrorPage(w, r, http.StatusBadRequest, pageTitle(r, "error_title.bad_id"), "Malformed role id.")
		return
	}
	if err := h.Roles.Delete(r.Context(), id); err != nil {
		status, msg, ok := h.roleRefusal(r, err)
		if !ok {
			h.LogErr("web role: delete", err)
			h.ErrorPage(w, r, http.StatusInternalServerError, pageTitle(r, "error_title.save_failed"), "Could not delete that role.")
			return
		}
		h.renderList(w, sc, status, msg, ErrorRef(err))
		return
	}
	h.redirectList(w, sc)
}

func (h *RoleHandler) requireRolesWrite(w http.ResponseWriter, r *http.Request) (OrgScope, bool) {
	sc, ok := h.RequireOrg(w, r)
	if !ok {
		return OrgScope{}, false
	}
	if !holds(sc.req, authz.RolesWrite) {
		h.forbidNonManager(w, sc.req, "roles.error_not_manager", rolesNotManager)
		return OrgScope{}, false
	}
	return sc, true
}

func (h *RoleHandler) parseForm(w http.ResponseWriter, sc OrgScope) (templates.RoleEditView, []authz.Permission, bool) {
	r := sc.req
	if err := r.ParseForm(); err != nil {
		h.ErrorPage(w, r, http.StatusBadRequest, pageTitle(r, "error_title.bad_request"), "Could not parse form body.")
		return templates.RoleEditView{}, nil, false
	}
	version, _ := strconv.ParseInt(r.PostForm.Get("version"), 10, 64)
	perms := make([]authz.Permission, 0, len(r.PostForm["perm"]))
	for _, p := range r.PostForm["perm"] {
		perms = append(perms, authz.Permission(p))
	}
	v := templates.RoleEditView{
		OrgSlug: sc.org.Slug, Version: version,
		Name: strings.TrimSpace(r.PostForm.Get("name")), Description: strings.TrimSpace(r.PostForm.Get("description")),
	}
	v.Perms = permOptions(r, perms)
	return v, perms, true
}

func (h *RoleHandler) applyCopy(r *http.Request, v *templates.RoleEditView, current []authz.Permission) {
	q := r.URL.Query()
	from := q.Get("from")
	if from == "" {
		v.Perms = permOptions(r, current)
		return
	}
	v.Name = strings.TrimSpace(q.Get("name"))
	v.Description = strings.TrimSpace(q.Get("description"))
	if n, err := strconv.ParseInt(q.Get("version"), 10, 64); err == nil {
		v.Version = n
	}
	v.Perms = permOptions(r, authz.Builtin(from))
}

func permOptions(r *http.Request, checked []authz.Permission) []templates.PermOption {
	tr := i18n.TranslatorFrom(r.Context())
	defs := authz.Catalogue()
	out := make([]templates.PermOption, 0, len(defs))
	for _, d := range defs {
		out = append(out, templates.PermOption{Value: string(d.Permission), Label: tr.T(d.LabelKey), Checked: slices.Contains(checked, d.Permission)})
	}
	return out
}

func (h *RoleHandler) renderEdit(w http.ResponseWriter, sc OrgScope, status int, v templates.RoleEditView) {
	title := pageTitle(sc.req, "role_edit.title_new")
	if v.ID != "" {
		title = pageTitle(sc.req, "roles.title")
	}
	RenderStatus(w, sc.req, status, templates.RoleEditLayout(h.LayoutForOrg(sc.req, title, sc.org.Slug, "roles"), v))
}

func (h *RoleHandler) refuseRole(w http.ResponseWriter, sc OrgScope, v templates.RoleEditView, err error) {
	r := sc.req
	switch {
	case authz.IsDeniedError(err):
		h.forbidNonManager(w, r, "roles.error_not_manager", rolesNotManager)
		return
	case role.IsNotFoundError(err):
		h.ErrorPage(w, r, http.StatusNotFound, pageTitle(r, "error_title.not_found"), localizedDefault(r, "roles.error_not_found", "That role no longer exists."), err)
		return
	}
	status, msg, ok := h.roleRefusal(r, err)
	if !ok {
		h.LogErr("web role: save", err)
		h.ErrorPage(w, r, http.StatusInternalServerError, pageTitle(r, "error_title.save_failed"), "Could not save that role.")
		return
	}
	v.Error, v.ErrorCode = msg, ErrorRef(err)
	h.renderEdit(w, sc, status, v)
}

//i18n:use roles.error_system_org
//i18n:use roles.error_in_use
//i18n:use roles.error_not_found
//i18n:use role_edit.error_invalid_name
//i18n:use role_edit.error_already_exists
//i18n:use role_edit.error_invalid_permission
//i18n:use role_edit.error_stale
func (d Deps) roleRefusal(r *http.Request, err error) (status int, msg string, ok bool) { //nolint:nonamedreturns // status and message differ in role
	tr := i18n.TranslatorFrom(r.Context())
	if m, limited := d.limitMessage(r.Context(), err); limited {
		return http.StatusConflict, m, true
	}
	if e, escalated := errors.AsType[*authz.EscalationError](err); escalated {
		return http.StatusForbidden, tr.T("authz.escalation", "Role", e.Role), true
	}
	switch {
	case role.IsInvalidNameError(err):
		return http.StatusUnprocessableEntity, tr.T("role_edit.error_invalid_name"), true
	case role.IsAlreadyExistsError(err):
		return http.StatusConflict, tr.T("role_edit.error_already_exists"), true
	case role.IsInvalidPermissionError(err):
		return http.StatusUnprocessableEntity, tr.T("role_edit.error_invalid_permission"), true
	case role.IsStaleVersionError(err):
		return http.StatusConflict, tr.T("role_edit.error_stale"), true
	case role.IsSystemOrgError(err):
		return http.StatusConflict, tr.T("roles.error_system_org"), true
	case role.IsInUseError(err):
		return http.StatusConflict, tr.T("roles.error_in_use"), true
	case role.IsNotFoundError(err):
		return http.StatusNotFound, tr.T("roles.error_not_found"), true
	}
	return 0, "", false
}

func (d Deps) customRoles(r *http.Request) []*role.Role {
	if d.Roles == nil || !holds(r, authz.MembersWrite) {
		return nil
	}
	items, err := d.Roles.List(r.Context())
	if err != nil {
		d.LogErr("web role: picker", err)
		return nil
	}
	return items
}

func grantableCustomRoles(r *http.Request, items []*role.Role) []templates.RoleOption {
	held := authz.Held(r.Context())
	out := make([]templates.RoleOption, 0, len(items))
	for _, it := range items {
		if authz.Covers(held, it.Permissions...) {
			out = append(out, templates.RoleOption{Value: it.ID.String(), Label: it.Name})
		}
	}
	return out
}

func customRoleName(items []*role.Role, id uuid.UUID) string {
	if id == uuid.Nil {
		return ""
	}
	for _, it := range items {
		if it.ID == id {
			return it.Name
		}
	}
	return ""
}

func roleIDString(id uuid.UUID) string {
	if id == uuid.Nil {
		return ""
	}
	return id.String()
}

func parseRoleValue(v string) (string, uuid.UUID) {
	if id, err := uuid.Parse(v); err == nil {
		return "custom", id
	}
	return v, uuid.Nil
}

func (h *RoleHandler) redirectList(w http.ResponseWriter, sc OrgScope) {
	http.Redirect(w, sc.req, ResolveReturnTo(h.Cfg.HTTP.BasePath, "/orgs/"+sc.org.Slug+"/roles"), http.StatusSeeOther) //nolint:gosec // G710: destination sanitized via ResolveReturnTo → SanitizeReturnTo
}

// Register wires the role routes onto the mux.
func (h *RoleHandler) Register(mux web.Mux) {
	mux.HandleFunc("GET /orgs/{org}/roles", h.GetList)
	mux.HandleFunc("GET /orgs/{org}/roles/new", h.GetNew)
	mux.HandleFunc("POST /orgs/{org}/roles", h.PostCreate)
	mux.HandleFunc("GET /orgs/{org}/roles/{id}", h.GetEdit)
	mux.HandleFunc("POST /orgs/{org}/roles/{id}", h.PostUpdate)
	mux.HandleFunc("POST /orgs/{org}/roles/{id}/delete", h.PostDelete)
}
