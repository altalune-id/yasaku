package handlers

import (
	"cmp"
	"strings"

	"altalune.id/yasaku/internal/web"
)

func (d Deps) legalURLs() (terms, privacy string) {
	return cmp.Or(strings.TrimSpace(d.Cfg.Compliance.TermsURL), web.Path(d.Cfg.HTTP.BasePath, "/terms")),
		cmp.Or(strings.TrimSpace(d.Cfg.Compliance.PrivacyURL), web.Path(d.Cfg.HTTP.BasePath, "/privacy"))
}
