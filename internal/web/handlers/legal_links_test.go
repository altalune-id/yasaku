package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/web/handlers"
)

func TestLogin_LegalLinks(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, terms, privacy, wantTerms, wantPrivacy string
	}{
		{name: "default paths", wantTerms: `href="/terms"`, wantPrivacy: `href="/privacy"`},
		{name: "configured urls", terms: " https://example.com/tos ", privacy: "https://example.com/privacy",
			wantTerms: `href="https://example.com/tos"`, wantPrivacy: `href="https://example.com/privacy"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			f.Cfg.Compliance.TermsURL = tc.terms
			f.Cfg.Compliance.PrivacyURL = tc.privacy
			h := handlers.NewAuthHandler(f.Deps, newAuthSvc(t, f), f.Users, f.Orgs, f.Projects, nil, nil)
			mux := http.NewServeMux()
			h.Register(mux)
			for _, path := range []string{"/login", "/admin-login"} {
				rec := httptest.NewRecorder()
				mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
				require.Equal(t, http.StatusOK, rec.Code, path)
				body := rec.Body.String()
				assert.Contains(t, body, tc.wantTerms, path)
				assert.Contains(t, body, tc.wantPrivacy, path)
				assert.Contains(t, body, "data-legal-links", path)
			}
		})
	}
}
