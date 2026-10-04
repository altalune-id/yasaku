package transaction_test

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/transaction"
	"altalune.id/yasaku/money"
	"altalune.id/yasaku/schema"
)

func newFileSQLiteService(t *testing.T) (*transaction.Service, sqliteFixture) {
	t.Helper()
	cfg := config.Defaults()
	dbCfg := db.DBConfig{Driver: db.DriverSQLite, DSN: filepath.Join(t.TempDir(), "uow.db"), TablePrefix: cfg.DB.TablePrefix}
	pool, err := db.OpenPool(t.Context(), dbCfg, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })
	require.NoError(t, schema.MigrateUp(t.Context(), pool.W, cfg))

	prefix := cfg.DB.TablePrefix
	userID, orgID, projID := seedTxnTenant(t, pool.W, prefix)
	f := sqliteFixture{db: pool.W, prefix: prefix, tc: tenant.Context{OrgID: orgID, ProjectID: projID, UserID: userID}}
	f.walletA = seedWallet(t, pool.W, prefix, orgID, projID, "Cash", "IDR")
	f.walletB = seedWallet(t, pool.W, prefix, orgID, projID, "Bank", "IDR")
	f.period = seedPeriod(t, pool.W, prefix, orgID, projID, "August", "2026-08-01", "2026-08-31")
	f.store = transaction.NewStore(dbCfg, pool, nil)

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	wallets := transaction.WalletReaderFunc(func(ctx context.Context, _, _, id uuid.UUID) (transaction.WalletInfo, error) {
		tx, _ := db.CurrentTx(ctx)
		var cur string
		if err := tx.QueryRowContext(ctx, "SELECT currency FROM "+prefix+"wallets WHERE id = ?", id.String()).Scan(&cur); err != nil {
			return transaction.WalletInfo{}, err
		}
		return transaction.WalletInfo{ID: id, Currency: money.Currency(cur)}, nil
	})
	svc := transaction.NewService(f.store, log, apperror.NewReporter(log, false).Unexpected, wallets,
		transaction.CategoryReaderFunc(func(_ context.Context, _, _, id uuid.UUID) (transaction.CategoryInfo, error) {
			return transaction.CategoryInfo{ID: id, Kind: "expense"}, nil
		}),
		&periodStub{
			infos:     map[uuid.UUID]transaction.PeriodInfo{f.period: {ID: f.period}},
			byInstant: func(time.Time) (uuid.UUID, bool) { return f.period, true },
		},
		transaction.UnitOfWork(tenant.NewUnitOfWork(dbCfg, pool, nil)))
	return svc, f
}

func concurrently(n int, fn func(i int) error) []error {
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() { errs[i] = fn(i) })
	}
	wg.Wait()
	return errs
}

func TestSQLite_ConcurrentRecordReviseDeleteInRealUnitsOfWorkNeverFail(t *testing.T) {
	svc, f := newFileSQLiteService(t)
	ctx := f.ctx()
	at := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	const n = 30

	ids := make([]uuid.UUID, n)
	for i, err := range concurrently(n, func(i int) error {
		tx, err := svc.Record(ctx, transaction.RecordInput{
			WalletID: f.walletA, Kind: transaction.KindExpense, Amount: money.New(1_000, money.IDR), OccurredAt: at,
		})
		if err == nil {
			ids[i] = tx.ID
		}
		return err
	}) {
		require.NoError(t, err, "Record %d", i)
	}
	for i, err := range concurrently(n, func(i int) error {
		_, err := svc.Revise(ctx, ids[i], transaction.RevisePatch{WalletID: &f.walletB})
		return err
	}) {
		require.NoError(t, err, "Revise %d", i)
	}
	for i, err := range concurrently(n, func(i int) error { return svc.Delete(ctx, ids[i]) }) {
		require.NoError(t, err, "Delete %d", i)
	}
}
