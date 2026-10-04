package controlplane_test

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	yasakuv1 "altalune.id/yasaku/gen/go/yasaku/v1"
	"altalune.id/yasaku/gen/go/yasaku/v1/yasakuv1connect"
	"altalune.id/yasaku/internal/opensheetsync"
	"altalune.id/yasaku/internal/platform/config"
	sqliteent "altalune.id/yasaku/internal/platform/db/entity/sqlite"
	"altalune.id/yasaku/internal/testutil/fakes"
)

const e2eOpensheetKey = "osk_live_e2e_0123456789abcd"

type opensheetE2E struct {
	*yasakuHarness
	sheets *fakes.Opensheet
	client yasakuv1connect.OpensheetServiceClient
}

// NOTE: the queue is off, so every kick runs the opensheet.sync handler inline in the request, against the fake server.
func newOpensheetE2E(t *testing.T) *opensheetE2E {
	t.Helper()
	sheets := fakes.NewOpensheet(t, "os-acme", "home", e2eOpensheetKey)
	cfg := newYasakuCfg(t)
	cfg.Opensheet = config.OpensheetConfig{BaseURL: sheets.URL(), AllowPrivateHosts: true}
	cfg.Security.EncryptionKey = strings.Repeat("cd", 32)
	cfg.Scheduler.Enabled = true
	h := newYasakuHarnessOn(t, cfg)
	return &opensheetE2E{yasakuHarness: h, sheets: sheets,
		client: yasakuv1connect.NewOpensheetServiceClient(http.DefaultClient, h.base(), h.opts()...)}
}

func (e *opensheetE2E) settings() *yasakuv1.OpensheetSettings {
	slugs := opensheetsync.DefaultSheetSlugs()
	return &yasakuv1.OpensheetSettings{OsOrg: "os-acme", OsProject: "home", ApiKey: e2eOpensheetKey,
		TransactionsSheet: slugs.Transactions, WalletsSheet: slugs.Wallets, CategoriesSheet: slugs.Categories}
}

func (e *opensheetE2E) publish() {
	for _, tab := range opensheetsync.Contract() {
		e.sheets.AddSheet(tab.DefaultSlug, fakes.OpensheetSheet{Columns: tab.Columns, Writable: true})
	}
}

func (e *opensheetE2E) target() *yasakuv1.Target {
	return &yasakuv1.Target{Org: e.org.Slug, Project: e.project.Slug}
}

func (e *opensheetE2E) test(ctx context.Context) *yasakuv1.TestOpensheetLinkResponse {
	e.t.Helper()
	resp, err := e.client.TestOpensheetLink(ctx, connect.NewRequest(&yasakuv1.TestOpensheetLinkRequest{Target: e.target(), Settings: e.settings()}))
	require.NoError(e.t, err)
	return resp.Msg
}

func (e *opensheetE2E) save(ctx context.Context) (*yasakuv1.OpensheetLink, error) {
	resp, err := e.client.SaveOpensheetLink(ctx, connect.NewRequest(&yasakuv1.SaveOpensheetLinkRequest{Target: e.target(), Settings: e.settings()}))
	if err != nil {
		return nil, err
	}
	return resp.Msg.GetLink(), nil
}

func (e *opensheetE2E) setEnabled(ctx context.Context, on bool) error {
	_, err := e.client.SetOpensheetLinkEnabled(ctx, connect.NewRequest(&yasakuv1.SetOpensheetLinkEnabledRequest{Target: e.target(), Enabled: on}))
	return err
}

func (e *opensheetE2E) link(ctx context.Context) *yasakuv1.OpensheetLink {
	e.t.Helper()
	resp, err := e.client.GetOpensheetLink(ctx, connect.NewRequest(&yasakuv1.GetOpensheetLinkRequest{Target: e.target()}))
	require.NoError(e.t, err)
	return resp.Msg.GetLink()
}

func (e *opensheetE2E) linkOn(ctx context.Context) {
	e.t.Helper()
	e.publish()
	_, err := e.save(ctx)
	require.NoError(e.t, err)
	require.NoError(e.t, e.setEnabled(ctx, true))
}

