package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/i18n"
	"altalune.id/yasaku/internal/web/handlers"
)

func TestRouteStubs_RenderThroughTheLayout(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	mux := http.NewServeMux()
	handlers.NewMoreHandler(f.Deps, f.Projects).Register(mux)
	tr := f.Bundle.For(i18n.IdID)

	for sub, key := range map[string]string{"/more": "nav.more"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, f.request(t, http.MethodGet, f.path(sub), nil, false))
		require.Equal(t, http.StatusOK, rec.Code, sub)
		assert.Contains(t, rec.Body.String(), "<title>"+tr.T(key), sub)
		assert.Contains(t, rec.Body.String(), "<h1", sub)
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, f.request(t, http.MethodGet, "/orgs/"+f.OrgSlug+"/projects/nope/more", nil, false))
	assert.Equal(t, http.StatusNotFound, rec.Code, "RequireProject guards the stub")
}
