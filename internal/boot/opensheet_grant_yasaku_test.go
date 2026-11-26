package boot

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/altalune-id/oidcrp"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/opensheetsync"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform/capabilities"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/sealer"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/testutil/fakes"
	"altalune.id/yasaku/internal/user"
)

// NOTE: fakeIssuer is the upstream IdP's discovery document and revocation endpoint, so boot's real oidcrp client revokes against it.
type fakeIssuer struct {
	srv     *httptest.Server
	mu      sync.Mutex
	revoked []string
}

func newFakeIssuer(t *testing.T) *fakeIssuer {
	t.Helper()
	f := &fakeIssuer{}
	mux := http.NewServeMux()
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer": f.srv.URL, "authorization_endpoint": f.srv.URL + "/authorize", "token_endpoint": f.srv.URL + "/token",
			"jwks_uri": f.srv.URL + "/jwks", "revocation_endpoint": f.srv.URL + "/revoke",
		})
	})
	mux.HandleFunc("POST /revoke", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.mu.Lock()
		f.revoked = append(f.revoked, r.PostForm.Get("token"))
		f.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})
	return f
}

func (f *fakeIssuer) Revoked() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.revoked)
}

func mountOpensheetGrants(t *testing.T, cfg *config.Config) *fakeIssuer {
	t.Helper()
	mountOpensheet(cfg)
	iss := newFakeIssuer(t)
	cfg.OIDC.Issuer, cfg.OIDC.ClientID, cfg.OIDC.ClientSecret = iss.srv.URL, "yasaku", "secret"
	cfg.Opensheet.Audience = "https://sheets.example.com/api"
	return iss
}

func testSealer(t *testing.T) sealer.Sealer {
	t.Helper()
	key, err := sealer.ParseKey(testEncryptionKey)
	require.NoError(t, err)
	sl, err := sealer.New(key)
	require.NoError(t, err)
	return sl
}

func seedOpensheetGrant(t *testing.T, grants opensheetsync.GrantStore, tc tenant.Context, refresh string) {
	t.Helper()
	g, err := opensheetsync.NewGrant(testSealer(t), tc.OrgID, tc.ProjectID, tc.UserID,
		oidcrp.TokenSet{AccessToken: "access", RefreshToken: refresh, ExpiresAt: time.Now().Add(time.Hour)}, time.Now())
	require.NoError(t, err)
	require.NoError(t, grants.SaveGrant(fakes.Owner(t.Context(), tc), g))
}

func TestBuildServices_OpensheetAuthNeedsAnAudience(t *testing.T) {
	cfg := newWiringConfig(t)
	mountOpensheet(cfg)
	svcs, err := buildServices(cfg, newWiringKernel(t, cfg), capabilities.Capabilities{})
	require.NoError(t, err)
	require.Nil(t, svcs.OpensheetAuth, "no opensheet.audience, no OAuth client")

	cfg = newWiringConfig(t)
	mountOpensheetGrants(t, cfg)
	svcs, err = buildServices(cfg, newWiringKernel(t, cfg), capabilities.Capabilities{})
	require.NoError(t, err)
	require.NotNil(t, svcs.OpensheetAuth)
}

func TestMemberRemoved_RevokesOpensheetGrantThroughTheHook(t *testing.T) {
	f := newSQLiteWipeFixture(t)
	member, err := f.svcs.Users.Create(f.ctx, user.CreateRequest{Email: "w-member@example.com", Name: "M", Source: user.SourceGenesis})
	require.NoError(t, err)
	_, err = f.svcs.Orgs.AddMember(f.ctx, f.tc.OrgID, member.ID, org.RoleMember)
	require.NoError(t, err)
	byMember := tenant.Context{OrgID: f.tc.OrgID, ProjectID: f.tc.ProjectID, UserID: member.ID}
	seedOpensheetGrant(t, f.grants, byMember, "member-refresh")

	require.NoError(t, f.svcs.Orgs.RemoveMember(f.ctx, f.tc.OrgID, member.ID))
	_, err = f.grants.GrantByProject(f.ctx, f.tc.OrgID, f.tc.ProjectID)
	require.True(t, opensheetsync.IsGrantNotFoundError(err), "the grant the member connected is gone: %v", err)
	require.Equal(t, []string{"member-refresh"}, f.issuer.Revoked(), "and its refresh token is revoked upstream")
}
