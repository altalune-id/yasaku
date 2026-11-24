package boot_test

import (
	"encoding/json"
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
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform/authn"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/testutil/fakes"
	"altalune.id/yasaku/internal/user"
)

type canSurfaces struct {
	*mcpFixture
	org         *org.Org
	owner       uuid.UUID
	member      *user.User
	plain       *user.User
	memberToken func(aud string) string
	target      map[string]any
	periodID    string
}

const canMemberSub = "can-member-subject"

func newCanSurfaces(t *testing.T) *canSurfaces {
	t.Helper()
	f := newMCPFixture(t, mcpOpts{enabled: true})
	f.seedOwnWallet(t)
	c := &canSurfaces{mcpFixture: f, target: map[string]any{"org": "mcp-org", "project": "mcp-project"}}
	o, err := f.srv.Orgs.BySlug(t.Context(), "mcp-org")
	require.NoError(t, err)
	c.org = o
	owner, err := f.srv.Users.EnsureFromOIDC(t.Context(), user.Claims{Issuer: f.issuer.url, Subject: tokenSubject, Email: tokenEmail, EmailVerified: true, Name: "MCP Agent"})
	require.NoError(t, err)
	c.owner = owner.ID
	c.member, err = f.srv.Users.EnsureFromOIDC(t.Context(), user.Claims{Issuer: f.issuer.url, Subject: canMemberSub, Email: "can-member@example.com", EmailVerified: true, Name: "Member"})
	require.NoError(t, err)
	c.plain, err = f.srv.Users.Create(t.Context(), user.CreateRequest{Email: "can-plain@example.com", Name: "Plain", Source: user.SourceOIDC})
	require.NoError(t, err)
	for _, u := range []uuid.UUID{c.member.ID, c.plain.ID} {
		_, err = f.srv.Orgs.AddMember(t.Context(), o.ID, u, org.RoleMember)
		require.NoError(t, err)
	}
	c.memberToken = func(aud string) string {
		return f.issuer.idp.AccessToken(map[string]any{"aud": aud, "sub": canMemberSub, "scope": strings.Join(readWrite(), " ")})
	}
	seed := c.rpc(t, f.issuer.mint(t, controlAudience, readWrite()), yasakuv1connect.TransactionServiceRecordExpenseProcedure,
		map[string]any{"wallet": ownWallet, "amount": map[string]any{"amount": "1000"}, "confirm": true})
	require.Equal(t, http.StatusOK, seed.Code, "the owner's entry opens the current period: %s", seed.Body.String())
	cur := c.rpc(t, f.issuer.mint(t, controlAudience, readWrite()), yasakuv1connect.PeriodServiceGetCurrentPeriodProcedure, map[string]any{})
	var period struct {
		Period struct {
			ID string `json:"id"`
		} `json:"period"`
	}
	require.NoError(t, json.Unmarshal(cur.Body.Bytes(), &period), cur.Body.String())
	c.periodID = period.Period.ID
	require.NotEmpty(t, c.periodID, cur.Body.String())
	return c
}

func (c *canSurfaces) rpc(t *testing.T, credential, procedure string, args map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	body := map[string]any{"target": c.target}
	for k, v := range args {
		body[k] = v
	}
	return c.connectRPC(t, credential, "/api"+procedure, body)
}

func (c *canSurfaces) tool(t *testing.T, credential, name string, args map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	body := map[string]any{"target": c.target}
	for k, v := range args {
		body[k] = v
	}
	return c.call(t, credential, callToolBody(name, body))
}