func (e *opensheetE2E) expense(ctx context.Context, wallet, category, note string) *yasakuv1.Transaction {
	e.t.Helper()
	rec, err := e.txClient().RecordExpense(ctx, connect.NewRequest(&yasakuv1.RecordExpenseRequest{
		Wallet: wallet, Category: category, Amount: &yasakuv1.Money{Amount: "25000"}, Note: note, Confirm: true,
	}))
	require.NoError(e.t, err)
	require.NotNil(e.t, rec.Msg.GetResult())
	return rec.Msg.GetResult()
}

// NOTE: the reconciler skips rows marked within its one-minute grace and the boot mirror reads the wall clock, so the test ages the dirty rows instead of waiting.
func (e *opensheetE2E) ageDirtyRows() {
	e.t.Helper()
	cfg := e.boot.Cfg.DB
	_, err := e.boot.Platform.Pool.W.Exec("UPDATE "+cfg.TablePrefix+"opensheet_sync_state SET updated_at = ? WHERE synced_version < version",
		sqliteent.SQLiteTime(time.Now().Add(-2*time.Minute)))
	require.NoError(e.t, err)
}

func (e *opensheetE2E) reconcile(ctx context.Context) {
	e.t.Helper()
	require.NoError(e.t, e.boot.Scheduler.RunOnce(ctx, "opensheet-reconcile"))
}

func TestOpensheetE2E_SetUpThenEveryWriteReachesTheSheet(t *testing.T) {
	e := newOpensheetE2E(t)
	ctx := context.Background()

	test := e.test(ctx)
	require.False(t, test.GetOk())
	require.Equal(t, "OSL004", test.GetChecks()[0].GetCode())
	_, err := e.save(ctx)
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err), "Save is refused while the Test fails")

	e.publish()
	saved, err := e.save(ctx)
	require.NoError(t, err)
	require.Equal(t, "abcd", saved.GetApiKeyHint())
	require.False(t, saved.GetEnabled())

	w := e.createWallet(ctx, "BCA", &yasakuv1.Money{Amount: "100000"})
	require.Empty(t, e.sheets.Rows("yasaku-wallets"), "nothing is mirrored while the switch is off")

	require.NoError(t, e.setEnabled(ctx, true))
	row, ok := e.sheets.Row("yasaku-wallets", w.GetId())
	require.True(t, ok, "enabling backfills what was written while off")
	require.Equal(t, "100000", row["balance"])
	require.Len(t, e.sheets.Rows("yasaku-transactions"), 1, "the backfill carries the opening transaction too")

	tx := e.expense(ctx, "BCA", "", "kopi")
	txRow, ok := e.sheets.Row("yasaku-transactions", tx.GetId())
	require.True(t, ok, "a recorded transaction reaches the sheet in the request")
	require.Equal(t, "25000", txRow["amount"])
	require.Equal(t, "BCA", txRow["wallet"])
	require.Equal(t, "kopi", txRow["note"])
	require.Empty(t, txRow["deleted_at"])
	row, _ = e.sheets.Row("yasaku-wallets", w.GetId())
	require.Equal(t, "75000", row["balance"], "recording a transaction re-pushes its wallet")

	_, err = e.txClient().DeleteTransaction(ctx, connect.NewRequest(&yasakuv1.DeleteTransactionRequest{Id: tx.GetId(), Confirm: true}))
	require.NoError(t, err)
	txRow, _ = e.sheets.Row("yasaku-transactions", tx.GetId())
	require.NotEmpty(t, txRow["deleted_at"], "a deleted transaction is tombstoned in the sheet")
	row, _ = e.sheets.Row("yasaku-wallets", w.GetId())
	require.Equal(t, "100000", row["balance"], "the delete re-pushes the wallet")

	l := e.link(ctx)
	require.Zero(t, l.GetPending())
	require.NotEmpty(t, l.GetLastSyncedAt())
	require.Empty(t, l.GetLastError())
}

