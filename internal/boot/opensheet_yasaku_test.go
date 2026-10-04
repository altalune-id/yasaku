package boot

import (
	"slices"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	yasakuv1 "altalune.id/yasaku/gen/go/yasaku/v1"
	"altalune.id/yasaku/internal/category"
	"altalune.id/yasaku/internal/opensheetsync"
	"altalune.id/yasaku/internal/platform/capabilities"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/sealer"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/transaction"
	"altalune.id/yasaku/internal/user"
	"altalune.id/yasaku/internal/wallet"
)

const testEncryptionKey = "abababababababababababababababababababababababababababababababab"

func mountOpensheet(cfg *config.Config) {
	cfg.Opensheet = config.OpensheetConfig{BaseURL: "http://127.0.0.1:9", AllowPrivateHosts: true}
	cfg.Security.EncryptionKey = testEncryptionKey
}

func TestBuildServices_OpensheetIsUnmountedWithoutABaseURL(t *testing.T) {
	cfg := newWiringConfig(t)
	svcs, err := buildServices(cfg, newWiringKernel(t, cfg), capabilities.Capabilities{})
	require.NoError(t, err)
	require.Nil(t, svcs.Opensheet)
	require.Nil(t, svcs.OpensheetSync)
	require.Nil(t, svcs.OpensheetMirror)
	require.Empty(t, opensheetsync.NewConsumer(svcs.OpensheetSync).ConsumerHandlers())
	require.Empty(t, opensheetsync.NewScheduler(svcs.OpensheetMirror, discardLogger()).SchedulerJobs())
}

func TestBuildServices_OpensheetRefusesToMountWithoutAnEncryptionKey(t *testing.T) {
	cfg := newWiringConfig(t)
	mountOpensheet(cfg)
	cfg.Security.EncryptionKey = "  "
	_, err := buildServices(cfg, newWiringKernel(t, cfg), capabilities.Capabilities{})
	require.ErrorContains(t, err, "security.encryptionKey")
	require.ErrorContains(t, err, "(set YASAKU_SECURITY_ENCRYPTION_KEY to 32 bytes hex or base64, or unset YASAKU_OPENSHEET_BASE_URL)")
}

func TestBuildServices_OpensheetRefusesPlainHTTPOnAPublicHost(t *testing.T) {
	const hint = "(use https://, or set YASAKU_OPENSHEET_ALLOW_PRIVATE_HOSTS=true on a private network)"
	tests := []struct {
		base, want string
	}{
		{"http://opensheet.example.com", "boot: opensheet.baseURL http://opensheet.example.com must be https " + hint},
		{"ftp://opensheet.example.com", "boot: opensheet.baseURL ftp://opensheet.example.com must be https " + hint},
		{"http://ops:hunter2@opensheet.example.com", "boot: opensheet.baseURL http://ops:xxxxx@opensheet.example.com must be https " + hint},
		{"://bad", "boot: opensheet.baseURL is not a valid URL (set YASAKU_OPENSHEET_BASE_URL to an https:// URL, or unset it)"},
		{"https://", "boot: opensheet.baseURL is not a valid URL (set YASAKU_OPENSHEET_BASE_URL to an https:// URL, or unset it)"},
		{"https://10.0.0.5", "boot: opensheet.baseURL https://10.0.0.5 is refused (use a public host, or set YASAKU_OPENSHEET_ALLOW_PRIVATE_HOSTS=true on a private network): " +
			`opensheet: BaseURL "https://10.0.0.5" is a private host; set AllowPrivateHosts to allow it`},
	}
	for _, tt := range tests {
		t.Run(tt.base, func(t *testing.T) {
			cfg := newWiringConfig(t)
			mountOpensheet(cfg)
			cfg.Opensheet = config.OpensheetConfig{BaseURL: tt.base}
			_, err := buildServices(cfg, newWiringKernel(t, cfg), capabilities.Capabilities{})
			require.EqualError(t, err, tt.want)
			require.NotContains(t, err.Error(), "hunter2")
		})
	}
	t.Run("https on a public host", func(t *testing.T) {
		cfg := newWiringConfig(t)
		mountOpensheet(cfg)
		cfg.Opensheet = config.OpensheetConfig{BaseURL: "  https://opensheet.example.com  "}
		svcs, err := buildServices(cfg, newWiringKernel(t, cfg), capabilities.Capabilities{})
		require.NoError(t, err)
		require.NotNil(t, svcs.Opensheet)
	})
	t.Run("a private https host on a private network", func(t *testing.T) {
		cfg := newWiringConfig(t)
		mountOpensheet(cfg)
		cfg.Opensheet = config.OpensheetConfig{BaseURL: "https://10.0.0.5", AllowPrivateHosts: true}
		_, err := buildServices(cfg, newWiringKernel(t, cfg), capabilities.Capabilities{})
		require.NoError(t, err)
	})
}

