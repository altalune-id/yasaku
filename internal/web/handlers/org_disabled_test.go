package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"altalune.id/yasaku/internal/i18n"
	"altalune.id/yasaku/internal/platform/capabilities"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/web/handlers"
)

func TestOrgHandler_CreationDisabledPageFollowsTheLocale(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.Deps.Caps = capabilities.Capabilities{}
	mux := http.NewServeMux()
	handlers.NewOrgHandler(f.Deps, f.Orgs).Register(mux)
	tr := i18n.NewEmbeddedBundle(i18n.EnUS).For(i18n.IdID)

	for _, method := range []string{http.MethodGet, http.MethodPost} {
		target := "/orgs/new"
		if method == http.MethodPost {
			target = "/orgs"
		}
		r := f.authedRequest(t, method, target, "", session.Principal{UserID: uuid.New()})
		r = r.WithContext(i18n.TranslatorInto(r.Context(), tr))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, r)

		assert.Equal(t, http.StatusForbidden, rec.Code, method)
		assert.Contains(t, rec.Body.String(), tr.T("org_new.disabled_title"), method)
		assert.Contains(t, rec.Body.String(), tr.T("org_new.disabled"), method)
		assert.NotContains(t, rec.Body.String(), "Organization creation is disabled", method)
	}
}
