package handlers

import (
	"net/http"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/i18n"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/user"
	"altalune.id/yasaku/internal/web"
)

// ThemeHandler serves POST /theme, storing the theme choice in a cookie and, for a signed-in user, on the account.
type ThemeHandler struct {
	Deps
	Users *user.Service
}

// NewThemeHandler wires the handler.
func NewThemeHandler(d Deps, users *user.Service) *ThemeHandler {
	return &ThemeHandler{Deps: d, Users: users}
}

// Register mounts POST /theme on mux.
func (h *ThemeHandler) Register(mux web.Mux) {
	mux.HandleFunc("POST /theme", h.postTheme)
}

func (h *ThemeHandler) postTheme(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	pref := web.ParseThemePref(r.PostFormValue("family"), r.PostFormValue("mode"))
	web.SetCookie(w, web.CookieOpts{
		Name:         web.ThemeCookieName,
		Value:        pref.CookieValue(),
		BasePath:     h.Cfg.HTTP.BasePath,
		CookieSecure: h.SecureCookie(r),
		MaxAge:       i18n.CookieMaxAge,
	})
	h.persist(r, pref)
	target := i18n.SanitizeRedirect(r.PostFormValue("redirect"))
	if target == "" {
		target = h.Cfg.HTTP.BasePath + "/"
	}
	if web.IsHTMXRequest(r) {
		w.Header().Set(web.HeaderRedirect, target)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, target, http.StatusSeeOther) //nolint:gosec // G710: SanitizeRedirect rejects any target that is not a same-origin absolute path
}

// SECURITY: writes only the session principal's own row; the form never names a user.
func (h *ThemeHandler) persist(r *http.Request, pref web.ThemePref) {
	if h.Users == nil || h.Sessions == nil {
		return
	}
	p, sid, ok := h.LoadSession(r)
	if !ok || p.UserID == uuid.Nil {
		return
	}
	if err := h.Users.UpdateThemePrefs(r.Context(), p.UserID, pref.Family, pref.Mode); err != nil {
		h.LogErr("theme: persist", err)
		return
	}
	p.Theme, p.ColorMode = pref.Family, pref.Mode
	if err := h.UpdateSession(r, sid, p); err != nil {
		h.LogErr("theme: update session", err)
	}
}

func themePref(r *http.Request, p *session.Principal) web.ThemePref {
	if p != nil && p.ColorMode != "" {
		return web.ParseThemePref(p.Theme, p.ColorMode)
	}
	c, err := r.Cookie(web.ThemeCookieName)
	if err != nil {
		return web.ParseThemePref("", "")
	}
	return web.ParseThemeCookie(c.Value)
}
