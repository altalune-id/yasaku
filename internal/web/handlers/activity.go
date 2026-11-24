package handlers

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"altalune.id/yasaku/internal/i18n"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform/audit"
	"altalune.id/yasaku/internal/platform/authz"
	"altalune.id/yasaku/internal/web"
	"altalune.id/yasaku/internal/web/templates"
)

const activityPageSize = 50

//i18n:use activity.error_not_manager
const activityNotManager = "Only an owner or admin can view activity."

type activityLister interface {
	List(ctx context.Context, f audit.Filter) ([]audit.Entry, error)
}

// ActivityHandler owns /orgs/{org}/activity.
type ActivityHandler struct {
	Deps
	Audit activityLister
}

// NewActivityHandler wires the handler.
func NewActivityHandler(d Deps, orgs *org.Service, rec activityLister) *ActivityHandler {
	d.Orgs = orgs
	return &ActivityHandler{Deps: d, Audit: rec}
}

// GetList renders the org's audit entries, newest first.
func (h *ActivityHandler) GetList(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.RequireOrg(w, r)
	if !ok {
		return
	}
	r = sc.req
	if err := authz.Can(r.Context(), authz.MembersWrite); err != nil {
		if h.refuseNotManager(w, r, err, "activity.error_not_manager", activityNotManager) {
			return
		}
		h.LogErr("web activity: manager check", err)
		h.ErrorPage(w, r, http.StatusInternalServerError, pageTitle(r, "error_title.list_failed"), "Could not load activity.", err)
		return
	}
	var before time.Time
	if raw := r.URL.Query().Get("before"); raw != "" {
		t, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			h.ErrorPage(w, r, http.StatusBadRequest, pageTitle(r, "error_title.bad_request"), "Malformed page cursor.")
			return
		}
		before = t
	}
	entries, err := h.Audit.List(r.Context(), audit.Filter{Limit: activityPageSize, Before: before})
	if err != nil {
		h.LogErr("web activity: list", err)
		h.ErrorPage(w, r, http.StatusInternalServerError, pageTitle(r, "error_title.list_failed"), "Could not load activity.", err)
		return
	}
	v := templates.ActivityView{Rows: activityRows(i18n.TranslatorFrom(r.Context()), entries)}
	if len(entries) == activityPageSize {
		last := entries[len(entries)-1].OccurredAt.UTC().Format(time.RFC3339Nano)
		v.NextHref = web.Path(h.Cfg.HTTP.BasePath, "/orgs/"+sc.org.Slug+"/activity") + "?before=" + url.QueryEscape(last)
	}
	Render(w, r, templates.ActivityLayout(h.LayoutForOrg(r, pageTitle(r, "activity.title"), sc.org.Slug, "activity"), v))
}

//i18n:use activity.action.*
//i18n:use activity.target.*

func activityRows(tr *i18n.Translator, entries []audit.Entry) []templates.ActivityRow {
	out := make([]templates.ActivityRow, 0, len(entries))
	for _, e := range entries {
		out = append(out, templates.ActivityRow{
			Actor:      e.ActorEmail,
			ActorKind:  string(e.ActorKind),
			Action:     translatedOr(tr, "activity.action."+e.Action, e.Action),
			Target:     activityTarget(tr, e),
			Reason:     e.Reason,
			OccurredAt: e.OccurredAt,
		})
	}
	return out
}

// SECURITY: never falls back to TargetID, so a raw id never reaches the page.
func activityTarget(tr *i18n.Translator, e audit.Entry) string {
	if k := e.Detail["key"]; k != "" {
		if v := e.Detail["value"]; v != "" {
			return k + ": " + v
		}
		return k
	}
	for _, k := range []string{"email", "name", "to_name"} {
		if v := e.Detail[k]; v != "" {
			return v
		}
	}
	if e.TargetKind == "" {
		return ""
	}
	return translatedOr(tr, "activity.target."+e.TargetKind, e.TargetKind)
}

func translatedOr(tr *i18n.Translator, key, fallback string) string {
	if v := tr.T(key); v != key {
		return v
	}
	return fallback
}

// Register wires the activity route onto the mux.
func (h *ActivityHandler) Register(mux web.Mux) {
	mux.HandleFunc("GET /orgs/{org}/activity", h.GetList)
}