func TestBuildServices_OpensheetMounted(t *testing.T) {
	cfg := newWiringConfig(t)
	mountOpensheet(cfg)
	k := newWiringKernel(t, cfg)
	key, err := sealer.ParseKey(testEncryptionKey)
	require.NoError(t, err)
	k.Sealer, err = sealer.New(key)
	require.NoError(t, err)
	svcs, err := buildServices(cfg, k, capabilities.Capabilities{})
	require.NoError(t, err)
	require.NotNil(t, svcs.Opensheet)
	require.NotNil(t, svcs.OpensheetSync)
	require.NotNil(t, svcs.OpensheetMirror)
	require.NotEmpty(t, opensheetsync.NewConsumer(svcs.OpensheetSync).ConsumerHandlers())
	require.NotEmpty(t, opensheetsync.NewScheduler(svcs.OpensheetMirror, discardLogger()).SchedulerJobs())
}

func TestMirrorEntityNames_AreOpensheetEntities(t *testing.T) {
	for _, name := range []string{
		transaction.MirrorTransaction, transaction.MirrorWallet,
		wallet.MirrorWallet, wallet.MirrorTransaction, category.MirrorCategory,
	} {
		_, ok := opensheetsync.ParseEntity(name)
		require.True(t, ok, name)
	}
}

func TestBootServer_OpensheetMountsOnlyWithABaseURL(t *testing.T) {
	off, err := BootServer(t.Context(), schedulerBootCfg(t))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, off.Close()) })
	for _, r := range off.Routes {
		require.NotContains(t, r, "/opensheet")
	}
	for _, p := range off.API.MountedProcedures() {
		require.NotContains(t, p, "OpensheetService")
	}
	for _, j := range off.Scheduler.Jobs() {
		require.NotEqual(t, "opensheet-reconcile", j.Name)
	}

	cfg := schedulerBootCfg(t)
	mountOpensheet(cfg)
	on, err := BootServer(t.Context(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, on.Close()) })
	require.Contains(t, on.Routes, "GET /orgs/{org}/projects/{project}/opensheet")
	require.True(t, slices.ContainsFunc(on.API.MountedProcedures(), func(p string) bool { return strings.Contains(p, "OpensheetService") }))
	names := make([]string, 0)
	for _, j := range on.Scheduler.Jobs() {
		names = append(names, j.Name)
	}
	require.Contains(t, names, "opensheet-reconcile")
}

func TestBootServer_OpensheetRefusesToBootWithoutAnEncryptionKey(t *testing.T) {
	cfg := schedulerBootCfg(t)
	mountOpensheet(cfg)
	cfg.Security.EncryptionKey = ""
	_, err := BootServer(t.Context(), cfg)
	require.ErrorContains(t, err, "security.encryptionKey")
}

// NOTE: the yasaku counterpart of the skipped template test TestBootServer_ProducerOnlyStillDeclaresJobs.
func TestBootServer_ProducerOnlySubmitsTheOpensheetJob(t *testing.T) {
	ns := startNATSServer(t)
	cfg := queueBootCfg(t, ns.ClientURL())
	mountOpensheet(cfg)
	srv, err := BootServer(t.Context(), cfg, WithConsumer(false))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, srv.Close()) })
	require.Nil(t, srv.Consumer)

	ctx := t.Context()
	owner, err := srv.Users.Create(ctx, user.CreateRequest{Email: "q-owner@example.com", Name: "Q", Source: user.SourceGenesis})
	require.NoError(t, err)
	o, err := srv.Orgs.BootstrapSingleton(ctx, "q-org", "Q Org", owner.ID)
	require.NoError(t, err)
	orgCtx := tenant.Into(ctx, tenant.Context{OrgID: o.ID, UserID: owner.ID})
	p, err := srv.Projects.Create(orgCtx, o.ID, "q-project", "Q Project")
	require.NoError(t, err)
	projCtx := tenant.WithProject(orgCtx, p.ID)

	l := opensheetsync.NewLink(o.ID, p.ID, owner.ID, uuid.Nil, time.Now())
	l.Configure(opensheetsync.Settings{OSOrg: "acme", OSProject: "home", Sheets: opensheetsync.DefaultSheetSlugs()}, []byte("sealed"), "", time.Now())
	require.NoError(t, l.Enable(time.Now()))
	require.NoError(t, opensheetsync.NewStore(cfg.DB, srv.Platform.Pool, srv.Platform.PgConn).SaveLink(projCtx, l))

	pctx := session.PrincipalInto(projCtx, session.Principal{UserID: owner.ID, ActiveOrgID: o.ID, ActiveProjectID: p.ID})
	target := &yasakuv1.Target{Org: o.Slug, Project: p.Slug}
	_, err = srv.API.CategorySvc.CreateCategory(pctx, connect.NewRequest(&yasakuv1.CreateCategoryRequest{
		Target: target, Name: "Food", Kind: "expense", Confirm: true,
	}))
	require.NoError(t, err)
	require.Equal(t, uint64(1), streamMsgs(t, ns.ClientURL(), "WORK"), "a category write in a linked project submits one opensheet.sync job")

	w, err := srv.API.WalletSvc.CreateWallet(pctx, connect.NewRequest(&yasakuv1.CreateWalletRequest{
		Target: target, Name: "Cash", Kind: "cash", Currency: "IDR", Confirm: true,
	}))
	require.NoError(t, err)
	require.Equal(t, uint64(2), streamMsgs(t, ns.ClientURL(), "WORK"), "a wallet write in a linked project submits one opensheet.sync job")

	_, err = srv.API.TransactionSvc.RecordExpense(pctx, connect.NewRequest(&yasakuv1.RecordExpenseRequest{
		Target: target, Wallet: w.Msg.GetResult().GetId(), Amount: &yasakuv1.Money{Amount: "1000", Currency: "IDR"}, Confirm: true,
	}))
	require.NoError(t, err)
	require.Equal(t, uint64(3), streamMsgs(t, ns.ClientURL(), "WORK"), "a transaction write in a linked project submits one opensheet.sync job")
}

