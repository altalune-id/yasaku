package controlplane_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	yasakuv1 "altalune.id/yasaku/gen/go/yasaku/v1"
	"altalune.id/yasaku/gen/go/yasaku/v1/yasakuv1connect"
	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/controlplane"
	"altalune.id/yasaku/internal/opensheetsync"
	"altalune.id/yasaku/internal/platform/authn"
	"altalune.id/yasaku/internal/platform/sealer"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/project"
	"altalune.id/yasaku/internal/testutil/fakes"
)

const opensheetKey = "osk_live_0123456789wxyz"

type opensheetFixture struct {
	h       *harness
	orgID   uuid.UUID
	project *project.Project
	sheets  *fakes.Opensheet
	store   *fakes.OpensheetSync
}

func newOpensheetFixture(t *testing.T) *opensheetFixture {
	t.Helper()
	userID := uuid.New()
	o := newOrg(t, userID, "acme")
	f := &opensheetFixture{orgID: o.ID, sheets: fakes.NewOpensheet(t, "acme-os", "home", opensheetKey), store: fakes.NewOpensheetSync()}
	for _, tab := range opensheetsync.Contract() {
		f.sheets.AddSheet(tab.DefaultSlug, fakes.OpensheetSheet{Columns: tab.Columns, Writable: true})
	}
	key, err := sealer.GenerateKey()
	require.NoError(t, err)
	sl, err := sealer.New(key)
	require.NoError(t, err)

	f.h = newHarnessOpts(t, session.Principal{UserID: userID, Email: "a@b", ActiveOrgID: o.ID}, nil, func(d *controlplane.Deps) {
		log := slog.New(slog.NewTextHandler(io.Discard, nil))
		unexpected := apperror.NewReporter(log, false).Unexpected
		mirror := opensheetsync.NewMirror(f.store, &fakes.Queue{}, false, log, unexpected, nil)
		d.Opensheet = opensheetsync.NewService(f.store, log, unexpected, opensheetsync.ServiceDeps{
			Mirror: mirror, Members: d.Orgs, Sealer: sl, UnitOfWork: fakes.UnitOfWork,
			Endpoint: opensheetsync.Endpoint{BaseURL: f.sheets.URL(), AllowPrivateHosts: true, Timeout: 2 * time.Second},
		})
	})
	f.h.saveOrg(t, o, userID)
	p, err := project.New(o.ID, "main", "Main")
	require.NoError(t, err)
	require.NoError(t, f.h.projs.Save(context.Background(), p))
	f.project = p
	return f
}

func (f *opensheetFixture) client() yasakuv1connect.OpensheetServiceClient {
	return yasakuv1connect.NewOpensheetServiceClient(http.DefaultClient, f.h.server.URL+"/api")
}

func opensheetSettings(key string) *yasakuv1.OpensheetSettings {
	slugs := opensheetsync.DefaultSheetSlugs()
	return &yasakuv1.OpensheetSettings{
		OsOrg: "acme-os", OsProject: "home", ApiKey: key,
		TransactionsSheet: slugs.Transactions, WalletsSheet: slugs.Wallets, CategoriesSheet: slugs.Categories,
	}
}

func asOwner[T any](m *T) *connect.Request[T] {
	req := connect.NewRequest(m)
	withBearer(req.Header())
	return req
}

func asKey[T any](key string, m *T) *connect.Request[T] {
	req := connect.NewRequest(m)
	withKey(key)(req.Header())
	return req
}

func requireNoKeyOnTheWire(t *testing.T, sealed []byte, msgs ...proto.Message) {
	t.Helper()
	for _, m := range msgs {
		wire, err := proto.Marshal(m)
		require.NoError(t, err)
		js, err := protojson.Marshal(m)
		require.NoError(t, err)
		for _, body := range []string{string(wire), string(js)} {
			require.NotContains(t, body, opensheetKey, "the plaintext key went back to the caller")
			require.NotContains(t, body, string(sealed), "the sealed key went back to the caller")
		}
	}
}

