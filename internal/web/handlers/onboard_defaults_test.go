package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/i18n"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/web/handlers"
)

func TestOnboardHandler_DefaultsAndPlaceholdersFollowTheLocale(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.Cfg.Tenant.SingletonOrg.Name = config.DefaultSingletonOrgName
	req := &atomicBoolWrapper{}
	req.b.Store(true)
	mux := http.NewServeMux()
	handlers.NewOnboardHandler(f.Deps, f.Users, f.Orgs, f.Projects, f.Onboards, &req.b, nil, "").Register(mux)
	tr := i18n.NewEmbeddedBundle(i18n.EnUS).For(i18n.IdID)

	r := httptest.NewRequest(http.MethodGet, "/onboard", nil)
	r = r.WithContext(i18n.TranslatorInto(r.Context(), tr))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, r)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	for _, key := range []string{"onboard.default_org_name", "onboard.default_project_name", "welcome.display_name_placeholder", "org_new.name_placeholder"} {
		assert.Contains(t, body, tr.T(key), key)
	}
	for _, english := range []string{"Default Organization", "Default Project", `placeholder="Your name"`} {
		assert.NotContains(t, body, english)
	}
}
