//go:build integration

package boot

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/opensheetsync"
	"altalune.id/yasaku/internal/platform"
	"altalune.id/yasaku/internal/platform/capabilities"
	"altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/platform/queue"
	"altalune.id/yasaku/internal/platform/sealer"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/testutil/pgtest"
	"altalune.id/yasaku/internal/transaction"
	"altalune.id/yasaku/internal/wallet"
	"altalune.id/yasaku/mailer"
	"altalune.id/yasaku/money"
	"altalune.id/yasaku/schema"
)

type pgServices struct {
	svcs  *Services
	db    *sql.DB
	q     func(string) string
	ctx   context.Context
	tc    tenant.Context
	store opensheetsync.Store
}

// NOTE: AllowBypassRLS, as the other Postgres fixtures do; these tests are about locks, not about RLS.
func newPgServices(t *testing.T, mounted bool) pgServices {
	t.Helper()
	h := pgtest.New(t)
	sqlDB := h.OpenDB(t)
	cfg := newWiringConfig(t)
	cfg.DB.Driver = db.DriverPostgres
	cfg.DB.DSN = h.DSN
	cfg.DB.Schema = h.Schema
	cfg.DB.AllowBypassRLS = true
	require.NoError(t, schema.MigrateUp(t.Context(), sqlDB, cfg))
	mail, err := mailer.New(mailerConfig(cfg.Mail))
	require.NoError(t, err)
	log := discardLogger()
	k := &platform.Kernel{
		Pool: db.Pool{W: sqlDB, R: sqlDB}, PgConn: tenant.NewPgConn(sqlDB),
		Log: log, Reporter: apperror.NewReporter(log, false), Mail: mail,
		// NOTE: a disabled queue with nothing bound: a kick reports an undeclared job and returns, which keeps these tests about the write path.
		Queue: queue.Disabled(log),
	}
	if mounted {
		mountOpensheet(cfg)
		key, kErr := sealer.ParseKey(testEncryptionKey)
		require.NoError(t, kErr)
		k.Sealer, err = sealer.New(key)
		require.NoError(t, err)
	}
	svcs, err := buildServices(cfg, k, capabilities.Capabilities{})
	require.NoError(t, err)

	q := func(table string) string { return h.Schema + "." + cfg.DB.TablePrefix + table }
	userID, orgID, projID := uuid.New(), uuid.New(), uuid.New()
	now := time.Now().UTC()
	_, err = sqlDB.Exec("INSERT INTO "+q("users")+" (id, email, name, avatar_url, is_admin, created_at, updated_at) VALUES ($1, $2, '', '', false, $3, $3)", userID, userID.String()+"@x.co", now)
	require.NoError(t, err)
	_, err = sqlDB.Exec("INSERT INTO "+q("orgs")+" (id, slug, name, created_by, created_at, updated_at) VALUES ($1, $2, 'Acme', $3, $4, $4)", orgID, orgID.String()[:8], userID, now)
	require.NoError(t, err)
	_, err = sqlDB.Exec("INSERT INTO "+q("projects")+" (id, org_id, slug, name, created_by, created_at, updated_at) VALUES ($1, $2, $3, 'Cash', $4, $5, $5)", projID, orgID, projID.String()[:8], userID, now)
	require.NoError(t, err)
	tc := tenant.Context{OrgID: orgID, ProjectID: projID, UserID: userID}
	return pgServices{
		svcs: svcs, db: sqlDB, q: q, ctx: tenant.Into(context.Background(), tc), tc: tc,
		store: opensheetsync.NewStore(cfg.DB, k.Pool, k.PgConn),
	}
}

func (p pgServices) wallet(t *testing.T, name string) uuid.UUID {
	t.Helper()
	w, err := p.svcs.Wallets.Create(p.ctx, wallet.Params{Name: name, Kind: wallet.KindCash, Currency: money.IDR})
	require.NoError(t, err)
	return w.ID
}

func (p pgServices) enableLink(t *testing.T) {
	t.Helper()
	l := opensheetsync.NewLink(p.tc.OrgID, p.tc.ProjectID, p.tc.UserID, uuid.Nil, time.Now())
	l.Configure(opensheetsync.Settings{OSOrg: "acme", OSProject: "home", Sheets: opensheetsync.DefaultSheetSlugs()}, []byte("sealed"), "", time.Now())
	require.NoError(t, l.Enable(time.Now()))
	require.NoError(t, p.store.SaveLink(p.ctx, l))
}