func TestOpensheetService_OwnerRunsTheWholeLifecycleAndNeverSeesTheKey(t *testing.T) {
	f := newOpensheetFixture(t)
	ctx := t.Context()
	c := f.client()
	tgt := target("acme", "main")

	got, err := c.GetOpensheetLink(ctx, asOwner(&yasakuv1.GetOpensheetLinkRequest{Target: tgt}))
	require.NoError(t, err)
	require.Nil(t, got.Msg.GetLink(), "no link yet")

	tested, err := c.TestOpensheetLink(ctx, asOwner(&yasakuv1.TestOpensheetLinkRequest{Target: tgt, Settings: opensheetSettings(opensheetKey)}))
	require.NoError(t, err)
	require.True(t, tested.Msg.GetOk(), "checks=%v", tested.Msg.GetChecks())
	require.Len(t, tested.Msg.GetChecks(), len(opensheetsync.Contract()))

	saved, err := c.SaveOpensheetLink(ctx, asOwner(&yasakuv1.SaveOpensheetLinkRequest{Target: tgt, Settings: opensheetSettings(opensheetKey)}))
	require.NoError(t, err)
	require.Equal(t, opensheetsync.KeyHint(opensheetKey), saved.Msg.GetLink().GetApiKeyHint())
	require.NotEmpty(t, saved.Msg.GetLink().GetVerifiedAt())
	require.True(t, saved.Msg.GetLink().GetAwaitingFirstSync())
	require.False(t, saved.Msg.GetLink().GetEnabled(), "a Save never turns the mirror on")

	stored, err := f.store.LinkByProject(ctx, f.orgID, f.project.ID)
	require.NoError(t, err)
	require.NotEmpty(t, stored.APIKeySealed, "the key was sealed")

	on, err := c.SetOpensheetLinkEnabled(ctx, asOwner(&yasakuv1.SetOpensheetLinkEnabledRequest{Target: tgt, Enabled: true}))
	require.NoError(t, err)
	require.True(t, on.Msg.GetLink().GetEnabled())

	synced, err := c.SyncOpensheetNow(ctx, asOwner(&yasakuv1.SyncOpensheetNowRequest{Target: tgt}))
	require.NoError(t, err)
	require.Zero(t, synced.Msg.GetMarked(), "an empty project marks nothing")

	got, err = c.GetOpensheetLink(ctx, asOwner(&yasakuv1.GetOpensheetLinkRequest{Target: tgt}))
	require.NoError(t, err)
	require.True(t, got.Msg.GetLink().GetEnabled())
	requireNoKeyOnTheWire(t, stored.APIKeySealed, tested.Msg, saved.Msg, on.Msg, got.Msg)

	_, err = c.DeleteOpensheetLink(ctx, asOwner(&yasakuv1.DeleteOpensheetLinkRequest{Target: tgt}))
	require.NoError(t, err)
	got, err = c.GetOpensheetLink(ctx, asOwner(&yasakuv1.GetOpensheetLinkRequest{Target: tgt}))
	require.NoError(t, err)
	require.Nil(t, got.Msg.GetLink(), "the link is gone")
}

