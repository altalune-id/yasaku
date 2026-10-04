package boot

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"

	yasakuv1 "altalune.id/yasaku/gen/go/yasaku/v1"
	"altalune.id/yasaku/internal/opensheetsync"
	"altalune.id/yasaku/internal/platform/config"
	sqliteent "altalune.id/yasaku/internal/platform/db/entity/sqlite"
	"altalune.id/yasaku/internal/platform/queue"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/testutil/fakes"
	"altalune.id/yasaku/internal/user"
)

const jsOpensheetKey = "osk_live_js_0123456789abcd"

type opensheetJS struct {
	t       *testing.T
	srv     *Server
	sheets  *fakes.Opensheet
	js      jetstream.JetStream
	ctx     context.Context
	target  *yasakuv1.Target
	project uuid.UUID
}

// NOTE: a full boot on a real JetStream server with the real consumer running; only opensheet is the fake.
func newOpensheetJS(t *testing.T) *opensheetJS {
	t.Helper()
	ns := startNATSServer(t)
	sheets := fakes.NewOpensheet(t, "os-acme", "home", jsOpensheetKey)
	cfg := queueBootCfg(t, ns.ClientURL())
	cfg.Opensheet = config.OpensheetConfig{BaseURL: sheets.URL(), AllowPrivateHosts: true}
	cfg.Security.EncryptionKey = testEncryptionKey
	srv, err := BootServer(t.Context(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, srv.Close()) })
	require.NotNil(t, srv.Consumer)
	runConsumer(t, srv.Consumer)

	nc, err := nats.Connect(ns.ClientURL())
	require.NoError(t, err)
	t.Cleanup(nc.Close)
	js, err := jetstream.New(nc)
	require.NoError(t, err)

	ctx := context.Background()
	owner, err := srv.Users.Create(ctx, user.CreateRequest{Email: "js-owner@example.com", Name: "JS", Source: user.SourceGenesis})
	require.NoError(t, err)
	o, err := srv.Orgs.BootstrapSingleton(ctx, "js-org", "JS Org", owner.ID)
	require.NoError(t, err)
	orgCtx := tenant.Into(ctx, tenant.Context{OrgID: o.ID, UserID: owner.ID})
	p, err := srv.Projects.Create(orgCtx, o.ID, "js-project", "JS Project")
	require.NoError(t, err)
	pctx := session.PrincipalInto(tenant.WithProject(orgCtx, p.ID), session.Principal{UserID: owner.ID, ActiveOrgID: o.ID, ActiveProjectID: p.ID})
	return &opensheetJS{t: t, srv: srv, sheets: sheets, js: js, ctx: pctx, target: &yasakuv1.Target{Org: o.Slug, Project: p.Slug}, project: p.ID}
}

func (e *opensheetJS) publish() {
	for _, tab := range opensheetsync.Contract() {
		e.sheets.AddSheet(tab.DefaultSlug, fakes.OpensheetSheet{Columns: tab.Columns, Writable: true})
	}
}

func (e *opensheetJS) save() {
	e.t.Helper()
	slugs := opensheetsync.DefaultSheetSlugs()
	_, err := e.srv.API.OpensheetSvc.SaveOpensheetLink(e.ctx, connect.NewRequest(&yasakuv1.SaveOpensheetLinkRequest{Target: e.target, Settings: &yasakuv1.OpensheetSettings{
		OsOrg: "os-acme", OsProject: "home", ApiKey: jsOpensheetKey,
		TransactionsSheet: slugs.Transactions, WalletsSheet: slugs.Wallets, CategoriesSheet: slugs.Categories,
	}}))
	require.NoError(e.t, err)
}

func (e *opensheetJS) setEnabled(on bool) error {
	_, err := e.srv.API.OpensheetSvc.SetOpensheetLinkEnabled(e.ctx, connect.NewRequest(&yasakuv1.SetOpensheetLinkEnabledRequest{Target: e.target, Enabled: on}))
	return err
}

func (e *opensheetJS) linkOn() {
	e.t.Helper()
	e.publish()
	e.save()
	require.NoError(e.t, e.setEnabled(true))
	e.drained()
}