func TestOpensheetE2E_ARenameReachesEveryTransactionThroughTheReconciler(t *testing.T) {
	e := newOpensheetE2E(t)
	ctx := context.Background()
	e.linkOn(ctx)
	w := e.createWallet(ctx, "BCA", nil)
	_, err := e.categoryClient().CreateCategory(ctx, connect.NewRequest(&yasakuv1.CreateCategoryRequest{Name: "Makan", Kind: "expense", Confirm: true}))
	require.NoError(t, err)
	first, second := e.expense(ctx, "BCA", "Makan", "kopi"), e.expense(ctx, "BCA", "Makan", "nasi")

	_, err = e.walletClient().UpdateWallet(ctx, connect.NewRequest(&yasakuv1.UpdateWalletRequest{Wallet: w.GetId(), Name: new("BCA Prioritas"), Confirm: true}))
	require.NoError(t, err)
	row, _ := e.sheets.Row("yasaku-wallets", w.GetId())
	require.Equal(t, "BCA Prioritas", row["name"], "the renamed wallet is pushed in the request")
	e.requireColumn(t, "wallet", "BCA", "the cascade marks the transactions; only the reconciler pushes them", first, second)
	require.EqualValues(t, 2, e.link(ctx).GetPending())
	e.reconcile(ctx)
	require.EqualValues(t, 2, e.link(ctx).GetPending(), "rows inside the grace are left to their own kick")
	e.ageDirtyRows()
	e.reconcile(ctx)
	e.requireColumn(t, "wallet", "BCA Prioritas", "the reconciler pushes the wallet rename", first, second)
	require.Zero(t, e.link(ctx).GetPending())

	_, err = e.categoryClient().RenameCategory(ctx, connect.NewRequest(&yasakuv1.RenameCategoryRequest{Category: "Makan", Name: "Makanan"}))
	require.NoError(t, err)
	e.requireColumn(t, "category", "Makan", "the cascade marks the transactions; only the reconciler pushes them", first, second)
	require.EqualValues(t, 2, e.link(ctx).GetPending())
	e.ageDirtyRows()
	e.reconcile(ctx)
	e.requireColumn(t, "category", "Makanan", "the reconciler pushes the category rename", first, second)
	require.Zero(t, e.link(ctx).GetPending())
}

func (e *opensheetE2E) requireColumn(t *testing.T, column, want, msg string, txs ...*yasakuv1.Transaction) {
	t.Helper()
	for _, tx := range txs {
		row, ok := e.sheets.Row("yasaku-transactions", tx.GetId())
		require.True(t, ok)
		require.Equal(t, want, row[column], msg)
	}
}

func TestOpensheetE2E_QueueOff_EnablePushesOneBatchAndTheReconcilerDrainsTwoPagesATick(t *testing.T) {
	e := newOpensheetE2E(t)
	ctx := context.Background()
	e.publish()
	_, err := e.save(ctx)
	require.NoError(t, err)
	const n = 157
	for i := range n {
		e.createWallet(ctx, fmt.Sprintf("W%03d", i), nil)
	}
	require.Empty(t, e.sheets.Rows("yasaku-wallets"))

	require.NoError(t, e.setEnabled(ctx, true))
	require.Equal(t, 50, len(e.sheets.Rows("yasaku-wallets")), "inline, the backfill pushes one batch and never chains")
	require.EqualValues(t, n-50, e.link(ctx).GetPending())

	e.ageDirtyRows()
	e.reconcile(ctx)
	require.Equal(t, 150, len(e.sheets.Rows("yasaku-wallets")), "an inline tick drains two pages per project")
	e.ageDirtyRows()
	e.reconcile(ctx)
	require.Equal(t, n, len(e.sheets.Rows("yasaku-wallets")))
	require.Zero(t, e.link(ctx).GetPending())
}

func TestOpensheetE2E_TheReconcilerDrainsWhatAnOutageLeftDirty(t *testing.T) {
	e := newOpensheetE2E(t)
	ctx := context.Background()
	e.linkOn(ctx)
	e.sheets.FailNext(http.MethodPatch, "yasaku-wallets", http.StatusServiceUnavailable, "GEN503", 1)
	w := e.createWallet(ctx, "BCA", nil)
	_, ok := e.sheets.Row("yasaku-wallets", w.GetId())
	require.False(t, ok, "opensheet was down during the request")
	l := e.link(ctx)
	require.EqualValues(t, 1, l.GetPending())
	require.Zero(t, l.GetFailing(), "a transient failure never counts as a refusal")
	require.True(t, l.GetEnabled())

	e.ageDirtyRows()
	e.reconcile(ctx)
	_, ok = e.sheets.Row("yasaku-wallets", w.GetId())
	require.True(t, ok, "the reconciler pushes it once opensheet is back")
	require.Zero(t, e.link(ctx).GetPending())
}