func TestOpensheetService_RefusalsCarryTheirOSLCodeAndConnectCode(t *testing.T) {
	f := newOpensheetFixture(t)
	c := f.client()
	tgt := target("acme", "main")

	badSlug := opensheetSettings(opensheetKey)
	badSlug.WalletsSheet = "Not A Slug"
	unknownSheet := opensheetSettings(opensheetKey)
	unknownSheet.CategoriesSheet = "nowhere"

	tests := []struct {
		name     string
		call     func(context.Context) error
		code     string
		connCode connect.Code
	}{
		{"enable without a link", func(ctx context.Context) error {
			_, err := c.SetOpensheetLinkEnabled(ctx, asOwner(&yasakuv1.SetOpensheetLinkEnabledRequest{Target: tgt, Enabled: true}))
			return err
		}, apperror.CodeOpensheetLinkNotFound, connect.CodeNotFound},
		{"invalid slug", func(ctx context.Context) error {
			_, err := c.SaveOpensheetLink(ctx, asOwner(&yasakuv1.SaveOpensheetLinkRequest{Target: tgt, Settings: badSlug}))
			return err
		}, apperror.CodeOpensheetInvalidSetting, connect.CodeInvalidArgument},
		{"first save without a key", func(ctx context.Context) error {
			_, err := c.SaveOpensheetLink(ctx, asOwner(&yasakuv1.SaveOpensheetLinkRequest{Target: tgt, Settings: opensheetSettings("")}))
			return err
		}, apperror.CodeOpensheetAPIKeyRequired, connect.CodeInvalidArgument},
		{"save refused by the Test", func(ctx context.Context) error {
			_, err := c.SaveOpensheetLink(ctx, asOwner(&yasakuv1.SaveOpensheetLinkRequest{Target: tgt, Settings: unknownSheet}))
			return err
		}, apperror.CodeOpensheetSheetUnreachable, connect.CodeFailedPrecondition},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call(t.Context())
			require.Error(t, err)
			require.Equal(t, tt.connCode, connectCode(err), "err=%v", err)
			require.Equal(t, tt.code, appErrorDetail(t, err).GetCode())
		})
	}

	t.Run("sync now on a link turned off", func(t *testing.T) {
		ctx := t.Context()
		_, err := c.SaveOpensheetLink(ctx, asOwner(&yasakuv1.SaveOpensheetLinkRequest{Target: tgt, Settings: opensheetSettings(opensheetKey)}))
		require.NoError(t, err)
		_, err = c.SyncOpensheetNow(ctx, asOwner(&yasakuv1.SyncOpensheetNowRequest{Target: tgt}))
		require.Equal(t, connect.CodeFailedPrecondition, connectCode(err), "err=%v", err)
		require.Equal(t, apperror.CodeOpensheetLinkDisabled, appErrorDetail(t, err).GetCode())
	})

	t.Run("a failing tab is a check, not an error", func(t *testing.T) {
		res, err := c.TestOpensheetLink(t.Context(), asOwner(&yasakuv1.TestOpensheetLinkRequest{Target: tgt, Settings: unknownSheet}))
		require.NoError(t, err)
		require.False(t, res.Msg.GetOk())
		var codes []string
		for _, ch := range res.Msg.GetChecks() {
			codes = append(codes, ch.GetCode())
		}
		require.Contains(t, codes, apperror.CodeOpensheetSheetUnreachable)
	})
}

// SECURITY: D8, a key principal has no user, so even a yasaku:write key cannot re-point or switch the mirror.
func TestOpensheetService_AKeyReadsTheLinkButNeverChangesIt(t *testing.T) {
	f := newOpensheetFixture(t)
	ctx := t.Context()
	c := f.client()
	tgt := target("acme", "main")

	_, err := c.SaveOpensheetLink(ctx, asOwner(&yasakuv1.SaveOpensheetLinkRequest{Target: tgt, Settings: opensheetSettings(opensheetKey)}))
	require.NoError(t, err)
	stored, err := f.store.LinkByProject(tenant.Into(ctx, tenant.Context{OrgID: f.orgID, ProjectID: f.project.ID}), f.orgID, f.project.ID)
	require.NoError(t, err)

	writeKey := f.h.mintKey(f.orgID, f.project.ID, []string{authn.ScopeYasakuRead, authn.ScopeYasakuWrite}, nil)
	got, err := c.GetOpensheetLink(ctx, asKey(writeKey, &yasakuv1.GetOpensheetLinkRequest{Target: tgt}))
	require.NoError(t, err)
	require.Equal(t, "acme-os", got.Msg.GetLink().GetOsOrg())
	requireNoKeyOnTheWire(t, stored.APIKeySealed, got.Msg)

	elsewhere := opensheetSettings(opensheetKey)
	elsewhere.OsOrg = "attacker"
	calls := map[string]func(context.Context, string) error{
		"Test": func(ctx context.Context, k string) error {
			_, err := c.TestOpensheetLink(ctx, asKey(k, &yasakuv1.TestOpensheetLinkRequest{Target: tgt, Settings: elsewhere}))
			return err
		},
		"Save": func(ctx context.Context, k string) error {
			_, err := c.SaveOpensheetLink(ctx, asKey(k, &yasakuv1.SaveOpensheetLinkRequest{Target: tgt, Settings: elsewhere}))
			return err
		},
		"SetEnabled": func(ctx context.Context, k string) error {
			_, err := c.SetOpensheetLinkEnabled(ctx, asKey(k, &yasakuv1.SetOpensheetLinkEnabledRequest{Target: tgt, Enabled: true}))
			return err
		},
		"SyncNow": func(ctx context.Context, k string) error {
			_, err := c.SyncOpensheetNow(ctx, asKey(k, &yasakuv1.SyncOpensheetNowRequest{Target: tgt}))
			return err
		},
		"Delete": func(ctx context.Context, k string) error {
			_, err := c.DeleteOpensheetLink(ctx, asKey(k, &yasakuv1.DeleteOpensheetLinkRequest{Target: tgt}))
			return err
		},
	}
	readKey := f.h.mintKey(f.orgID, f.project.ID, []string{authn.ScopeYasakuRead}, nil)
	for name, call := range calls {
		t.Run(name+" with yasaku:write", func(t *testing.T) {
			err := call(t.Context(), writeKey)
			require.Equal(t, connect.CodePermissionDenied, connectCode(err), "err=%v", err)
			require.True(t, strings.HasPrefix(appErrorDetail(t, err).GetCode(), "ORG"), "the manager gate refused it, not the scope check: %v", err)
		})
		t.Run(name+" with yasaku:read", func(t *testing.T) {
			require.Equal(t, connect.CodePermissionDenied, connectCode(call(t.Context(), readKey)))
		})
	}

	after, err := f.store.LinkByProject(ctx, f.orgID, f.project.ID)
	require.NoError(t, err)
	require.Equal(t, "acme-os", after.OSOrg, "the link still points where the owner saved it")
	require.False(t, after.Enabled)
}