func (e *opensheetJS) link() *yasakuv1.OpensheetLink {
	e.t.Helper()
	resp, err := e.srv.API.OpensheetSvc.GetOpensheetLink(e.ctx, connect.NewRequest(&yasakuv1.GetOpensheetLinkRequest{Target: e.target}))
	require.NoError(e.t, err)
	return resp.Msg.GetLink()
}

func (e *opensheetJS) wallet(name, opening string) *yasakuv1.Wallet {
	e.t.Helper()
	req := &yasakuv1.CreateWalletRequest{Target: e.target, Name: name, Kind: "bank", Currency: "IDR", Confirm: true}
	if opening != "" {
		req.OpeningBalance = &yasakuv1.Money{Amount: opening, Currency: "IDR"}
	}
	resp, err := e.srv.API.WalletSvc.CreateWallet(e.ctx, connect.NewRequest(req))
	require.NoError(e.t, err)
	require.NotNil(e.t, resp.Msg.GetResult())
	return resp.Msg.GetResult()
}

func (e *opensheetJS) msgs(stream string) uint64 {
	e.t.Helper()
	s, err := e.js.Stream(context.Background(), stream)
	require.NoError(e.t, err)
	info, err := s.Info(context.Background())
	require.NoError(e.t, err)
	return info.State.Msgs
}

// NOTE: WORK is a work queue, so it holds a job until its handler acks or terms it, and a follow-up is published before the ack; empty means every job submitted so far has run.
func (e *opensheetJS) drained() {
	e.t.Helper()
	require.Eventually(e.t, func() bool { return e.msgs("WORK") == 0 }, natsWait, natsTick, "every opensheet.sync job runs")
}

func (e *opensheetJS) row(slug, id string, ok func(row map[string]string) bool, msg string) {
	e.t.Helper()
	require.Eventually(e.t, func() bool {
		row, found := e.sheets.Row(slug, id)
		return found && ok(row)
	}, natsWait, natsTick, msg)
}

// NOTE: submits a job for one row without a new mark, as a redelivery or a duplicate kick would.
func (e *opensheetJS) resubmit(entity opensheetsync.Entity, id string) {
	e.t.Helper()
	payload := map[string]any{"project_id": e.project, "refs": []map[string]string{{"entity": string(entity), "id": id}}}
	require.NoError(e.t, e.srv.Platform.Queue.Submit(e.ctx, queue.Job{Name: "opensheet.sync", Version: 1}, payload))
}

func ageDirtyRows(t *testing.T, srv *Server) {
	t.Helper()
	_, err := srv.Platform.Pool.W.Exec("UPDATE "+srv.Cfg.DB.TablePrefix+"opensheet_sync_state SET updated_at = ? WHERE synced_version < version",
		sqliteent.SQLiteTime(time.Now().Add(-2*time.Minute)))
	require.NoError(t, err)
}

