package opensheetsync_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/opensheetsync"
	"altalune.id/yasaku/internal/platform/queue"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/testutil/fakes"
	"altalune.id/yasaku/money"
)

// NOTE: handlerQueue runs each job through the real consumer handler in the caller, as the inline submitter does, and keeps every run's error.
type handlerQueue struct {
	handle func(context.Context, queue.Message) error
	mu     sync.Mutex
	runs   []error
}

func (q *handlerQueue) Submit(ctx context.Context, j queue.Job, data any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	err = q.handle(ctx, queue.Message{ID: uuid.New(), Name: j.Name, Version: j.Version, Data: raw, NumDelivered: 1})
	q.mu.Lock()
	q.runs = append(q.runs, err)
	q.mu.Unlock()
	return err
}

func (q *handlerQueue) Runs() []error {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]error(nil), q.runs...)
}

// NOTE: runReconcileScenario proves, on a real store, that rows an opensheet outage or a lost kick left dirty are picked up by the reconciler after its grace, and that a duplicate job writes once.
func runReconcileScenario(t *testing.T, f storeFixture) {
	t.Helper()
	ctx := tenant.Into(context.Background(), f.tc)
	now := time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	unexpected := apperror.NewReporter(log, false).Unexpected
	sl := newSealer(t)
	sheets := fakes.NewOpensheet(t, "acme", "home", goodKey)
	addContractSheets(sheets, true)

	sealed, err := opensheetsync.SealKey(sl, f.tc.OrgID, f.tc.ProjectID, goodKey)
	require.NoError(t, err)
	l := opensheetsync.NewLink(f.tc.OrgID, f.tc.ProjectID, f.tc.UserID, uuid.Nil, now)
	l.Configure(settings(""), sealed, "abcd", now)
	require.NoError(t, l.Enable(now))
	require.NoError(t, f.store.SaveLink(ctx, l))

	down, lost := f.wallet(t, f.tc), f.wallet(t, f.tc)
	src := &stubSource{txs: map[uuid.UUID]opensheetsync.TransactionFacts{}, wallet: map[uuid.UUID]opensheetsync.WalletFacts{
		down: {ID: down, Name: "BCA", Kind: "bank", Balance: money.New(0, money.IDR), UpdatedAt: now},
		lost: {ID: lost, Name: "Cash", Kind: "cash", Balance: money.New(0, money.IDR), UpdatedAt: now},
	}}
	syncer := opensheetsync.NewSyncer(f.store, log, unexpected, opensheetsync.SyncerDeps{
		Sealer: sl, Endpoint: opensheetsync.Endpoint{BaseURL: sheets.URL(), AllowPrivateHosts: true, Timeout: 2 * time.Second},
		Source: src, Now: clock,
	})
	inline := &handlerQueue{handle: opensheetsync.NewConsumer(syncer).ConsumerHandlers()[0].Handle}
	mirror := opensheetsync.NewMirror(f.store, inline, false, log, unexpected, clock)

	sheets.FailNext(http.MethodPatch, "yasaku-wallets", http.StatusServiceUnavailable, "GEN503", 1)
	d := ref(opensheetsync.EntityWallet, down)
	require.NoError(t, mirror.Mark(ctx, d))
	mirror.Kick(ctx, d)
	require.Len(t, inline.Runs(), 1)
	require.Error(t, inline.Runs()[0], "opensheet is down: the job asks for a retry")
	require.False(t, queue.IsPermanentError(inline.Runs()[0]), "an outage is never permanent")

	dropped := &fakes.Queue{Err: errors.New("nats unreachable")}
	w := ref(opensheetsync.EntityWallet, lost)
	require.NoError(t, opensheetsync.NewMirror(f.store, dropped, false, log, unexpected, clock).Mark(ctx, w))
	opensheetsync.NewMirror(f.store, dropped, false, log, unexpected, clock).Kick(ctx, w)
	require.Len(t, dropped.Recorded(), 1, "the kick was tried and lost")
	require.Empty(t, sheets.Rows("yasaku-wallets"))
	b, err := f.store.Backlog(ctx, f.tc.OrgID, f.tc.ProjectID)
	require.NoError(t, err)
	require.EqualValues(t, 2, b.Pending)
	require.Zero(t, b.Failing, "neither row was refused")

	orgCtx := tenant.Into(context.Background(), tenant.Context{OrgID: f.tc.OrgID})
	n, err := mirror.Reconcile(orgCtx)
	require.NoError(t, err)
	require.Zero(t, n, "rows marked under a minute ago are left to their own kick")

	now = now.Add(2 * time.Minute)
	n, err = mirror.Reconcile(orgCtx)
	require.NoError(t, err)
	require.Equal(t, 2, n)
	require.Len(t, inline.Runs(), 2, "one job carries both rows")
	require.NoError(t, inline.Runs()[1])
	for _, id := range []uuid.UUID{down, lost} {
		_, ok := sheets.Row("yasaku-wallets", id.String())
		require.True(t, ok, "the reconciler pushed %s once opensheet was back", id)
	}
	b, err = f.store.Backlog(ctx, f.tc.OrgID, f.tc.ProjectID)
	require.NoError(t, err)
	require.Zero(t, b.Pending)

	posts := sheets.CountRequests(http.MethodPost, "yasaku-wallets")
	require.Equal(t, 2, posts)
	require.NoError(t, syncer.Sync(ctx, f.tc.ProjectID, []opensheetsync.Ref{d, w}), "the same job delivered again")
	require.Equal(t, posts, sheets.CountRequests(http.MethodPost, "yasaku-wallets"), "a duplicate job writes once")

	now = now.Add(10 * time.Minute)
	n, err = mirror.Reconcile(orgCtx)
	require.NoError(t, err)
	require.Zero(t, n, "nothing is left to reconcile")
}