func TestOpensheetE2E_ABrokenSheetTurnsTheLinkOffAndWritesStillSucceed(t *testing.T) {
	e := newOpensheetE2E(t)
	ctx := context.Background()
	e.linkOn(ctx)

	e.sheets.SetToken("osk_revoked")
	for i := range opensheetsync.DisableAfter {
		e.createWallet(ctx, "W"+string(rune('A'+i)), nil)
	}
	l := e.link(ctx)
	require.False(t, l.GetEnabled())
	require.True(t, l.GetAutoDisabled())
	require.Contains(t, l.GetLastError(), "SHT001")
	require.EqualValues(t, opensheetsync.DisableAfter, l.GetPending(), "the rows wait for a fixed link")
	require.Empty(t, e.sheets.Rows("yasaku-wallets"))

	err := e.setEnabled(ctx, true)
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err), "OSL010: a new Test and Save first")
}

func TestOpensheetE2E_AnEmptyTabPassesAndTheFirstSyncCatchesAMissingColumn(t *testing.T) {
	e := newOpensheetE2E(t)
	ctx := context.Background()
	e.publish()
	tab, ok := opensheetsync.TabFor(opensheetsync.EntityTransaction)
	require.True(t, ok)
	short := slices.DeleteFunc(slices.Clone(tab.Columns), func(c string) bool { return c == "note" })
	e.sheets.AddSheet(tab.DefaultSlug, fakes.OpensheetSheet{Columns: short, Writable: true})

	test := e.test(ctx)
	require.True(t, test.GetOk(), "empty tabs pass: opensheet cannot show their columns yet")
	for _, c := range test.GetChecks() {
		require.True(t, c.GetColumnsDeferred(), c.GetSheet())
	}
	saved, err := e.save(ctx)
	require.NoError(t, err)
	require.True(t, saved.GetAwaitingFirstSync())
	require.NoError(t, e.setEnabled(ctx, true))

	w := e.createWallet(ctx, "BCA", nil)
	_, ok = e.sheets.Row("yasaku-wallets", w.GetId())
	require.True(t, ok, "a complete empty tab takes its first row")
	e.expense(ctx, "BCA", "", "kopi")
	require.Empty(t, e.sheets.Rows(tab.DefaultSlug))
	l := e.link(ctx)
	require.Contains(t, l.GetLastError(), "SHT014", "the first sync names the gap on the link")
	require.True(t, l.GetEnabled(), "one refusal; DisableAfter turns it off")
	require.Zero(t, l.GetFailing(), "an unknown column is the tab's fault, not the row's")

	e.sheets.AddSheet(tab.DefaultSlug, fakes.OpensheetSheet{Columns: short, Writable: true})
	e.sheets.SetHeaderColumns(true)
	test = e.test(ctx)
	require.False(t, test.GetOk())
	check := test.GetChecks()[0]
	require.Equal(t, tab.DefaultSlug, check.GetSheet())
	require.Equal(t, "OSL005", check.GetCode(), "once opensheet reports the header row, the Test catches it")
	require.Equal(t, []string{"note"}, check.GetMissing())
	require.False(t, check.GetColumnsDeferred())
}

func TestOpensheetE2E_ADeleteWhileTheLinkIsOffIsTombstonedWhenItIsTurnedBackOn(t *testing.T) {
	e := newOpensheetE2E(t)
	ctx := context.Background()
	e.linkOn(ctx)
	e.createWallet(ctx, "BCA", nil)
	tx := e.expense(ctx, "BCA", "", "kopi")
	txRow, ok := e.sheets.Row("yasaku-transactions", tx.GetId())
	require.True(t, ok)
	require.Empty(t, txRow["deleted_at"])

	require.NoError(t, e.setEnabled(ctx, false))
	_, err := e.txClient().DeleteTransaction(ctx, connect.NewRequest(&yasakuv1.DeleteTransactionRequest{Id: tx.GetId(), Confirm: true}))
	require.NoError(t, err)
	txRow, _ = e.sheets.Row("yasaku-transactions", tx.GetId())
	require.Empty(t, txRow["deleted_at"], "nothing is mirrored while the switch is off")

	require.NoError(t, e.setEnabled(ctx, true))
	txRow, ok = e.sheets.Row("yasaku-transactions", tx.GetId())
	require.True(t, ok)
	require.NotEmpty(t, txRow["deleted_at"], "the backfill tombstones a row deleted while the link was off")
	require.Zero(t, e.link(ctx).GetPending())
}
