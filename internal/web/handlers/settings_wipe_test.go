package handlers_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/ledger"
	"altalune.id/yasaku/internal/limits"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/testutil/fakes"
	"altalune.id/yasaku/internal/web/handlers"
)

type wipeCounter struct{ calls *int }

func (c wipeCounter) DeleteAllInProject(context.Context) (int64, error) {
	*c.calls++
	return 0, nil
}

type wipeNoop struct{ seedErr error }

func (wipeNoop) ProjectName(context.Context) (string, error)   { return "Alpha", nil }
func (wipeNoop) Lock(context.Context, ...limits.Subject) error { return nil }
func (n wipeNoop) SeedDefaults(context.Context) (int, error)   { return 0, n.seedErr }
func (wipeNoop) EnsureCurrent(context.Context) error           { return nil }

func newWipeMux(t *testing.T) (ledgerFixture, *http.ServeMux, *int) {
	t.Helper()
	return newWipeMuxWith(t, wipeNoop{})
}

func newWipeMuxWith(t *testing.T, ports wipeNoop) (ledgerFixture, *http.ServeMux, *int) {
	t.Helper()
	l := newLedgerFixture(t)
	calls := 0
	del := wipeCounter{calls: &calls}
	h := handlers.NewSettingsHandler(l.f.Deps, l.f.Projects, l.ledger, nil)
	h.Wipe = ledger.NewWipeWorkflow(ledger.WipeDeps{
		Projects: ports, Limits: ports, Seeder: ports, Current: ports,
		Transactions: del, Periods: del, Wallets: del, Categories: del,
		Audit: &fakes.Audit{}, UnitOfWork: fakes.UnitOfWork,
	}, discardLogger(), passthroughUnexpected())
	mux := http.NewServeMux()
	h.Register(mux)
	return l, mux, &calls
}

func postWipe(t *testing.T, l ledgerFixture, mux *http.ServeMux, uid [16]byte, confirm string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, l.f.authedRequest(t, http.MethodPost, "/orgs/acme/projects/alpha/wipe", "confirm="+confirm, session.Principal{UserID: uid, ActiveOrgID: l.o.ID}))
	return rec
}

func TestWipe_Route_OwnerWipesAndReturnsToSettings(t *testing.T) {
	t.Parallel()
	l, mux, calls := newWipeMux(t)
	rec := postWipe(t, l, mux, l.owner, "Alpha")
	require.Equal(t, http.StatusSeeOther, rec.Code, rec.Body.String())
	assert.Equal(t, "/orgs/acme/projects/alpha/settings?saved=wiped", rec.Header().Get("Location"))
	assert.Equal(t, 4, *calls)

	page := httptest.NewRecorder()
	mux.ServeHTTP(page, l.f.authedRequest(t, http.MethodGet, "/orgs/acme/projects/alpha/settings?saved=wiped", "", session.Principal{UserID: l.owner, ActiveOrgID: l.o.ID}))
	assert.Contains(t, page.Body.String(), "settings.wiped")
}

func TestWipe_Route_MismatchShowsDAT001(t *testing.T) {
	t.Parallel()
	l, mux, calls := newWipeMux(t)
	rec := postWipe(t, l, mux, l.owner, "alpha")
	require.Equal(t, http.StatusSeeOther, rec.Code, "a wrong confirm returns to settings instead of a 422 page")
	assert.Equal(t, "/orgs/acme/projects/alpha/settings?wipe=mismatch", rec.Header().Get("Location"))
	assert.Zero(t, *calls)

	page := httptest.NewRecorder()
	mux.ServeHTTP(page, l.f.authedRequest(t, http.MethodGet, "/orgs/acme/projects/alpha/settings?wipe=mismatch", "", session.Principal{UserID: l.owner, ActiveOrgID: l.o.ID}))
	require.Equal(t, http.StatusOK, page.Code)
	body := page.Body.String()
	assert.Contains(t, body, "DAT001")
	assert.NotContains(t, body, "data-dirty-start", "nothing was edited, so no unsaved-changes bar")
}

func TestWipe_Route_MemberIsForbidden(t *testing.T) {
	t.Parallel()
	l, mux, calls := newWipeMux(t)
	rec := postWipe(t, l, mux, l.member, "Alpha")
	require.Equal(t, http.StatusForbidden, rec.Code)
	assert.Zero(t, *calls)

	page := httptest.NewRecorder()
	mux.ServeHTTP(page, l.f.authedRequest(t, http.MethodGet, "/orgs/acme/projects/alpha/settings", "", session.Principal{UserID: l.member, ActiveOrgID: l.o.ID}))
	assert.NotContains(t, page.Body.String(), "/orgs/acme/projects/alpha/wipe", "a member is never offered the wipe")
}

func TestWipe_Route_SetupFailureSaysTheDataIsGone(t *testing.T) {
	t.Parallel()
	l, mux, calls := newWipeMuxWith(t, wipeNoop{seedErr: errors.New("seed down")})
	rec := postWipe(t, l, mux, l.owner, "Alpha")
	require.Equal(t, http.StatusSeeOther, rec.Code, rec.Body.String())
	assert.Equal(t, "/orgs/acme/projects/alpha/settings?saved=wiped_partial", rec.Header().Get("Location"))
	assert.Equal(t, 4, *calls)

	page := httptest.NewRecorder()
	mux.ServeHTTP(page, l.f.authedRequest(t, http.MethodGet, "/orgs/acme/projects/alpha/settings?saved=wiped_partial", "", session.Principal{UserID: l.owner, ActiveOrgID: l.o.ID}))
	body := page.Body.String()
	assert.Contains(t, body, "settings.wiped_partial")
	assert.NotContains(t, body, "Could not wipe")
}
