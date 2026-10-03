package boot_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/boot"
	"altalune.id/yasaku/internal/onboard"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/events"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/user"
	"altalune.id/yasaku/internal/webhook"
)

func TestBootServer_PublishEnqueuesAWebhookDelivery(t *testing.T) {
	assertPublishEnqueues(t, newSmokeCfg(t))
}

func assertPublishEnqueues(t *testing.T, cfg *config.Config) {
	t.Helper()
	cfg.Mode = config.ModeCloud
	cfg.OIDC = config.OIDCConfig{Issuer: stubIssuer(t), ClientID: "wh-client", ClientSecret: "wh-secret"}

	ctx := context.Background()
	seed, err := boot.BootServer(ctx, cfg, boot.WithScheduler(false))
	require.NoError(t, err)
	seedUser, err := seed.Users.Create(ctx, user.CreateRequest{Email: "wh-seed@example.com", Name: "Seed", Source: user.SourceOIDC})
	require.NoError(t, err)
	_, err = seed.Onboards.Complete(ctx, seedUser.ID, onboard.MethodCLIInit)
	require.NoError(t, err)
	require.NoError(t, seed.Close())

	srv, err := boot.BootServer(ctx, cfg, boot.WithScheduler(false))
	require.NoError(t, err)
	t.Cleanup(func() { _ = srv.Close() })

	owner, err := srv.Users.Create(ctx, user.CreateRequest{Email: "wh-owner@example.com", Name: "Owner", Source: user.SourceOIDC})
	require.NoError(t, err)
	o, err := srv.Orgs.Create(ctx, org.CreateRequest{Slug: "wh-org", Name: "WH Org", OwnerID: owner.ID})
	require.NoError(t, err)
	orgCtx := tenant.Into(ctx, tenant.Context{OrgID: o.ID, UserID: owner.ID})
	p, err := srv.Projects.Create(orgCtx, o.ID, "wh-project", "WH Project")
	require.NoError(t, err)
	projCtx := tenant.WithProject(orgCtx, p.ID)

	endpoint, _, err := srv.Webhooks.Create(projCtx, "https://hooks.example.com/in", "", []events.Type{events.PostPublished})
	require.NoError(t, err)
	cat, err := srv.Categories.Create(projCtx, "News", "news")
	require.NoError(t, err)
	post, err := srv.Posts.Create(projCtx, cat.ID, "Hello", "hello", "body")
	require.NoError(t, err)

	_, err = srv.Posts.Publish(projCtx, post.ID, 0)
	require.NoError(t, err)

	deliveries, err := srv.Webhooks.Deliveries(projCtx, endpoint.ID, 10)
	require.NoError(t, err)
	require.Len(t, deliveries, 1)
	assert.Equal(t, events.PostPublished, deliveries[0].EventType)
	var env webhook.Envelope
	require.NoError(t, json.Unmarshal(deliveries[0].Payload, &env))
	assert.Equal(t, "wh-project", env.Tenant.ProjectSlug)
	assert.Equal(t, p.ID, env.Tenant.ProjectID)
}
