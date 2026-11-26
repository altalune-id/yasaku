package boot

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/civil"
	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/category"
	"altalune.id/yasaku/internal/ledger"
	"altalune.id/yasaku/internal/limits"
	"altalune.id/yasaku/internal/opensheetsync"
	"altalune.id/yasaku/internal/period"
	"altalune.id/yasaku/internal/platform/audit"
	"altalune.id/yasaku/internal/platform/capabilities"
	"altalune.id/yasaku/internal/platform/queue"
	"altalune.id/yasaku/internal/platform/sealer"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/testutil/fakes"
	"altalune.id/yasaku/internal/transaction"
	"altalune.id/yasaku/internal/user"
	"altalune.id/yasaku/internal/wallet"
	"altalune.id/yasaku/money"
)

type wipeFixture struct {
	uow    tenant.UnitOfWork
	svcs   *Services
	ctx    context.Context
	tc     tenant.Context
	store  opensheetsync.Store
	grants opensheetsync.GrantStore
	issuer *fakeIssuer
	name   string
}

func newSQLiteWipeFixture(t *testing.T) wipeFixture {
	t.Helper()
	cfg := newWiringConfig(t)
	issuer := mountOpensheetGrants(t, cfg)
	k := newWiringKernel(t, cfg)
	k.Queue = queue.Disabled(k.Log)
	key, err := sealer.ParseKey(testEncryptionKey)
	require.NoError(t, err)
	k.Sealer, err = sealer.New(key)
	require.NoError(t, err)
	svcs, err := buildServices(cfg, k, capabilities.Capabilities{})
	require.NoError(t, err)

	ctx := t.Context()
	owner, err := svcs.Users.Create(ctx, user.CreateRequest{Email: "w-owner@example.com", Name: "W", Source: user.SourceGenesis})
	require.NoError(t, err)
	o, err := svcs.Orgs.BootstrapSingleton(ctx, "w-org", "W Org", owner.ID)
	require.NoError(t, err)
	orgCtx := fakes.Owner(ctx, tenant.Context{OrgID: o.ID, UserID: owner.ID})
	p, err := svcs.Projects.Create(orgCtx, o.ID, "w-project", "W Project")
	require.NoError(t, err)
	tc := tenant.Context{OrgID: o.ID, ProjectID: p.ID, UserID: owner.ID}
	return wipeFixture{
		uow: tenant.NewUnitOfWork(cfg.DB, k.Pool, k.PgConn), svcs: svcs, ctx: fakes.Owner(ctx, tc), tc: tc,
		store: opensheetsync.NewStore(cfg.DB, k.Pool, k.PgConn), grants: opensheetsync.NewGrantStore(cfg.DB, k.Pool, k.PgConn),
		issuer: issuer, name: p.Name,
	}
}

func TestWipe_ClosedPeriodsGoneAndProjectUsable(t *testing.T) {
	runWipeScenario(t, newSQLiteWipeFixture(t))
}

