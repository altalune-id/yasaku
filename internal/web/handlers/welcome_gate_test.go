package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/web/handlers"
)

func TestWelcomeGate_LetsLanguageAndThemeThroughBeforeAcceptance(t *testing.T) {
	t.Parallel()
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	gate := handlers.WelcomeGate("", true)(next)
	p := session.Principal{UserID: uuid.New()}
	for _, path := range []string{"/locale", "/theme"} {
		r := httptest.NewRequest(http.MethodPost, path, nil)
		r = r.WithContext(session.PrincipalInto(r.Context(), p))
		rec := httptest.NewRecorder()
		gate.ServeHTTP(rec, r)
		assert.Equal(t, http.StatusTeapot, rec.Code, "%s must reach its handler so the welcome page can switch language and theme", path)
	}
	r := httptest.NewRequest(http.MethodGet, "/orgs", nil)
	r = r.WithContext(session.PrincipalInto(r.Context(), p))
	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, r)
	assert.Equal(t, http.StatusSeeOther, rec.Code, "other pages still wait for acceptance")
}
