package boot_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/apikey"
	"altalune.id/yasaku/internal/boot"
	"altalune.id/yasaku/internal/dataplane"
	"altalune.id/yasaku/internal/onboard"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform/authn"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/published"
	"altalune.id/yasaku/internal/user"
)

type dataplaneOpts struct {
	enabled     bool
	publicReads bool
}

type dataplaneFixture struct {
	srv      *boot.Server
	postsURL string
	postURL  string
	readKey  string
	noneKey  string
	orgCtx   context.Context
	orgID    uuid.UUID
	project  uuid.UUID
}

func newDataplaneFixture(t *testing.T, opts dataplaneOpts) *dataplaneFixture {
	t.Helper()

	cfg := newSmokeCfg(t)
	cfg.Mode = config.ModeCloud
	cfg.OIDC = config.OIDCConfig{
		Issuer:       stubIssuer(t),
		ClientID:     "dp-client",
		ClientSecret: "dp-secret",
	}
	cfg.DataPlane.Enabled = opts.enabled
	cfg.Blog.PublicReads = opts.publicReads
	cfg.HTTP.CSP.Enabled = true

	seed, err := boot.BootServer(context.Background(), cfg)
	require.NoError(t, err)
	seedUser, err := seed.Users.Create(context.Background(), user.CreateRequest{
		Email: "dp-seed@example.com", Name: "DP Seed", Source: user.SourceOIDC,
	})
	require.NoError(t, err)
	_, err = seed.Onboards.Complete(context.Background(), seedUser.ID, onboard.MethodCLIInit)
	require.NoError(t, err)
	require.NoError(t, seed.Close())

	srv, err := boot.BootServer(context.Background(), cfg)
	require.NoError(t, err)
	require.True(t, srv.Onboarded, "the fixture must be past the onboarding gate")
	t.Cleanup(func() { _ = srv.Close() })

	ctx := context.Background()
	owner, err := srv.Users.Create(ctx, user.CreateRequest{
		Email: "dp-owner@example.com", Name: "DP Owner", Source: user.SourceOIDC,
	})
	require.NoError(t, err)
	o, err := srv.Orgs.Create(ctx, org.CreateRequest{Slug: "dp-org", Name: "DP Org", OwnerID: owner.ID})
	require.NoError(t, err)

	orgCtx := tenant.Into(ctx, tenant.Context{OrgID: o.ID, UserID: owner.ID})
	p, err := srv.Projects.Create(orgCtx, o.ID, "dp-project", "DP Project")
	require.NoError(t, err)

	projCtx := tenant.WithProject(orgCtx, p.ID)
	cat, err := srv.Categories.Create(projCtx, "News", "news")
	require.NoError(t, err)
	post, err := srv.Posts.Create(projCtx, cat.ID, "Hello", "hello", "body")
	require.NoError(t, err)
	_, err = srv.Posts.Publish(projCtx, post.ID, 0)
	require.NoError(t, err)

	_, readKey, err := srv.APIKeys.Mint(projCtx, "reader", []string{authn.ScopeYasakuRead}, nil, soon())
	require.NoError(t, err)
	_, noneKey, err := srv.APIKeys.Mint(projCtx, "keys-only", []string{authn.ScopeAPIKeysRead}, nil, soon())
	require.NoError(t, err)

	base := "/api/v1/orgs/" + o.Slug + "/projects/" + p.Slug + "/posts"
	return &dataplaneFixture{
		srv:      srv,
		postsURL: base,
		postURL:  base + "/hello",
		readKey:  readKey,
		noneKey:  noneKey,
		orgCtx:   orgCtx,
		orgID:    o.ID,
		project:  p.ID,
	}
}

func (f *dataplaneFixture) get(t *testing.T, path, key string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	rec := httptest.NewRecorder()
	f.srv.Web.ServeHTTP(rec, req)
	return rec
}

