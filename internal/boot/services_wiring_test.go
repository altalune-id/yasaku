package boot

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/platform"
	"altalune.id/yasaku/internal/platform/capabilities"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/mailer"
)

func newWiringConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg := config.Defaults()
	cfg.Mode = config.ModeSelfhosted
	cfg.DB.Driver = db.DriverSQLite
	cfg.DB.DSN = filepath.Join(t.TempDir(), "wiring.db")
	cfg.DB.AutoMigrate = true
	return cfg
}

func newWiringKernel(t *testing.T, cfg *config.Config) *platform.Kernel {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	pool, pgConn, err := openDBAndMigrate(context.Background(), cfg, log)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })

	mail, err := mailer.New(mailerConfig(cfg.Mail))
	require.NoError(t, err)

	return &platform.Kernel{
		Pool:     pool,
		PgConn:   pgConn,
		Log:      log,
		Reporter: apperror.NewReporter(log, false),
		Mail:     mail,
	}
}

func TestBuildServices_SQLite_WiresEveryYasakuField(t *testing.T) {
	cfg := newWiringConfig(t)
	svcs, err := buildServices(cfg, newWiringKernel(t, cfg), capabilities.Capabilities{})
	require.NoError(t, err)

	stores := map[string]any{
		"LedgerStore":      svcs.LedgerStore,
		"WalletStore":      svcs.WalletStore,
		"TxCategoryStore":  svcs.TxCategoryStore,
		"PeriodStore":      svcs.PeriodStore,
		"TransactionStore": svcs.TransactionStore,
	}
	for name, store := range stores {
		require.NotNil(t, store, name)
	}

	require.NotNil(t, svcs.Ledgers, "Ledgers")
	require.NotNil(t, svcs.Wallets, "Wallets")
	require.NotNil(t, svcs.TxCategories, "TxCategories")
	require.NotNil(t, svcs.Periods, "Periods")
	require.NotNil(t, svcs.Transactions, "Transactions")
	require.NotNil(t, svcs.Reports, "Reports")
	require.NotNil(t, svcs.WalletOpen, "WalletOpen")

	require.NotNil(t, svcs.Auth, "the template services must still be wired")
	require.NotNil(t, svcs.Categories, "the blog categories service must still be wired")
}

func TestUnitOfWork_SQLite_IsARealTransaction(t *testing.T) {
	cfg := newWiringConfig(t)
	k := newWiringKernel(t, cfg)

	uow := unitOfWork(cfg.DB, k.Pool, k.PgConn)

	var sawTx bool
	err := uow(context.Background(), func(ctx context.Context) error {
		_, sawTx = db.CurrentTx(ctx)
		return nil
	})
	require.NoError(t, err)
	require.True(t, sawTx, "the unit of work must expose a real transaction on the context")
}