func TestOpensheetE2E_OnJetStream_EveryWriteReachesTheSheetThroughTheConsumer(t *testing.T) {
	t.Parallel()
	e := newOpensheetJS(t)
	e.linkOn()

	w := e.wallet("BCA", "100000")
	e.row("yasaku-wallets", w.GetId(), func(r map[string]string) bool { return r["balance"] == "100000" }, "a new wallet reaches the sheet")
	rec, err := e.srv.API.TransactionSvc.RecordExpense(e.ctx, connect.NewRequest(&yasakuv1.RecordExpenseRequest{
		Target: e.target, Wallet: w.GetId(), Amount: &yasakuv1.Money{Amount: "25000", Currency: "IDR"}, Note: "kopi", Confirm: true,
	}))
	require.NoError(t, err)
	tx := rec.Msg.GetResult()
	e.row("yasaku-transactions", tx.GetId(), func(r map[string]string) bool {
		return r["amount"] == "25000" && r["wallet"] == "BCA" && r["note"] == "kopi"
	}, "a recorded transaction reaches the sheet")
	e.row("yasaku-wallets", w.GetId(), func(r map[string]string) bool { return r["balance"] == "75000" }, "its wallet is re-pushed")

	e.drained()
	posts := e.sheets.CountRequests(http.MethodPost, "yasaku-transactions")
	requests := len(e.sheets.Requests())
	e.resubmit(opensheetsync.EntityTransaction, tx.GetId())
	e.resubmit(opensheetsync.EntityTransaction, tx.GetId())
	e.drained()
	require.Equal(t, posts, e.sheets.CountRequests(http.MethodPost, "yasaku-transactions"), "a duplicate job writes once")
	require.Len(t, e.sheets.Requests(), requests, "a clean row is not pushed again")

	fresh := e.wallet("Fresh", "")
	e.resubmit(opensheetsync.EntityWallet, fresh.GetId())
	e.resubmit(opensheetsync.EntityWallet, fresh.GetId())
	e.drained()
	_, ok := e.sheets.Row("yasaku-wallets", fresh.GetId())
	require.True(t, ok)
	require.Equal(t, 1, countRequests(e.sheets, http.MethodPost, "yasaku-wallets", fresh.GetId()), "three live jobs for one dirty row create it once")
	require.Len(t, e.sheets.Rows("yasaku-wallets"), 2)

	_, err = e.srv.API.WalletSvc.UpdateWallet(e.ctx, connect.NewRequest(&yasakuv1.UpdateWalletRequest{Target: e.target, Wallet: w.GetId(), Name: new("BCA Prioritas"), Confirm: true}))
	require.NoError(t, err)
	e.row("yasaku-wallets", w.GetId(), func(r map[string]string) bool { return r["name"] == "BCA Prioritas" }, "the renamed wallet reaches the sheet")
	e.drained()
	txRow, _ := e.sheets.Row("yasaku-transactions", tx.GetId())
	require.Equal(t, "BCA", txRow["wallet"], "the cascade marks the transaction; only the reconciler pushes it")
	ageDirtyRows(t, e.srv)
	require.NoError(t, e.srv.Scheduler.RunOnce(context.Background(), "opensheet-reconcile"))
	e.row("yasaku-transactions", tx.GetId(), func(r map[string]string) bool { return r["wallet"] == "BCA Prioritas" }, "the reconciler's job carries the rename")

	_, err = e.srv.API.TransactionSvc.DeleteTransaction(e.ctx, connect.NewRequest(&yasakuv1.DeleteTransactionRequest{Target: e.target, Id: tx.GetId(), Confirm: true}))
	require.NoError(t, err)
	e.row("yasaku-transactions", tx.GetId(), func(r map[string]string) bool { return r["deleted_at"] != "" }, "a deleted transaction is tombstoned")
	e.row("yasaku-wallets", w.GetId(), func(r map[string]string) bool { return r["balance"] == "100000" }, "the delete re-pushes the wallet")
	e.drained()
	l := e.link()
	require.Zero(t, l.GetPending())
	require.Empty(t, l.GetLastError())
	require.Zero(t, e.msgs("DLQ"))
}

func TestOpensheetE2E_OnJetStream_AFollowUpDrainsMoreThanOneBatch(t *testing.T) {
	t.Parallel()
	e := newOpensheetJS(t)
	e.publish()
	e.save()
	const n = 2*50 + 7
	for i := range n {
		e.wallet(fmt.Sprintf("W%03d", i), "")
	}
	e.drained()
	require.Empty(t, e.sheets.Rows("yasaku-wallets"), "nothing is mirrored while the switch is off")

	require.NoError(t, e.setEnabled(true))
	require.Eventually(t, func() bool { return len(e.sheets.Rows("yasaku-wallets")) == n }, 2*natsWait, natsTick,
		"the backfill kicks one batch and each full batch follows up with the next; the reconciler never runs here")
	e.drained()
	require.Equal(t, n, e.sheets.CountRequests(http.MethodPost, "yasaku-wallets"), "every row is created once")
	require.Zero(t, e.link().GetPending())
}

