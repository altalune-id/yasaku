package handlers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/i18n"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/user"
	"altalune.id/yasaku/internal/web"
)

func TestSuspendedMidSession_RendersPage(t *testing.T) {
	f := newFixture(t)
	f.Deps.Users = f.Users
	f.Cfg.Compliance.ContactEmail = "help@example.com"
	ctx := context.Background()
	u, err := user.New("mid@example.com", "Mid", user.SourceOIDC)
	require.NoError(t, err)
	require.NoError(t, f.UserStore.Save(ctx, u))
	_, err = f.Orgs.Create(ctx, org.CreateRequest{Slug: "acme", Name: "Acme", OwnerID: u.ID})
	require.NoError(t, err)
	p := session.Principal{UserID: u.ID, Source: session.SourceOIDC}

	active := f.authedRequest(t, http.MethodGet, "/orgs/acme", "", p)
	active.SetPathValue("org", "acme")
	_, ok := f.Deps.RequireOrg(httptest.NewRecorder(), active)
	require.True(t, ok, "an active user passes")

	require.NoError(t, f.UserStore.SetSuspended(ctx, u.ID, new(time.Now())))
	r := f.authedRequest(t, http.MethodGet, "/orgs/acme", "", p)
	r = r.WithContext(i18n.TranslatorInto(r.Context(), i18n.NewEmbeddedBundle(i18n.EnUS).For(i18n.EnUS)))
	r.SetPathValue("org", "acme")
	w := httptest.NewRecorder()
	_, ok = f.Deps.RequireOrg(w, r)
	assert.False(t, ok)
	assert.Equal(t, http.StatusForbidden, w.Code, "a suspended user sees the suspended page, not a login loop")
	assert.Contains(t, w.Body.String(), "This account is suspended. Contact help@example.com.")
	assert.Contains(t, w.Body.String(), "data-suspended", "the suspended page, with its sign-out")
	_, _, ok = f.Deps.LoadSession(r)
	assert.False(t, ok, "a suspended user loads as signed out")
	c, err := r.Cookie(web.SessionCookieName)
	require.NoError(t, err)
	sid, err := web.VerifyCookie(f.Deps.Key(web.PurposeSession), c.Value)
	require.NoError(t, err)
	_, ok, err = f.Deps.Sessions.Load(ctx, sid)
	require.NoError(t, err)
	assert.True(t, ok, "the session stays, so an unsuspend restores it")

	f.Deps.ClearSession(httptest.NewRecorder(), r)
	_, ok, err = f.Deps.Sessions.Load(ctx, sid)
	require.NoError(t, err)
	assert.False(t, ok, "sign-out still deletes a suspended user's session")

	unknown := session.Principal{UserID: uuid.New(), Source: session.SourceOIDC}
	w = httptest.NewRecorder()
	_, _, ok = f.Deps.RequireSignedIn(w, f.authedRequest(t, http.MethodGet, "/", "", unknown))
	assert.False(t, ok)
	assert.Equal(t, http.StatusSeeOther, w.Code, "a session for a deleted user goes back to login")
}
