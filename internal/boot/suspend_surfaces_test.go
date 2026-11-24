package boot_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/gen/go/yasaku/v1/yasakuv1connect"
	"altalune.id/yasaku/gen/go/yasaku/v1/yasakuv1mcp"
	"altalune.id/yasaku/internal/apikey"
	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/auth"
	"altalune.id/yasaku/internal/platform/authn"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/testutil/fakes"
	"altalune.id/yasaku/internal/user"
)

func staffCtx(t *testing.T, c *canSurfaces) context.Context {
	t.Helper()
	staff, err := c.srv.Users.Create(t.Context(), user.CreateRequest{Email: "staff-" + uuid.NewString()[:8] + "@example.com", Name: "Staff", Source: user.SourceOIDC})
	require.NoError(t, err)
	_, err = c.srv.Orgs.BootstrapSingleton(t.Context(), "ops", "Ops", staff.ID)
	require.NoError(t, err)
	return session.PrincipalInto(t.Context(), session.Principal{UserID: staff.ID, Source: session.SourceOIDC})
}

func consoleGET(t *testing.T, c *canSurfaces, userID uuid.UUID, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.AddCookie(probeCookie(t, c.srv, session.Principal{UserID: userID, ActiveOrgID: c.org.ID, Source: session.SourceOIDC}))
	rec := httptest.NewRecorder()
	c.srv.Web.ServeHTTP(rec, req)
	return rec
}

// TestSuspend_RefusedOnEverySurface suspends a member and drives sign-in, console, procedure, MCP and a personal key. SECURITY: one check per surface, so none lets a suspended user act.
func TestSuspend_RefusedOnEverySurface(t *testing.T) {
	c := newCanSurfaces(t)
	_, personal, err := c.srv.APIKeys.MintPersonal(fakes.Owner(t.Context(), tenant.Context{OrgID: c.org.ID, UserID: c.member.ID}), "mine",
		[]string{authn.ScopeYasakuRead}, apikey.ProjectGrant{All: true}, new(time.Now().Add(time.Hour)))
	require.NoError(t, err)
	list := yasakuv1connect.WalletServiceListWalletsProcedure
	require.Equal(t, http.StatusOK, c.rpc(t, personal, list, map[string]any{}).Code, "the key works before the suspension")
	require.Equal(t, http.StatusOK, consoleGET(t, c, c.member.ID, "/orgs/mcp-org").Code)

	require.NoError(t, c.srv.Users.Suspend(staffCtx(t, c), c.member.ID))

	_, err = c.srv.Auth.LoginOIDC(t.Context(), auth.OIDCClaims{Issuer: c.issuer.url, Subject: canMemberSub, Email: "can-member@example.com", EmailVerified: true})
	assert.True(t, user.IsSuspendedError(err), "sign-in: got %T: %v", err, err)

	rec := consoleGET(t, c, c.member.ID, "/orgs/mcp-org")
	assert.Equal(t, http.StatusForbidden, rec.Code, "console")
	assert.Contains(t, rec.Body.String(), "data-suspended", "console")

	proc := c.rpc(t, c.memberToken(controlAudience), list, map[string]any{})
	assert.Equal(t, http.StatusForbidden, proc.Code, "procedure")
	assert.Contains(t, proc.Body.String(), apperror.CodeUserSuspended, "procedure names the suspension")
	requireBareChallenge(t, proc.Header().Get("WWW-Authenticate"), "procedure")
	mcp := c.tool(t, c.memberToken(mcpAudience), yasakuv1mcp.ListWalletsToolName, map[string]any{})
	assert.Equal(t, http.StatusForbidden, mcp.Code, "MCP: %s", mcp.Body.String())
	assert.Contains(t, mcp.Body.String(), apperror.CodeUserSuspended, "MCP names the suspension")
	requireBareChallenge(t, mcp.Header().Get("WWW-Authenticate"), "MCP")
	assert.Equal(t, http.StatusUnauthorized, c.rpc(t, personal, list, map[string]any{}).Code, "personal key")

	require.NoError(t, c.srv.Users.Unsuspend(staffCtx(t, c), c.member.ID))
	assert.Equal(t, http.StatusOK, c.rpc(t, c.memberToken(controlAudience), list, map[string]any{}).Code, "unsuspend restores access")
}