func TestOpensheetE2E_OnJetStream_ALinkRefusalTurnsTheLinkOffAfterDisableAfter(t *testing.T) {
	t.Parallel()
	e := newOpensheetJS(t)
	e.linkOn()
	e.sheets.SetToken("osk_revoked")

	for i := range opensheetsync.DisableAfter {
		e.wallet(fmt.Sprintf("W%d", i), "")
		require.Eventually(t, func() bool { return e.msgs("DLQ") == uint64(i+1) }, natsWait, natsTick, "a link refusal is dead-lettered at once")
		l := e.link()
		require.Contains(t, l.GetLastError(), "SHT001")
		require.Equal(t, i+1 < opensheetsync.DisableAfter, l.GetEnabled(), "refusal %d", i+1)
	}
	e.drained()
	l := e.link()
	require.True(t, l.GetAutoDisabled())
	require.EqualValues(t, opensheetsync.DisableAfter, l.GetPending(), "the rows wait for a fixed link")
	require.Zero(t, l.GetFailing(), "a link refusal never charges the rows")
	require.Empty(t, e.sheets.Rows("yasaku-wallets"))
}

func TestOpensheetE2E_OnJetStream_ARowRefusalBacksOffAndNeverTurnsTheLinkOff(t *testing.T) {
	t.Parallel()
	e := newOpensheetJS(t)
	e.linkOn()
	e.sheets.FailNext(http.MethodPatch, "yasaku-wallets", http.StatusBadRequest, "SHT016", 1)

	bad := e.wallet("BAD", "")
	e.drained()
	_, ok := e.sheets.Row("yasaku-wallets", bad.GetId())
	require.False(t, ok)
	l := e.link()
	require.True(t, l.GetEnabled())
	require.False(t, l.GetAutoDisabled())
	require.EqualValues(t, 1, l.GetFailing())
	require.EqualValues(t, 1, l.GetPending())
	require.Zero(t, e.msgs("DLQ"), "a row refusal is not a failed job")

	requests := len(e.sheets.Requests())
	e.resubmit(opensheetsync.EntityWallet, bad.GetId())
	e.drained()
	require.Len(t, e.sheets.Requests(), requests, "the refused row is held back by its backoff")

	good := e.wallet("GOOD", "")
	e.row("yasaku-wallets", good.GetId(), func(map[string]string) bool { return true }, "the link keeps syncing other rows")
	e.drained()
	l = e.link()
	require.True(t, l.GetEnabled())
	require.EqualValues(t, 1, l.GetFailing())
}

func TestOpensheetE2E_OnJetStream_AForbiddenSheetIsALinkRefusal(t *testing.T) {
	t.Parallel()
	e := newOpensheetJS(t)
	e.linkOn()
	e.sheets.FailNext(http.MethodPatch, "yasaku-wallets", http.StatusForbidden, "SHT009", 1)

	w := e.wallet("BCA", "")
	require.Eventually(t, func() bool { return e.msgs("DLQ") == 1 }, natsWait, natsTick, "a 403 is dead-lettered at once")
	e.drained()
	_, ok := e.sheets.Row("yasaku-wallets", w.GetId())
	require.False(t, ok)
	l := e.link()
	require.Contains(t, l.GetLastError(), "SHT009")
	require.True(t, l.GetEnabled(), "one refusal of DisableAfter")
	require.EqualValues(t, 1, l.GetPending())
	require.Zero(t, l.GetFailing(), "a link refusal never charges the row")
}

func countRequests(f *fakes.Opensheet, method, slug, id string) int {
	n := 0
	for _, r := range f.Requests() {
		if r.Method == method && r.Slug == slug && (r.ID == id || r.Body["id"] == id) {
			n++
		}
	}
	return n
}

// NOTE: CI runs this in both test jobs (neither passes -short), so each run pays the consumer's 10s first redelivery delay.
func TestOpensheetE2E_OnJetStream_ATransientFailureIsRedelivered(t *testing.T) {
	if testing.Short() {
		t.Skip("waits for the consumer's first 10s redelivery delay")
	}
	t.Parallel()
	e := newOpensheetJS(t)
	e.linkOn()
	e.sheets.FailNext(http.MethodPatch, "yasaku-wallets", http.StatusServiceUnavailable, "GEN503", 1)

	w := e.wallet("BCA", "")
	require.Eventually(t, func() bool {
		_, ok := e.sheets.Row("yasaku-wallets", w.GetId())
		return ok
	}, 20*time.Second, 100*time.Millisecond, "the nak'd job is redelivered and lands")
	e.drained()
	l := e.link()
	require.True(t, l.GetEnabled())
	require.Zero(t, l.GetFailing())
	require.Zero(t, e.msgs("DLQ"))
}
