package handlers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/web"
	"altalune.id/yasaku/internal/web/handlers"
)

func postTip(t *testing.T, f *handlerFixture, r *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	handlers.NewTipsHandler(f.Deps, f.Users).Register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, r)
	return rec
}

func flowTip(t *testing.T) web.Tip {
	t.Helper()
	for _, tip := range web.Tips() {
		if len(tip.FlowIDs()) > 1 {
			return tip
		}
	}
	t.Fatal("no flow with more than one tip")
	return web.Tip{}
}

func TestTips_SeenStoresForThePrincipalOnly(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	me := seedUser(t, f, "me@example.com")
	other := seedUser(t, f, "other@example.com")
	tip := flowTip(t)
	r := f.authedRequest(t, http.MethodPost, "/tips/"+tip.ID+"/seen", "redirect=/orgs?x=1", session.Principal{UserID: me.ID, Email: me.Email})

	rec := postTip(t, f, r)
	require.Equal(t, http.StatusSeeOther, rec.Code, rec.Body.String())
	assert.Equal(t, "/orgs?x=1", rec.Header().Get("Location"))

	got, err := f.UserStore.ByID(context.Background(), me.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{tip.ID}, got.TipsSeen)
	untouched, err := f.UserStore.ByID(context.Background(), other.ID)
	require.NoError(t, err)
	assert.Empty(t, untouched.TipsSeen)

	sp, _, ok := f.Deps.LoadSession(r)
	require.True(t, ok)
	assert.Equal(t, []string{tip.ID}, sp.TipsSeen, "the session sees it at once, so the next page skips the tip")
}

func TestTips_SkipAllMarksTheWholeFlow(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	me := seedUser(t, f, "me@example.com")
	tip := flowTip(t)
	r := f.authedRequest(t, http.MethodPost, "/tips/"+tip.ID+"/seen", "all=1&redirect=/", session.Principal{UserID: me.ID, Email: me.Email})

	require.Equal(t, http.StatusSeeOther, postTip(t, f, r).Code)
	got, err := f.UserStore.ByID(context.Background(), me.ID)
	require.NoError(t, err)
	assert.ElementsMatch(t, tip.FlowIDs(), got.TipsSeen)
}

func TestTips_UnknownIDIsRefused(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	me := seedUser(t, f, "me@example.com")
	for _, id := range []string{"nope", "x%22y", strings.ToUpper(flowTip(t).ID)} {
		r := f.authedRequest(t, http.MethodPost, "/tips/"+id+"/seen", "all=1&redirect=/", session.Principal{UserID: me.ID, Email: me.Email})
		assert.Equal(t, http.StatusNotFound, postTip(t, f, r).Code, id)
	}
	got, err := f.UserStore.ByID(context.Background(), me.ID)
	require.NoError(t, err)
	assert.Empty(t, got.TipsSeen)
}

func TestTips_ExpiredSessionGoesToSignIn(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	r := httptest.NewRequest(http.MethodPost, "/tips/"+flowTip(t).ID+"/seen", strings.NewReader("redirect=/"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := postTip(t, f, r)
	assert.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, "/login", rec.Header().Get("Location"))
}

func TestTips_ExpiredSessionHTMXRedirectsTheWholePage(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	r := httptest.NewRequest(http.MethodPost, "/tips/"+flowTip(t).ID+"/seen", strings.NewReader("redirect=/"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("HX-Request", "true")
	rec := postTip(t, f, r)
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, "/login", rec.Header().Get(web.HeaderRedirect))
}

func TestTips_DeletedUserWithLiveSessionIsUnauthorized(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	r := f.authedRequest(t, http.MethodPost, "/tips/"+flowTip(t).ID+"/seen", "redirect=/", session.Principal{UserID: uuid.New(), Email: "gone@example.com"})
	assert.Equal(t, http.StatusUnauthorized, postTip(t, f, r).Code)
}

func TestTips_MissingUserServiceIsAServerError(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	me := seedUser(t, f, "me@example.com")
	r := f.authedRequest(t, http.MethodPost, "/tips/"+flowTip(t).ID+"/seen", "redirect=/", session.Principal{UserID: me.ID, Email: me.Email})
	mux := http.NewServeMux()
	handlers.NewTipsHandler(f.Deps, nil).Register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, r)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestTips_HTMXGetsNoContent(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	me := seedUser(t, f, "me@example.com")
	r := f.authedRequest(t, http.MethodPost, "/tips/"+flowTip(t).ID+"/seen", "redirect=/orgs", session.Principal{UserID: me.ID, Email: me.Email})
	r.Header.Set("HX-Request", "true")
	rec := postTip(t, f, r)
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Empty(t, rec.Header().Get("Location"))
	assert.Empty(t, rec.Header().Get("HX-Redirect"))
}

func TestTips_RejectsOffsiteRedirect(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	me := seedUser(t, f, "me@example.com")
	for _, target := range []string{"//evil.example", "https://evil.example/", "/\\evil.example", "javascript:alert(1)"} {
		r := f.authedRequest(t, http.MethodPost, "/tips/"+flowTip(t).ID+"/seen", "redirect="+target, session.Principal{UserID: me.ID, Email: me.Email})
		assert.Equal(t, "/", postTip(t, f, r).Header().Get("Location"), target)
	}
}

func TestTips_RouteIsPostOnly(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	rec := postTip(t, f, httptest.NewRequest(http.MethodGet, "/tips/"+flowTip(t).ID+"/seen", nil))
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}
