package boot

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/platform/capabilities"
	sqliteent "altalune.id/yasaku/internal/platform/db/entity/sqlite"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/transaction"
	"altalune.id/yasaku/internal/wallet"
	"altalune.id/yasaku/money"
)

func TestBuildServices_SQLite_ConcurrentRecordsThroughBootsPoolNeverFail(t *testing.T) {
	cfg := newWiringConfig(t)
	k := newWiringKernel(t, cfg)
	svcs, err := buildServices(cfg, k, capabilities.Capabilities{})
	require.NoError(t, err)

	tc := tenant.Context{OrgID: uuid.New(), ProjectID: uuid.New(), UserID: uuid.New()}
	prefix := cfg.DB.TablePrefix
	now := sqliteent.SQLiteTime(time.Now())
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{"INSERT INTO " + prefix + "users (id, email, name, avatar_url, is_admin, created_at, updated_at) VALUES (?, ?, '', '', 0, ?, ?)",
			[]any{tc.UserID.String(), tc.UserID.String() + "@x.co", now, now}},
		{"INSERT INTO " + prefix + "orgs (id, slug, name, created_by, created_at, updated_at) VALUES (?, ?, 'Org', ?, ?, ?)",
			[]any{tc.OrgID.String(), tc.OrgID.String()[:8], tc.UserID.String(), now, now}},
		{"INSERT INTO " + prefix + "projects (id, org_id, slug, name, created_by, created_at, updated_at) VALUES (?, ?, ?, 'Web', ?, ?, ?)",
			[]any{tc.ProjectID.String(), tc.OrgID.String(), tc.ProjectID.String()[:8], tc.UserID.String(), now, now}},
	} {
		_, err := k.Pool.W.ExecContext(t.Context(), q.sql, q.args...)
		require.NoError(t, err)
	}
	ctx := tenant.Into(context.Background(), tc)
	w, err := svcs.Wallets.Create(ctx, wallet.Params{Name: "Cash", Kind: wallet.KindCash, Currency: money.IDR})
	require.NoError(t, err)

	const n = 30
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			_, errs[i] = svcs.Transactions.Record(ctx, transaction.RecordInput{
				WalletID: w.ID, Kind: transaction.KindExpense, Amount: money.New(1_000, money.IDR), OccurredAt: time.Now(),
			})
		})
	}
	wg.Wait()
	for i, err := range errs {
		require.NoError(t, err, "Record %d through boot's pool and unit of work", i)
	}
}
