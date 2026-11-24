package controlplane_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	yasakuv1 "altalune.id/yasaku/gen/go/yasaku/v1"
	"altalune.id/yasaku/gen/go/yasaku/v1/yasakuv1connect"
	"altalune.id/yasaku/internal/apikey"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform/authn"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/platform/tokens"
	"altalune.id/yasaku/internal/testutil/fakes"
	"altalune.id/yasaku/internal/user"
)

func bearer(token string) connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			req.Header().Set("Authorization", "Bearer "+token)
			return next(ctx, req)
		}
	}
}

func requireNotManager(t *testing.T, err error) {
	t.Helper()
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err), "err=%v", err)
	require.Contains(t, err.Error(), "Ask an owner or admin.", "the refusal names the role side")
}

// SECURITY: membership is org-wide, so a plain member's token and a member's personal key must both hit the one service gate on RPC (and MCP, which calls the same handlers).
func TestManagerGate_RPCRefusesAMemberAndAdmitsManagersAndOrgKeys(t *testing.T) {
	h := newYasakuHarness(t)
	ctx := t.Context()
	h.createWallet(ctx, "BCA", nil)
	_, err := h.txClient().RecordExpense(ctx, connect.NewRequest(&yasakuv1.RecordExpenseRequest{
		Wallet: "BCA", Amount: &yasakuv1.Money{Amount: "25000"}, Note: "kopi", Confirm: true,
	}))
	require.NoError(t, err)

	member, err := h.boot.Users.EnsureFromOIDC(ctx, user.Claims{Issuer: testIssuer, Subject: "sub-member", Email: "member@example.com", EmailVerified: true, Name: "Member"})
	require.NoError(t, err)
	orgCtx := fakes.Owner(ctx, tenant.Context{OrgID: h.org.ID, UserID: h.user.ID})
	_, err = h.boot.Orgs.AddMember(orgCtx, h.org.ID, member.ID, org.RoleMember)
	require.NoError(t, err)

	memberCtx := fakes.Owner(ctx, tenant.Context{OrgID: h.org.ID, UserID: member.ID})
	soon := time.Now().Add(24 * time.Hour)
	_, personal, err := h.boot.APIKeys.MintPersonal(memberCtx, "laptop", []string{authn.ScopeYasakuRead, authn.ScopeYasakuWrite}, apikey.ProjectGrant{All: true}, &soon)
	require.NoError(t, err)
	_, orgKey, err := h.boot.APIKeys.MintOrg(orgCtx, "bot", []string{authn.ScopeYasakuRead, authn.ScopeYasakuWrite}, apikey.ProjectGrant{All: true}, &soon)
	require.NoError(t, err)

	memberVerifier := stubVerifier{principal: fullTokenGrant(session.Principal{UserID: member.ID, Email: member.Email, IDPIssuer: testIssuer, IDPSubject: "sub-member"})}
	h.boot.API.Authn = authn.Chain{tokens.NewAuthenticator(memberVerifier, authn.Scheme{Prefix: apikey.DefaultPrefix}), h.live}
	ts := httptest.NewServer(h.boot.API.Handler(""))
	t.Cleanup(ts.Close)
	base := ts.URL + "/api"

	target := &yasakuv1.Target{Org: h.org.Slug, Project: h.project.Slug}
	day := int32(25)
	clients := func(token string) (yasakuv1connect.LedgerServiceClient, yasakuv1connect.PeriodServiceClient) {
		opts := connect.WithInterceptors(bearer(token))
		return yasakuv1connect.NewLedgerServiceClient(http.DefaultClient, base, opts),
			yasakuv1connect.NewPeriodServiceClient(http.DefaultClient, base, opts)
	}

	keyLedger, keyPeriods := clients(orgKey)
	closed, err := keyPeriods.ClosePeriod(ctx, connect.NewRequest(&yasakuv1.ClosePeriodRequest{Target: target, Confirm: true}))
	require.NoError(t, err, "an org key is a manager's grant")
	closedID := closed.Msg.GetResult().GetId()

	for name, token := range map[string]string{"member token": "stub.token.value", "member personal key": personal} {
		t.Run(name, func(t *testing.T) {
			lc, pc := clients(token)
			_, err := lc.UpdateSettings(ctx, connect.NewRequest(&yasakuv1.UpdateSettingsRequest{Target: target, PeriodStartDay: &day}))
			requireNotManager(t, err)
			_, err = pc.ClosePeriod(ctx, connect.NewRequest(&yasakuv1.ClosePeriodRequest{Target: target, Confirm: true}))
			requireNotManager(t, err)
			_, err = pc.ReopenPeriod(ctx, connect.NewRequest(&yasakuv1.ReopenPeriodRequest{Target: target, Period: closedID, Confirm: true}))
			requireNotManager(t, err)
			_, err = pc.RenamePeriod(ctx, connect.NewRequest(&yasakuv1.RenamePeriodRequest{Target: target, Period: closedID, Name: "Mine"}))
			requireNotManager(t, err)
			got, err := lc.GetSettings(ctx, connect.NewRequest(&yasakuv1.GetSettingsRequest{Target: target}))
			require.NoError(t, err, "a member keeps reading the settings")
			require.NotEqual(t, day, got.Msg.GetSettings().GetPeriodStartDay())
		})
	}

	_, err = keyLedger.UpdateSettings(ctx, connect.NewRequest(&yasakuv1.UpdateSettingsRequest{Target: target, PeriodStartDay: &day}))
	require.NoError(t, err)
	_, err = keyPeriods.RenamePeriod(ctx, connect.NewRequest(&yasakuv1.RenamePeriodRequest{Target: target, Period: closedID, Name: "Ours"}))
	require.NoError(t, err)
	_, err = keyPeriods.ReopenPeriod(ctx, connect.NewRequest(&yasakuv1.ReopenPeriodRequest{Target: target, Period: closedID, Confirm: true}))
	require.NoError(t, err)
}