// TestDataPlane_IsMountedAndAuthenticates walks the surface end to end on the real wiring.
func TestDataPlane_IsMountedAndAuthenticates(t *testing.T) {
	if !published.Blog {
		t.Skip("yasaku: blog is not mounted; see internal/boot/surfaces_yasaku.go")
	}
	f := newDataplaneFixture(t, dataplaneOpts{enabled: true})

	t.Run("valid key reads the post", func(t *testing.T) {
		rec := f.get(t, f.postURL, f.readKey)
		require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
		require.Contains(t, rec.Body.String(), `"slug":"hello"`)
		require.NotEmpty(t, rec.Header().Get("ETag"), "a data-plane read must carry a validator")
	})

	t.Run("valid key lists posts", func(t *testing.T) {
		rec := f.get(t, f.postsURL, f.readKey)
		require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
		require.Contains(t, rec.Body.String(), `"slug":"hello"`)
	})

	t.Run("no credential is 404 while publicReads is off", func(t *testing.T) {
		rec := f.get(t, f.postURL, "")
		require.Equal(t, http.StatusNotFound, rec.Code, "body=%s", rec.Body.String())
		require.Contains(t, rec.Body.String(), `"code":"not_found"`)
	})

	t.Run("bogus key is 401", func(t *testing.T) {
		rec := f.get(t, f.postURL, "key_not_a_real_key")
		require.Equal(t, http.StatusUnauthorized, rec.Code, "body=%s", rec.Body.String())
	})

	// SECURITY: an insufficient scope reports the same masked not-found as a missing post.
	t.Run("insufficient scope is 404", func(t *testing.T) {
		rec := f.get(t, f.postURL, f.noneKey)
		require.Equal(t, http.StatusNotFound, rec.Code, "body=%s", rec.Body.String())
		require.NotContains(t, rec.Body.String(), "hello")
	})

	t.Run("unknown org slug is the same 404", func(t *testing.T) {
		rec := f.get(t, "/api/v1/orgs/absent/projects/dp-project/posts/hello", f.readKey)
		require.Equal(t, http.StatusNotFound, rec.Code)
	})
}

func TestDataPlane_PublicReadsFlagAdmitsPublishedPosts(t *testing.T) {
	if !published.Blog {
		t.Skip("yasaku: blog is not mounted; see internal/boot/surfaces_yasaku.go")
	}
	f := newDataplaneFixture(t, dataplaneOpts{enabled: true, publicReads: true})

	rec := f.get(t, f.postURL, "")
	require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
	require.Contains(t, rec.Body.String(), `"slug":"hello"`)
}

// TestDataPlane_RunsOnTheDataChainNotTheConsoleChain is the mount guard: no CSP header or session cookie on /api/v1.
func TestDataPlane_RunsOnTheDataChainNotTheConsoleChain(t *testing.T) {
	if !published.Blog {
		t.Skip("yasaku: blog is not mounted; see internal/boot/surfaces_yasaku.go")
	}
	f := newDataplaneFixture(t, dataplaneOpts{enabled: true, publicReads: true})

	console := f.get(t, "/login", "")
	require.NotEmpty(t, console.Header().Get("Content-Security-Policy"),
		"the console chain must set CSP, or this guard proves nothing")

	for _, key := range []string{"", f.readKey} {
		rec := f.get(t, f.postURL, key)
		require.Empty(t, rec.Header().Get("Content-Security-Policy"),
			"the data plane ran the console chain: CSP is an SSR concern")
		require.Empty(t, rec.Header().Values("Set-Cookie"),
			"the data plane ran the console chain: it must never issue a session cookie")
		require.Equal(t, "application/json; charset=utf-8", rec.Header().Get("Content-Type"))
	}
}

// TestDataPlane_DisabledLeavesTheSubtreeUnmounted keeps dataplane.enabled meaningful.
func TestDataPlane_DisabledLeavesTheSubtreeUnmounted(t *testing.T) {
	f := newDataplaneFixture(t, dataplaneOpts{enabled: false, publicReads: true})

	rec := f.get(t, f.postURL, f.readKey)
	require.NotEqual(t, http.StatusOK, rec.Code)
	require.NotContains(t, rec.Body.String(), `"slug":"hello"`,
		"an unmounted data plane must not serve posts: %s", rec.Body.String())
}

// TestDataPlane_OrgKeyAgreesWithItsGrant proves the data plane asks the same reach rule as the control plane for an org key.
func TestDataPlane_OrgKeyAgreesWithItsGrant(t *testing.T) {
	if !published.Blog {
		t.Skip("yasaku: blog is not mounted; see internal/boot/surfaces_yasaku.go")
	}
	f := newDataplaneFixture(t, dataplaneOpts{enabled: true})
	other, err := f.srv.Projects.Create(f.orgCtx, f.orgID, "dp-other", "DP Other")
	require.NoError(t, err)

	_, granted, err := f.srv.APIKeys.MintOrg(f.orgCtx, "granted", []string{dataplane.ScopePostsRead},
		apikey.ProjectGrant{ProjectIDs: []uuid.UUID{f.project}}, soon())
	require.NoError(t, err)
	_, elsewhere, err := f.srv.APIKeys.MintOrg(f.orgCtx, "elsewhere", []string{dataplane.ScopePostsRead},
		apikey.ProjectGrant{ProjectIDs: []uuid.UUID{other.ID}}, soon())
	require.NoError(t, err)

	rec := f.get(t, f.postURL, granted)
	require.Equal(t, http.StatusOK, rec.Code, "an org key granted the project must read it; body=%s", rec.Body.String())
	rec = f.get(t, f.postURL, elsewhere)
	require.Equal(t, http.StatusNotFound, rec.Code, "an org key granted another project must read nothing here; body=%s", rec.Body.String())
}

func soon() *time.Time {
	t := time.Now().UTC().Add(24 * time.Hour)
	return &t
}
