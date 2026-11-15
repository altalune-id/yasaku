package opensheetsync_test

import (
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/opensheetsync"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/db"
	sqliteent "altalune.id/yasaku/internal/platform/db/entity/sqlite"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/testutil/fakes"
	"altalune.id/yasaku/money"
	"altalune.id/yasaku/schema"
)

func TestSQLite_TheResyncMigrationDirtiesEveryRowAndTheNextPassClearsIt(t *testing.T) {
	f, pool := newSQLiteFixture(t)
	now := time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	unexpected := apperror.NewReporter(log, false).Unexpected
	sl := newSealer(t)
	sheets := fakes.NewOpensheet(t, "acme", "home", goodKey)
	addContractSheets(sheets, true)
	src := &stubSource{txs: map[uuid.UUID]opensheetsync.TransactionFacts{}, wallet: map[uuid.UUID]opensheetsync.WalletFacts{}}
	syncer := opensheetsync.NewSyncer(f.store, log, unexpected, opensheetsync.SyncerDeps{
		Sealer: sl, Endpoint: opensheetsync.Endpoint{BaseURL: sheets.URL(), AllowPrivateHosts: true, Timeout: 2 * time.Second},
		Source: src, Now: func() time.Time { return now },
	})
	mirror := opensheetsync.NewMirror(f.store, &fakes.Queue{}, false, log, unexpected, func() time.Time { return now })

	tenants := []tenant.Context{f.tc, f.other}
	refs := map[uuid.UUID][]opensheetsync.Ref{}
	for _, tc := range tenants {
		ctx := tenant.Into(t.Context(), tc)
		sealed, err := opensheetsync.SealKey(sl, tc.OrgID, tc.ProjectID, goodKey)
		require.NoError(t, err)
		l := opensheetsync.NewLink(tc.OrgID, tc.ProjectID, tc.UserID, uuid.Nil, now)
		l.Configure(settings(""), sealed, "abcd", now)
		require.NoError(t, l.Enable(now))
		require.NoError(t, f.store.SaveLink(ctx, l))
		for range 2 {
			id := f.wallet(t, tc)
			src.wallet[id] = opensheetsync.WalletFacts{ID: id, Name: "w-" + id.String()[:8], Kind: "cash", Balance: money.New(12_345, money.IDR), UpdatedAt: now}
			r := ref(opensheetsync.EntityWallet, id)
			require.NoError(t, mirror.Mark(ctx, r))
			refs[tc.ProjectID] = append(refs[tc.ProjectID], r)
		}
		require.NoError(t, syncer.Sync(ctx, tc.ProjectID, refs[tc.ProjectID]))
	}
	pending := func(tc tenant.Context) int64 {
		t.Helper()
		b, err := f.store.Backlog(tenant.Into(t.Context(), tc), tc.OrgID, tc.ProjectID)
		require.NoError(t, err)
		return b.Pending
	}
	for _, tc := range tenants {
		require.Zero(t, pending(tc), "every row is synced before the migration")
	}

	cfg := config.Defaults()
	cfg.DB.Driver = db.DriverSQLite
	givenUp := refs[f.tc.ProjectID][0]
	_, err := pool.W.ExecContext(t.Context(), "UPDATE "+cfg.DB.TablePrefix+"opensheet_sync_state SET version = version + 1, attempts = ?, retry_after = ? WHERE entity_id = ?",
		opensheetsync.MaxRowAttempts, sqliteent.SQLiteTime(now.Add(time.Hour)), givenUp.ID.String())
	require.NoError(t, err)
	b, err := f.store.Backlog(tenant.Into(t.Context(), f.tc), f.tc.OrgID, f.tc.ProjectID)
	require.NoError(t, err)
	require.EqualValues(t, 1, b.GivenUp, "one row had given up before the migration")
	require.NoError(t, schema.MigrateDownTo(t.Context(), pool.W, cfg, 14))
	require.NoError(t, schema.MigrateUp(t.Context(), pool.W, cfg))
	for _, tc := range tenants {
		require.EqualValues(t, 2, pending(tc), "the migration marks every row of every org dirty, a given-up row included")
	}

	patches := sheets.CountRequests(http.MethodPatch, "yasaku-wallets")
	for _, tc := range tenants {
		require.NoError(t, syncer.Sync(tenant.Into(t.Context(), tc), tc.ProjectID, refs[tc.ProjectID]))
		require.Zero(t, pending(tc), "the next pass rewrites and settles every row")
	}
	require.Equal(t, patches+4, sheets.CountRequests(http.MethodPatch, "yasaku-wallets"), "each existing row is patched once, not created again")
}