func (p pgServices) dirty(t *testing.T, entity string) int {
	t.Helper()
	var n int
	require.NoError(t, p.db.QueryRow("SELECT COUNT(*) FROM "+p.q("opensheet_sync_state")+" WHERE project_id = $1 AND entity = $2", p.tc.ProjectID, entity).Scan(&n))
	return n
}

func (p pgServices) expense(walletID uuid.UUID, minor int64) error {
	_, err := p.svcs.Transactions.Record(p.ctx, transaction.RecordInput{
		WalletID: walletID, Kind: transaction.KindExpense, Amount: money.New(minor, money.IDR), OccurredAt: time.Now(),
	})
	return err
}

// NOTE: Record runs in a tenant transaction and creates the first period inside it; a unique violation there would abort the transaction, which is why EnsureCurrent uses ON CONFLICT DO NOTHING.
func TestRecord_Postgres_TwoConcurrentRecordsShareTheFirstPeriod(t *testing.T) {
	p := newPgServices(t, false)
	w := p.wallet(t, "Cash")
	var g errgroup.Group
	for i := range 2 {
		g.Go(func() error { return p.expense(w, int64(1000*(i+1))) })
	}
	require.NoError(t, g.Wait(), "both Records succeed although they race to create the first period")
	var periods, withPeriod int
	require.NoError(t, p.db.QueryRow("SELECT COUNT(*) FROM "+p.q("periods")+" WHERE project_id = $1", p.tc.ProjectID).Scan(&periods))
	require.Equal(t, 1, periods)
	require.NoError(t, p.db.QueryRow("SELECT COUNT(*) FROM "+p.q("transactions")+" WHERE project_id = $1 AND period_id IS NOT NULL", p.tc.ProjectID).Scan(&withPeriod))
	require.Equal(t, 2, withPeriod)
}

// NOTE: a transfer marks the transaction and both wallets; without LockOrder an A-to-B and a B-to-A transfer lock the two wallet rows in opposite orders.
func TestMark_Postgres_OppositeTransfersNeverDeadlock(t *testing.T) {
	p := newPgServices(t, true)
	a, b := p.wallet(t, "A"), p.wallet(t, "B")
	p.enableLink(t)
	transfer := func(from, to uuid.UUID) error {
		_, err := p.svcs.Transactions.Record(p.ctx, transaction.RecordInput{
			WalletID: from, ToWalletID: &to, Kind: transaction.KindTransfer, Amount: money.New(100, money.IDR), OccurredAt: time.Now(),
		})
		return err
	}
	for range 30 {
		var g errgroup.Group
		g.Go(func() error { return transfer(a, b) })
		g.Go(func() error { return transfer(b, a) })
		require.NoError(t, g.Wait())
	}
	require.Equal(t, 60, p.dirty(t, "transaction"), "every transfer was marked, so the test exercised the state-row locks")
	require.Equal(t, 2, p.dirty(t, "wallet"))
}

// NOTE: a rename marks the wallet and cascades to its transactions, while a Revise marks one transaction and its wallet; both must lock the transaction rows before the wallet row.
func TestMark_Postgres_RenameAndReviseNeverDeadlock(t *testing.T) {
	p := newPgServices(t, true)
	a := p.wallet(t, "A")
	p.enableLink(t)
	ids := make([]uuid.UUID, 0, 5)
	for i := range 5 {
		tx, err := p.svcs.Transactions.Record(p.ctx, transaction.RecordInput{
			WalletID: a, Kind: transaction.KindExpense, Amount: money.New(int64(100*(i+1)), money.IDR), OccurredAt: time.Now(),
		})
		require.NoError(t, err)
		ids = append(ids, tx.ID)
	}
	for i := range 30 {
		note := fmt.Sprintf("round %d", i)
		var g errgroup.Group
		g.Go(func() error {
			_, err := p.svcs.Wallets.Rename(p.ctx, a, fmt.Sprintf("A%d", i))
			return err
		})
		for _, id := range ids {
			g.Go(func() error {
				_, err := p.svcs.Transactions.Revise(p.ctx, id, transaction.RevisePatch{Note: &note})
				return err
			})
		}
		require.NoError(t, g.Wait())
	}
	require.Equal(t, 5, p.dirty(t, "transaction"), "the transactions were marked, so the test exercised the state-row locks")
}
