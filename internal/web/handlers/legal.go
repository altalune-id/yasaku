package handlers

import (
	"net/http"

	"altalune.id/yasaku/internal/legal"
	"altalune.id/yasaku/internal/web"
	"altalune.id/yasaku/internal/web/templates"
)

// LegalHandler serves the embedded Terms and Privacy documents at /terms and /privacy.
type LegalHandler struct{ Deps }

// NewLegalHandler wires the handler.
func NewLegalHandler(d Deps) *LegalHandler { return &LegalHandler{Deps: d} }

// Register mounts the /terms and /privacy routes on mux.
func (h *LegalHandler) Register(mux web.Mux) {
	mux.HandleFunc("GET /terms", h.get(legal.TermsSlug, "title.terms"))
	mux.HandleFunc("GET /privacy", h.get(legal.PrivacySlug, "title.privacy"))
}

func (h *LegalHandler) get(slug, fallbackKey string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		doc, err := legal.BySlug(slug)
		if err != nil {
			h.LogErr("legal: load", err)
			h.ErrorPage(w, r, http.StatusInternalServerError, pageTitle(r, "error_title.load_failed"), "Could not load the document.", err)
			return
		}
		title := doc.Title
		if title == "" {
			title = pageTitle(r, fallbackKey)
		}
		Render(w, r, templates.LegalLayout(h.Base(r, title), templates.LegalView{Doc: doc}))
	}
}