func TestOpensheetService_AnAmbiguousTargetNeedsAProject(t *testing.T) {
	f := newOpensheetFixture(t)
	side, err := project.New(f.orgID, "side", "Side")
	require.NoError(t, err)
	require.NoError(t, f.h.projs.Save(context.Background(), side))

	_, err = f.client().GetOpensheetLink(t.Context(), asOwner(&yasakuv1.GetOpensheetLinkRequest{}))
	require.Equal(t, connect.CodeInvalidArgument, connectCode(err), "err=%v", err)
	detail := appErrorDetail(t, err)
	require.Equal(t, "project", detail.GetMeta()["field"])
	require.Contains(t, detail.GetMeta()["reason"], "main")
	require.Contains(t, detail.GetMeta()["reason"], "side")
}

func TestOpensheetService_EnableAnswersWithTheBackfillBacklog(t *testing.T) {
	f := newOpensheetFixture(t)
	ctx := t.Context()
	c := f.client()
	tgt := target("acme", "main")
	for range 2 {
		f.store.SeedEntity(f.orgID, f.project.ID, opensheetsync.EntityWallet, uuid.New())
	}
	f.store.SeedEntity(f.orgID, f.project.ID, opensheetsync.EntityCategory, uuid.New())

	_, err := c.SaveOpensheetLink(ctx, asOwner(&yasakuv1.SaveOpensheetLinkRequest{Target: tgt, Settings: opensheetSettings(opensheetKey)}))
	require.NoError(t, err)
	on, err := c.SetOpensheetLinkEnabled(ctx, asOwner(&yasakuv1.SetOpensheetLinkEnabledRequest{Target: tgt, Enabled: true}))
	require.NoError(t, err)
	require.True(t, on.Msg.GetLink().GetEnabled())
	require.EqualValues(t, 3, on.Msg.GetLink().GetPending(), "the backfill marked every row, and the answer says so")

	saved, err := c.SaveOpensheetLink(ctx, asOwner(&yasakuv1.SaveOpensheetLinkRequest{Target: tgt, Settings: opensheetSettings("")}))
	require.NoError(t, err)
	require.EqualValues(t, 3, saved.Msg.GetLink().GetPending(), "a Save answers with the current backlog")

	got, err := c.GetOpensheetLink(ctx, asOwner(&yasakuv1.GetOpensheetLinkRequest{Target: tgt}))
	require.NoError(t, err)
	require.EqualValues(t, got.Msg.GetLink().GetPending(), on.Msg.GetLink().GetPending(), "the same counts the console reads")
}