// TestSuspend_RefusedOnSessionWrites drives the console writes that load the session without an org scope. SECURITY: the check lives in LoadSession, so none of them acts for a suspended user.
func TestSuspend_RefusedOnSessionWrites(t *testing.T) {
	c := newCanSurfaces(t)
	lone, err := c.srv.Users.Create(t.Context(), user.CreateRequest{Email: "lone@example.com", Name: "Lone", Source: user.SourceOIDC})
	require.NoError(t, err)
	require.NoError(t, c.srv.Users.Suspend(staffCtx(t, c), lone.ID))
	p := session.Principal{UserID: lone.ID, Email: lone.Email, Name: lone.Name, Source: session.SourceOIDC}

	rec := consolePOST(t, c, p, "/orgs", url.Values{"slug": {"lone-org"}, "name": {"Lone Org"}})
	assert.Less(t, rec.Code, 500, "POST /orgs: %s", rec.Body.String())
	_, err = c.srv.Orgs.BySlug(t.Context(), "lone-org")
	assert.Error(t, err, "POST /orgs created no org")

	consolePOST(t, c, p, "/signup/complete", url.Values{"name": {"Signup Org"}, "accept_terms": {"1"}})
	orgs, err := c.srv.Orgs.List(t.Context(), lone.ID)
	require.NoError(t, err)
	assert.Empty(t, orgs, "signup created no org")

	consolePOST(t, c, p, "/onboarding", url.Values{"display_name": {"Renamed"}, "accept_terms": {"1"}})
	got, err := c.srv.Users.ByID(t.Context(), lone.ID)
	require.NoError(t, err)
	assert.Equal(t, "Lone", got.Name, "onboarding renamed no one")
	assert.Nil(t, got.TermsAcceptedAt, "onboarding accepted no terms")

	req := httptest.NewRequest(http.MethodGet, "/invites/accept?token=any", nil)
	req.AddCookie(probeCookie(t, c.srv, p))
	rec = httptest.NewRecorder()
	c.srv.Web.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusSeeOther, rec.Code, "invite accept: %s", rec.Body.String())
	assert.Contains(t, rec.Header().Get("Location"), "/login", "invite accept treats a suspended user as signed out")
}

func consolePOST(t *testing.T, c *canSurfaces, p session.Principal, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(probeCookie(t, c.srv, p))
	rec := httptest.NewRecorder()
	c.srv.Web.ServeHTTP(rec, req)
	return rec
}

func TestSuspendedSoleOwner_OrgStillWorksForMembers(t *testing.T) {
	c := newCanSurfaces(t)
	_, orgKey, err := c.srv.APIKeys.MintOrg(fakes.Owner(t.Context(), tenant.Context{OrgID: c.org.ID, UserID: c.owner}), "ci",
		[]string{authn.ScopeYasakuRead}, apikey.ProjectGrant{All: true}, new(time.Now().Add(time.Hour)))
	require.NoError(t, err)

	require.NoError(t, c.srv.Users.Suspend(staffCtx(t, c), c.owner))

	list := yasakuv1connect.WalletServiceListWalletsProcedure
	assert.Equal(t, http.StatusUnauthorized, c.rpc(t, orgKey, list, map[string]any{}).Code, "a key acts for its creator, so it stops with them")
	assert.Equal(t, http.StatusOK, c.rpc(t, c.memberToken(controlAudience), list, map[string]any{}).Code, "members keep working")
	assert.Equal(t, http.StatusOK, consoleGET(t, c, c.member.ID, "/orgs/mcp-org").Code, "members keep the console")
}

// requireBareChallenge pins RFC 6750 Section 3: a refused request names the resource metadata, and no error value, since the token itself is valid.
func requireBareChallenge(t *testing.T, header, surface string) {
	t.Helper()
	assert.True(t, strings.HasPrefix(header, "Bearer "), "%s: WWW-Authenticate=%q", surface, header)
	assert.Contains(t, header, "resource_metadata=", surface)
	assert.NotContains(t, header, "error=", surface)
}