// NOTE: queued, one reconcile tick sends one page per project; inline it would send two.
func TestBuildServices_QueuedReconcileSendsOnePagePerProject(t *testing.T) {
	ns := startNATSServer(t)
	cfg := newWiringConfig(t)
	cfg.Queue = config.QueueConfig{Enabled: true, URL: ns.ClientURL(), ConnectTimeout: 5 * time.Second}
	mountOpensheet(cfg)
	k := newWiringKernel(t, cfg)
	q, err := openQueue(t.Context(), cfg, k, k.Reporter, discardLogger())
	require.NoError(t, err)
	t.Cleanup(func() { _ = q.Close() })
	k.Queue = q
	svcs, err := buildServices(cfg, k, capabilities.Capabilities{})
	require.NoError(t, err)
	_, _, err = declareQueue(q, svcs, &onboardingGate{}, discardLogger())
	require.NoError(t, err)

	ctx := t.Context()
	owner, err := svcs.Users.Create(ctx, user.CreateRequest{Email: "r-owner@example.com", Name: "R", Source: user.SourceGenesis})
	require.NoError(t, err)
	o, err := svcs.Orgs.BootstrapSingleton(ctx, "r-org", "R Org", owner.ID)
	require.NoError(t, err)
	orgCtx := tenant.Into(ctx, tenant.Context{OrgID: o.ID, UserID: owner.ID})
	p, err := svcs.Projects.Create(orgCtx, o.ID, "r-project", "R Project")
	require.NoError(t, err)
	projCtx := tenant.WithProject(orgCtx, p.ID)

	store := opensheetsync.NewStore(cfg.DB, k.Pool, k.PgConn)
	l := opensheetsync.NewLink(o.ID, p.ID, owner.ID, uuid.Nil, time.Now())
	l.Configure(opensheetsync.Settings{OSOrg: "acme", OSProject: "home", Sheets: opensheetsync.DefaultSheetSlugs()}, []byte("sealed"), "", time.Now())
	require.NoError(t, l.Enable(time.Now()))
	require.NoError(t, store.SaveLink(projCtx, l))
	refs := make([]opensheetsync.Ref, 0, 60)
	for range 60 {
		refs = append(refs, opensheetsync.Ref{Entity: opensheetsync.EntityWallet, ID: uuid.New()})
	}
	require.NoError(t, store.Mark(projCtx, o.ID, p.ID, refs, time.Now().Add(-2*time.Minute)))

	sent, err := svcs.OpensheetMirror.Reconcile(orgCtx)
	require.NoError(t, err)
	require.Equal(t, 50, sent)
	require.Equal(t, uint64(1), streamMsgs(t, ns.ClientURL(), "WORK"), "queued: one page per project per tick")
}

func TestNewOpensheetWiring_TrimsTheBaseURLOnceForValidationAndTheEndpoint(t *testing.T) {
	cfg := newWiringConfig(t)
	mountOpensheet(cfg)
	cfg.Opensheet = config.OpensheetConfig{BaseURL: "  https://opensheet.example.com  "}
	w, err := newOpensheetWiring(cfg, newWiringKernel(t, cfg), nil, discardLogger())
	require.NoError(t, err)
	require.Equal(t, "https://opensheet.example.com", w.endpoint.BaseURL)
	require.NoError(t, w.endpoint.Validate())
}