func requireRole001(t *testing.T, rec *httptest.ResponseRecorder, surface string) {
	t.Helper()
	var rpc struct {
		Code    string `json:"code"`
		Details []struct {
			Debug struct {
				Code string `json:"code"`
			} `json:"debug"`
		} `json:"details"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &rpc), "%s: body=%s", surface, rec.Body.String())
	assert.Equal(t, "permission_denied", rpc.Code, "%s", surface)
	assert.Contains(t, rec.Body.String(), "Ask an owner or admin.", "%s names the role side", surface)
}

// TestCan_RefusedOnEverySurface drives one refusal per permission through every surface that offers it. SECURITY: the gate lives in the service, so no surface can skip it.
func TestCan_RefusedOnEverySurface(t *testing.T) {
	c := newCanSurfaces(t)
	readOnly := []string{authn.ScopeYasakuRead}

	t.Run("procedure and MCP, scope side", func(t *testing.T) {
		for _, r := range []struct {
			perm, procedure, tool string
			args                  map[string]any
		}{
			{"wallets:write", yasakuv1connect.WalletServiceCreateWalletProcedure, yasakuv1mcp.CreateWalletToolName, map[string]any{"name": "Sneaky", "kind": "cash", "confirm": true}},
			{"categories:write", yasakuv1connect.CategoryServiceCreateCategoryProcedure, yasakuv1mcp.CreateCategoryToolName, map[string]any{"name": "Sneaky", "kind": "expense", "confirm": true}},
			{"transactions:write", yasakuv1connect.TransactionServiceRecordExpenseProcedure, yasakuv1mcp.RecordExpenseToolName, map[string]any{"wallet": ownWallet, "amount": map[string]any{"amount": "1000"}, "confirm": true}},
			{"wallets:read", yasakuv1connect.WalletServiceListWalletsProcedure, yasakuv1mcp.ListWalletsToolName, map[string]any{}},
		} {
			t.Run(r.perm, func(t *testing.T) {
				scopes := readOnly
				if r.perm == "wallets:read" {
					scopes = []string{authn.ScopeMembersRead}
				}
				rec := c.rpc(t, c.issuer.mint(t, controlAudience, scopes), r.procedure, r.args)
				assert.Equal(t, http.StatusForbidden, rec.Code, "procedure: %s", rec.Body.String())
				assert.Contains(t, rec.Body.String(), `"code":"permission_denied"`, "procedure")
				assert.Contains(t, rec.Header().Get("WWW-Authenticate"), "insufficient_scope", "procedure names the missing scope")
				mcp := c.tool(t, c.issuer.mint(t, mcpAudience, scopes), r.tool, r.args)
				assert.NotEqual(t, http.StatusOK, mcp.Code, "MCP tool: body=%s", mcp.Body.String())
			})
		}
	})

	t.Run("procedure and MCP, role side", func(t *testing.T) {
		requireRole001(t, c.rpc(t, c.memberToken(controlAudience), yasakuv1connect.PeriodServiceClosePeriodProcedure, map[string]any{"confirm": true}), "periods:close procedure")
		requireRole001(t, c.rpc(t, c.memberToken(controlAudience), yasakuv1connect.LedgerServiceUpdateSettingsProcedure, map[string]any{"periodStartDay": 25}), "ledger:settings procedure")
		payload := toolPayload(t, c.tool(t, c.memberToken(mcpAudience), yasakuv1mcp.ClosePeriodToolName, map[string]any{"confirm": true}))
		assert.Equal(t, apperror.CodePermissionDenied, payload.Code, "periods:close MCP tool")
		assert.Contains(t, payload.Message, "Ask an owner or admin.")
	})

	t.Run("console, role side", func(t *testing.T) {
		cookie := probeCookie(t, c.srv, session.Principal{UserID: c.member.ID, ActiveOrgID: c.org.ID})
		base := "/orgs/mcp-org"
		for _, r := range []struct {
			perm, path string
			form       url.Values
		}{
			{"periods:close", base + "/projects/mcp-project/periods/" + c.periodID + "/close", url.Values{"end_date": {time.Now().Format("2006-01-02")}}},
			{"ledger:settings", base + "/projects/mcp-project/settings", url.Values{"period_start_day": {"25"}}},
			{"projects:write", base + "/projects", url.Values{"name": {"Side"}, "slug": {"side"}}},
			{"members:write", base + "/members/" + c.plain.ID.String() + "/remove", url.Values{}},
			{"apikeys:write", base + "/apikeys", url.Values{"name": {"ci"}, "grant": {"all"}, "expires_in": {"30"}}},
		} {
			t.Run(r.perm, func(t *testing.T) {
				req := httptest.NewRequest(http.MethodPost, r.path, strings.NewReader(r.form.Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				req.AddCookie(cookie)
				rec := httptest.NewRecorder()
				c.srv.Web.ServeHTTP(rec, req)
				assert.Equal(t, http.StatusForbidden, rec.Code, "%s: body=%s", r.path, rec.Body.String())
			})
		}
		_, err := c.srv.Orgs.MembershipOf(fakes.Owner(t.Context(), tenant.Context{OrgID: c.org.ID}), c.org.ID, c.plain.ID)
		assert.NoError(t, err, "the refused remove left the member in place")
	})
}

// TestCan_WriteCredentialsRecord pins the allowed side: a member's token holding both scopes records through MCP, and a write-only token or key records through the procedure.
func TestCan_WriteCredentialsRecord(t *testing.T) {
	c := newCanSurfaces(t)
	expense := map[string]any{"wallet": ownWallet, "amount": map[string]any{"amount": "500"}, "confirm": true}

	rec := c.tool(t, c.memberToken(mcpAudience), yasakuv1mcp.RecordExpenseToolName, expense)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.NotContains(t, rec.Body.String(), `"isError":true`, "a member records an entry through MCP: %s", rec.Body.String())

	writeOnly := c.issuer.mint(t, controlAudience, []string{authn.ScopeYasakuWrite})
	rec = c.rpc(t, writeOnly, yasakuv1connect.TransactionServiceRecordExpenseProcedure, expense)
	assert.Equal(t, http.StatusOK, rec.Code, "a write-only token records: %s", rec.Body.String())

	_, key, err := c.srv.APIKeys.MintOrg(fakes.Owner(t.Context(), tenant.Context{OrgID: c.org.ID, UserID: c.owner}), "writer",
		[]string{authn.ScopeYasakuWrite}, apikey.ProjectGrant{All: true}, new(time.Now().Add(time.Hour)))
	require.NoError(t, err)
	rec = c.rpc(t, key, yasakuv1connect.TransactionServiceRecordExpenseProcedure, expense)
	assert.Equal(t, http.StatusOK, rec.Code, "a write-only key records: %s", rec.Body.String())
}
