package handlers

import (
	"net/http"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/i18n"
	"altalune.id/yasaku/internal/user"
	"altalune.id/yasaku/internal/web"
)

// TipsHandler serves POST /tips/{id}/seen, recording a dismissed tip on the signed-in user's account.
type TipsHandler struct {
	Deps
	Users *user.Service
}

// NewTipsHandler wires the handler.
func NewTipsHandler(d Deps, users *user.Service) *TipsHandler {
	return &TipsHandler{Deps: d, Users: users}
}

// Register mounts POST /tips/{id}/seen and POST /tips/reset on mux.
func (h *TipsHandler) Register(mux web.Mux) {
	mux.HandleFunc("POST /tips/{id}/seen", h.postSeen)
	mux.HandleFunc("POST /tips/reset", h.postReset)
}

// SECURITY: writes only the session principal's own row, and only registered tip ids.
func (h *TipsHandler) postSeen(w http.ResponseWriter, r *http.Request) {
	tip, ok := web.LookupTip(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if h.Users == nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	p, sid, ok := h.LoadSession(r)
	if !ok || p.UserID == uuid.Nil {
		h.toSignIn(w, r)
		return
	}
	ids := []string{tip.ID}
	if r.PostFormValue("all") != "" {
		ids = tip.FlowIDs()
	}
	seen, err := h.Users.MarkTipsSeen(r.Context(), p.UserID, ids)
	if user.IsNotFoundError(err) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if err != nil {
		h.LogErr("tips: mark seen", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	p.TipsSeen = seen
	if err := h.UpdateSession(r, sid, p); err != nil {
		h.LogErr("tips: update session", err)
	}
	h.done(w, r)
}

// SECURITY: clears only the session principal's own row.
func (h *TipsHandler) postReset(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if h.Users == nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	p, sid, ok := h.LoadSession(r)
	if !ok || p.UserID == uuid.Nil {
		h.toSignIn(w, r)
		return
	}
	err := h.Users.ResetTipsSeen(r.Context(), p.UserID)
	if user.IsNotFoundError(err) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if err != nil {
		h.LogErr("tips: reset", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	p.TipsSeen = nil
	if err := h.UpdateSession(r, sid, p); err != nil {
		h.LogErr("tips: update session", err)
	}
	h.done(w, r)
}

func (h *TipsHandler) done(w http.ResponseWriter, r *http.Request) {
	if web.IsHTMXRequest(r) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	target := i18n.SanitizeRedirect(r.PostFormValue("redirect"))
	if target == "" {
		target = h.Cfg.HTTP.BasePath + "/"
	}
	http.Redirect(w, r, target, http.StatusSeeOther) //nolint:gosec // G710: SanitizeRedirect rejects any target that is not a same-origin absolute path
}

func (h *TipsHandler) toSignIn(w http.ResponseWriter, r *http.Request) {
	login := ResolveReturnTo(h.Cfg.HTTP.BasePath, "/login")
	if web.IsHTMXRequest(r) {
		w.Header().Set(web.HeaderRedirect, login)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, login, http.StatusSeeOther)
}