func runWipeScenario(t *testing.T, f wipeFixture) {
	t.Helper()
	ctx, s := f.ctx, f.svcs
	utc := "UTC"
	_, err := s.Ledgers.Update(ctx, ledger.Patch{Timezone: &utc})
	require.NoError(t, err)

	l := opensheetsync.NewLink(f.tc.OrgID, f.tc.ProjectID, f.tc.UserID, uuid.Nil, time.Now())
	l.Configure(opensheetsync.Settings{OSOrg: "acme", OSProject: "home", Sheets: opensheetsync.DefaultSheetSlugs()}, []byte("sealed"), "", time.Now())
	require.NoError(t, l.Enable(time.Now()))
	require.NoError(t, f.store.SaveLink(ctx, l))
	seedOpensheetGrant(t, f.grants, f.tc, "wipe-refresh")

	w, err := s.Wallets.Create(ctx, wallet.Params{Name: "Cash", Kind: wallet.KindCash, Currency: money.IDR})
	require.NoError(t, err)
	seeded, err := s.TxCategories.SeedDefaults(ctx)
	require.NoError(t, err)
	require.Positive(t, seeded)
	expense, err := s.TxCategories.List(ctx, category.ListOpts{Kind: category.KindExpense})
	require.NoError(t, err)
	extra, err := s.TxCategories.Create(ctx, "Hobby", category.KindExpense, "", "")
	require.NoError(t, err)
	_, err = s.TxCategories.Archive(ctx, extra.ID)
	require.NoError(t, err)

	cur, err := s.Periods.EnsureCurrent(ctx)
	require.NoError(t, err)
	_, err = s.Transactions.Record(ctx, transaction.RecordInput{
		WalletID: w.ID, Kind: transaction.KindExpense, Amount: money.New(5000, money.IDR), CategoryID: &expense[0].ID, OccurredAt: time.Now(),
	})
	require.NoError(t, err)
	_, err = s.Periods.Close(ctx, cur.ID, civil.DateOf(time.Now(), time.UTC), f.tc.UserID)
	require.NoError(t, err)

	_, err = s.Wipe.Run(ctx, "not "+f.name)
	require.True(t, ledger.IsWipeConfirmMismatchError(err))

	counts, err := s.Wipe.Run(ctx, f.name)
	require.NoError(t, err)
	require.Equal(t, int64(1), counts.Transactions)
	require.Equal(t, int64(1), counts.Wallets)
	require.GreaterOrEqual(t, counts.Periods, int64(1))
	require.Equal(t, int64(seeded+1), counts.Categories, "archived rows go too")

	_, err = s.Periods.ByID(ctx, cur.ID)
	require.True(t, period.IsNotFoundError(err), "the closed period is gone: %v", err)
	_, err = f.store.LinkByProject(ctx, f.tc.OrgID, f.tc.ProjectID)
	require.True(t, opensheetsync.IsLinkNotFoundError(err), "the opensheet link is gone: %v", err)
	_, err = f.grants.GrantByProject(ctx, f.tc.OrgID, f.tc.ProjectID)
	require.True(t, opensheetsync.IsGrantNotFoundError(err), "the opensheet grant is gone: %v", err)
	require.Equal(t, []string{"wipe-refresh"}, f.issuer.Revoked(), "the grant is revoked upstream after the commit")
	ws, err := s.Wallets.List(ctx, wallet.ListOpts{IncludeArchived: true})
	require.NoError(t, err)
	require.Empty(t, ws)
	txs, _, err := s.Transactions.List(ctx, transaction.ListOpts{})
	require.NoError(t, err)
	require.Empty(t, txs)
	st, err := s.Ledgers.Get(ctx)
	require.NoError(t, err)
	require.Equal(t, "UTC", st.Timezone, "ledger settings stay")

	cats, err := s.TxCategories.List(ctx, category.ListOpts{IncludeArchived: true})
	require.NoError(t, err)
	require.Len(t, cats, seeded, "the defaults are back and the archived extra is not")
	now, err := s.Periods.Current(ctx)
	require.NoError(t, err)
	require.Nil(t, now.ClosedAt)
	fresh, err := s.Wallets.Create(ctx, wallet.Params{Name: "Cash", Kind: wallet.KindCash, Currency: money.IDR})
	require.NoError(t, err)
	_, err = s.Transactions.Record(ctx, transaction.RecordInput{
		WalletID: fresh.ID, Kind: transaction.KindExpense, Amount: money.New(1000, money.IDR), CategoryID: &cats[0].ID, OccurredAt: time.Now(),
	})
	require.NoError(t, err, "the wiped project takes new entries")

	entries, err := s.Audit.List(ctx, audit.Filter{})
	require.NoError(t, err)
	i := slices.IndexFunc(entries, func(e audit.Entry) bool { return e.Action == "project.wiped" })
	require.GreaterOrEqual(t, i, 0)
	require.Equal(t, f.tc.ProjectID, entries[i].ProjectID)
	require.Equal(t, "1", entries[i].Detail["transactions"])
}

type failingAudit struct{}

func (failingAudit) Record(context.Context, audit.Event) error { return errors.New("audit down") }

func TestWipe_FailedTransactionKeepsTheLink(t *testing.T) {
	f := newSQLiteWipeFixture(t)
	s := f.svcs
	l := opensheetsync.NewLink(f.tc.OrgID, f.tc.ProjectID, f.tc.UserID, uuid.Nil, time.Now())
	l.Configure(opensheetsync.Settings{OSOrg: "acme", OSProject: "home", Sheets: opensheetsync.DefaultSheetSlugs()}, []byte("sealed"), "", time.Now())
	require.NoError(t, l.Enable(time.Now()))
	require.NoError(t, f.store.SaveLink(f.ctx, l))
	_, err := s.Wallets.Create(f.ctx, wallet.Params{Name: "Cash", Kind: wallet.KindCash, Currency: money.IDR})
	require.NoError(t, err)

	wf := ledger.NewWipeWorkflow(ledger.WipeDeps{
		Projects: projectNamer{store: s.ProjectStore}, Opensheet: opensheetDisconnector(s.Opensheet), Limits: noLock{},
		Transactions: s.TransactionStore, Periods: s.PeriodStore, Wallets: s.WalletStore, Categories: s.TxCategoryStore,
		Seeder: s.TxCategories, Current: currentPeriod{periods: s.Periods}, Audit: failingAudit{}, UnitOfWork: f.uow,
	}, discardLogger(), apperror.NewReporter(discardLogger(), false).Unexpected)
	_, err = wf.Run(f.ctx, f.name)
	require.Error(t, err)

	got, err := f.store.LinkByProject(f.ctx, f.tc.OrgID, f.tc.ProjectID)
	require.NoError(t, err, "a failed wipe rolls the unlink back")
	require.Equal(t, l.ID, got.ID)
	ws, err := s.Wallets.List(f.ctx, wallet.ListOpts{})
	require.NoError(t, err)
	require.Len(t, ws, 1, "a failed wipe rolls the deletes back")
}

type noLock struct{}

func (noLock) Lock(context.Context, ...limits.Subject) error { return nil }
