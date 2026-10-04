package opensheetsync_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/opensheetsync"
	"altalune.id/yasaku/internal/platform/queue"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/testutil/fakes"
	"altalune.id/yasaku/money"
	"altalune.id/yasaku/opensheet"
)

// NOTE: stubSource is a hand-written Source: rows by id, plus an optional hook run while a row is read.
type stubSource struct {
	mu       sync.Mutex
	txs      map[uuid.UUID]opensheetsync.TransactionFacts
	wallet   map[uuid.UUID]opensheetsync.WalletFacts
	onRead   func()
	honorCtx bool
}

func (s *stubSource) Transaction(ctx context.Context, id uuid.UUID) (opensheetsync.TransactionFacts, bool, error) {
	s.mu.Lock()
	f, ok := s.txs[id]
	hook, honor := s.onRead, s.honorCtx
	s.mu.Unlock()
	if hook != nil {
		hook()
	}
	if honor && ctx.Err() != nil {
		return opensheetsync.TransactionFacts{}, false, fmt.Errorf("stub source: %w", ctx.Err())
	}
	return f, ok, nil
}

func (s *stubSource) Wallet(_ context.Context, id uuid.UUID) (opensheetsync.WalletFacts, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.wallet[id]
	return f, ok, nil
}

func (s *stubSource) Category(context.Context, uuid.UUID) (opensheetsync.CategoryFacts, bool, error) {
	return opensheetsync.CategoryFacts{}, false, nil
}

type syncEnv struct {
	*serviceEnv
	source *stubSource
	syncer *opensheetsync.Syncer
}

func newSyncEnv(t *testing.T) *syncEnv {
	t.Helper()
	e := &syncEnv{serviceEnv: newServiceEnv(t), source: &stubSource{txs: map[uuid.UUID]opensheetsync.TransactionFacts{}, wallet: map[uuid.UUID]opensheetsync.WalletFacts{}}}
	addContractSheets(e.sheets, true)
	_, _, err := e.svc.Save(e.ctx, settings(goodKey))
	require.NoError(t, err)
	_, err = e.svc.SetEnabled(e.ctx, true)
	require.NoError(t, err)
	e.syncer = e.newSyncer(nil, false)
	return e
}

func (e *syncEnv) newSyncer(jobs opensheetsync.Queue, queued bool) *opensheetsync.Syncer {
	return opensheetsync.NewSyncer(e.store, slog.New(slog.NewTextHandler(io.Discard, nil)),
		apperror.NewReporter(slog.New(slog.DiscardHandler), false).Unexpected, opensheetsync.SyncerDeps{
			Sealer:   e.sealer,
			Endpoint: opensheetsync.Endpoint{BaseURL: e.sheets.URL(), AllowPrivateHosts: true, Timeout: 2 * time.Second},
			Source:   e.source, Jobs: jobs, Queued: queued, Now: func() time.Time { return e.now },
		})
}

func (e *syncEnv) tx(t *testing.T, note string) opensheetsync.Ref {
	t.Helper()
	id := uuid.New()
	e.source.mu.Lock()
	e.source.txs[id] = opensheetsync.TransactionFacts{ID: id, Kind: "expense", Amount: money.New(4_000_000, money.IDR),
		OccurredAt: e.now, WalletID: uuid.New(), WalletName: "BCA", Note: note, UpdatedAt: e.now}
	e.source.mu.Unlock()
	r := ref(opensheetsync.EntityTransaction, id)
	require.NoError(t, e.mirror.Mark(e.ctx, r))
	return r
}

func (e *syncEnv) link(t *testing.T) *opensheetsync.Link {
	t.Helper()
	l, err := e.store.LinkByProject(e.ctx, e.tc.OrgID, e.tc.ProjectID)
	require.NoError(t, err)
	return l
}

func TestSync_CreatesThenPatchesAndADuplicateJobWritesOnce(t *testing.T) {
	e := newSyncEnv(t)
	r := e.tx(t, "nasi goreng")
	require.NoError(t, e.syncer.Sync(e.ctx, e.tc.ProjectID, []opensheetsync.Ref{r}))
	row, ok := e.sheets.Row("yasaku-transactions", r.ID.String())
	require.True(t, ok)
	require.Equal(t, "nasi goreng", row["note"])
	require.Equal(t, "40000", row["amount"])
	reqs := e.sheets.Requests()
	last := reqs[len(reqs)-1]
	require.Equal(t, http.MethodPost, last.Method)
	require.Equal(t, r.ID.String()+":1", last.IdempotencyKey)

	posts := e.sheets.CountRequests(http.MethodPost, "yasaku-transactions")
	require.NoError(t, e.syncer.Sync(e.ctx, e.tc.ProjectID, []opensheetsync.Ref{r}), "a redelivered job")
	require.Equal(t, posts, e.sheets.CountRequests(http.MethodPost, "yasaku-transactions"), "a clean row is not pushed again")
	st, _ := e.store.State(e.tc.OrgID, r.Entity, r.ID)
	require.False(t, st.Dirty())

	e.source.mu.Lock()
	f := e.source.txs[r.ID]
	f.Note = "nasi goreng spesial"
	e.source.txs[r.ID] = f
	e.source.mu.Unlock()
	require.NoError(t, e.mirror.Mark(e.ctx, r))
	require.NoError(t, e.syncer.Sync(e.ctx, e.tc.ProjectID, []opensheetsync.Ref{r}))
	row, _ = e.sheets.Row("yasaku-transactions", r.ID.String())
	require.Equal(t, "nasi goreng spesial", row["note"])
	require.Equal(t, posts, e.sheets.CountRequests(http.MethodPost, "yasaku-transactions"), "an existing row is patched")
	require.Empty(t, e.link(t).LastError)
	require.NotNil(t, e.link(t).LastSyncedAt)
}

func TestSync_DeletesADeletedOrVanishedRowAndTreatsAMissingRowAsDone(t *testing.T) {
	e := newSyncEnv(t)
	r := e.tx(t, "x")
	require.NoError(t, e.syncer.Sync(e.ctx, e.tc.ProjectID, []opensheetsync.Ref{r}))
	require.NoError(t, e.mirror.Mark(e.ctx, opensheetsync.Ref{Entity: r.Entity, ID: r.ID, Deleted: true}))
	require.NoError(t, e.syncer.Sync(e.ctx, e.tc.ProjectID, []opensheetsync.Ref{r}))
	row, _ := e.sheets.Row("yasaku-transactions", r.ID.String())
	require.NotEmpty(t, row["deleted_at"])

	gone := ref(opensheetsync.EntityWallet, uuid.New())
	require.NoError(t, e.mirror.Mark(e.ctx, gone))
	require.NoError(t, e.syncer.Sync(e.ctx, e.tc.ProjectID, []opensheetsync.Ref{gone}), "a row neither in yasaku nor in the sheet is done")
	st, _ := e.store.State(e.tc.OrgID, gone.Entity, gone.ID)
	require.False(t, st.Dirty())
}

func TestSync_ARowGoneFromTheSourceIsTombstonedInTheSheet(t *testing.T) {
	e := newSyncEnv(t)
	r := e.tx(t, "x")
	require.NoError(t, e.syncer.Sync(e.ctx, e.tc.ProjectID, []opensheetsync.Ref{r}))
	e.source.mu.Lock()
	delete(e.source.txs, r.ID)
	e.source.mu.Unlock()
	require.NoError(t, e.mirror.Mark(e.ctx, r))
	st, _ := e.store.State(e.tc.OrgID, r.Entity, r.ID)
	require.False(t, st.Deleted, "an edit mark, not a delete mark")

	require.NoError(t, e.syncer.Sync(e.ctx, e.tc.ProjectID, []opensheetsync.Ref{r}))
	require.Equal(t, 1, e.sheets.CountRequests(http.MethodDelete, "yasaku-transactions"))
	row, _ := e.sheets.Row("yasaku-transactions", r.ID.String())
	require.NotEmpty(t, row["deleted_at"], "a row the source no longer has is tombstoned")
	st, _ = e.store.State(e.tc.OrgID, r.Entity, r.ID)
	require.False(t, st.Dirty())
}

func TestSync_AMarkDuringThePushIsPushedByTheSameJob(t *testing.T) {
	e := newSyncEnv(t)
	r := e.tx(t, "first")
	once := sync.Once{}
	e.source.onRead = func() { once.Do(func() { require.NoError(t, e.mirror.Mark(e.ctx, r)) }) }
	require.NoError(t, e.syncer.Sync(e.ctx, e.tc.ProjectID, []opensheetsync.Ref{r}))
	st, _ := e.store.State(e.tc.OrgID, r.Entity, r.ID)
	require.EqualValues(t, 2, st.Version)
	require.EqualValues(t, 2, st.SyncedVersion, "the holder looped instead of leaving the row to the reconciler")
}

func TestSync_ALeasedRowIsLeftToItsHolder(t *testing.T) {
	e := newSyncEnv(t)
	r := e.tx(t, "x")
	_, ok, err := e.store.Claim(e.ctx, e.tc.OrgID, e.tc.ProjectID, r, uuid.New(), e.now, time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	before := len(e.sheets.Requests())
	require.NoError(t, e.syncer.Sync(e.ctx, e.tc.ProjectID, []opensheetsync.Ref{r}))
	require.Len(t, e.sheets.Requests(), before)
}

func TestSync_ALinkRefusalIsRecordedOnTheLinkAndTurnsItOffOnTheThird(t *testing.T) {
	e := newSyncEnv(t)
	r := e.tx(t, "x")
	for i := 1; i <= opensheetsync.DisableAfter; i++ {
		e.sheets.FailNext(http.MethodPatch, "yasaku-transactions", http.StatusForbidden, "SHT009", 1)
		err := e.syncer.Sync(e.ctx, e.tc.ProjectID, []opensheetsync.Ref{r})
		require.True(t, opensheetsync.IsSyncRefusedError(err), "attempt %d: %v", i, err)
		st, _ := e.store.State(e.tc.OrgID, r.Entity, r.ID)
		require.Zero(t, st.Attempts, "a link refusal is the link's fault, not the row's")
		require.Contains(t, st.LastError, "SHT009")
		require.True(t, st.Dirty(), "the lease is released and the row stays dirty")
	}
	l := e.link(t)
	require.False(t, l.Enabled)
	require.True(t, l.AutoDisabled())
	require.Contains(t, l.LastError, "SHT009")
	require.NoError(t, e.syncer.Sync(e.ctx, e.tc.ProjectID, []opensheetsync.Ref{r}), "a link that is off does nothing")
}

// NOTE: Revision 3 (C1): the Test cannot see an empty tab's columns, so the first push is where a gap shows, and it is the tab's fault.
func TestSync_AColumnMissingFromAnEmptyTabRefusesTheLinkNotTheRow(t *testing.T) {
	e := newSyncEnv(t)
	tab := mustTab(t, opensheetsync.EntityTransaction)
	short := slices.DeleteFunc(slices.Clone(tab.Columns), func(c string) bool { return c == "note" })
	e.sheets.AddSheet(tab.DefaultSlug, fakes.OpensheetSheet{Columns: short, Writable: true})
	cl, err := e.svc.Test(e.ctx, settings(""))
	require.NoError(t, err)
	require.True(t, cl.OK(), "the empty tab hides the gap from the Test")

	r := e.tx(t, "kopi")
	err = e.syncer.Sync(e.ctx, e.tc.ProjectID, []opensheetsync.Ref{r})
	require.True(t, opensheetsync.IsSyncRefusedError(err), "%v", err)
	st, _ := e.store.State(e.tc.OrgID, r.Entity, r.ID)
	require.Zero(t, st.Attempts, "an unknown column is the tab's fault, not the row's")
	require.Contains(t, st.LastError, "SHT014")
	l := e.link(t)
	require.Equal(t, 1, l.FailureStreak)
	require.Contains(t, l.LastError, "SHT014")
	require.Equal(t, "opensheet refused a write to sheet yasaku-transactions (SHT014)", l.LastError, "the fake echoes the code as its message, so only the code is kept")
	require.True(t, l.AwaitingFirstSync())
}

func TestSync_EveryLinkLevelAnswerRefusesTheLink(t *testing.T) {
	for _, tt := range []struct {
		status int
		code   string
	}{
		{http.StatusConflict, "SHT010"}, {http.StatusConflict, "SHT011"}, {http.StatusConflict, "SHT021"}, {http.StatusUnprocessableEntity, "SHT029"},
		{http.StatusConflict, "SHT023"}, {http.StatusFailedDependency, "CRD006"}, {http.StatusUnauthorized, "GEN002"},
		{http.StatusForbidden, "SHT009"}, {http.StatusNotFound, "SHT008"},
	} {
		t.Run(tt.code, func(t *testing.T) {
			e := newSyncEnv(t)
			r := e.tx(t, "x")
			e.sheets.FailNext(http.MethodPatch, "yasaku-transactions", tt.status, tt.code, 1)
			err := e.syncer.Sync(e.ctx, e.tc.ProjectID, []opensheetsync.Ref{r})
			require.True(t, opensheetsync.IsSyncRefusedError(err), "%v", err)
			st, _ := e.store.State(e.tc.OrgID, r.Entity, r.ID)
			require.Zero(t, st.Attempts)
			require.Equal(t, 1, e.link(t).FailureStreak)
			require.Contains(t, e.link(t).LastError, tt.code)
		})
	}
}

func TestSync_ATransientFailureIsRetriedNotRecordedOnTheLink(t *testing.T) {
	e := newSyncEnv(t)
	r := e.tx(t, "x")
	e.sheets.FailNext(http.MethodPatch, "yasaku-transactions", http.StatusTooManyRequests, "GSH429", 1)
	err := e.syncer.Sync(e.ctx, e.tc.ProjectID, []opensheetsync.Ref{r})
	require.Error(t, err)
	require.False(t, opensheetsync.IsSyncRefusedError(err))
	require.Zero(t, e.link(t).FailureStreak)
	require.NoError(t, e.syncer.Sync(e.ctx, e.tc.ProjectID, []opensheetsync.Ref{r}), "the retry succeeds")
}

func TestSync_AWrongKeyIsPermanent(t *testing.T) {
	e := newSyncEnv(t)
	r := e.tx(t, "x")
	e.sheets.SetToken("osk_rotated_elsewhere")
	err := e.syncer.Sync(e.ctx, e.tc.ProjectID, []opensheetsync.Ref{r})
	require.True(t, opensheetsync.IsSyncRefusedError(err))
	require.Contains(t, e.link(t).LastError, "SHT001")
}

func TestSync_AnUnreadableKeyRefusesTheLinkAndCountsTowardAutoDisable(t *testing.T) {
	e := newSyncEnv(t)
	r := e.tx(t, "x")
	e.sealer = newSealer(t)
	syncer := e.newSyncer(nil, false)
	for range opensheetsync.DisableAfter {
		err := syncer.Sync(e.ctx, e.tc.ProjectID, []opensheetsync.Ref{r})
		require.True(t, opensheetsync.IsSyncRefusedError(err))
		require.True(t, opensheetsync.IsKeyUnreadableError(err), "the same type the service returns (Revision 5)")
	}
	require.False(t, e.link(t).Enabled, "an unreadable key turns the link off after DisableAfter refusals (D11)")
	require.Zero(t, e.sheets.CountRequests(http.MethodPatch, "yasaku-transactions"))
}

func TestSync_AnIDAlreadyInTheTabFallsBackToAPatch(t *testing.T) {
	e := newSyncEnv(t)
	r := e.tx(t, "x")
	require.NoError(t, e.syncer.Sync(e.ctx, e.tc.ProjectID, []opensheetsync.Ref{r}))
	require.NoError(t, e.mirror.Mark(e.ctx, r))
	e.sheets.FailNext(http.MethodPatch, "yasaku-transactions", http.StatusNotFound, "SHT013", 1)
	require.NoError(t, e.syncer.Sync(e.ctx, e.tc.ProjectID, []opensheetsync.Ref{r}))
	require.Len(t, e.sheets.Rows("yasaku-transactions"), 1)
}

func TestSync_AnEditToARowTombstonedInTheSheetIsPatchedAndStaysTombstoned(t *testing.T) {
	e := newSyncEnv(t)
	r := e.tx(t, "x")
	require.NoError(t, e.syncer.Sync(e.ctx, e.tc.ProjectID, []opensheetsync.Ref{r}))
	c, err := opensheet.New(opensheet.Config{BaseURL: e.sheets.URL(), Org: "acme", Project: "home", Token: goodKey, AllowPrivateHosts: true})
	require.NoError(t, err)
	require.NoError(t, c.DeleteRow(e.ctx, "yasaku-transactions", r.ID.String()))
	tomb, _ := e.sheets.Row("yasaku-transactions", r.ID.String())

	e.source.mu.Lock()
	f := e.source.txs[r.ID]
	f.Note = "y"
	e.source.txs[r.ID] = f
	e.source.mu.Unlock()
	require.NoError(t, e.mirror.Mark(e.ctx, r))
	posts := e.sheets.CountRequests(http.MethodPost, "yasaku-transactions")
	require.NoError(t, e.syncer.Sync(e.ctx, e.tc.ProjectID, []opensheetsync.Ref{r}))
	row, _ := e.sheets.Row("yasaku-transactions", r.ID.String())
	require.Equal(t, "y", row["note"])
	require.Equal(t, tomb["deleted_at"], row["deleted_at"], "yasaku never resurrects a row the sheet tombstoned")
	require.Equal(t, posts, e.sheets.CountRequests(http.MethodPost, "yasaku-transactions"), "no create")
	st, _ := e.store.State(e.tc.OrgID, r.Entity, r.ID)
	require.False(t, st.Dirty())
}

func TestSync_RefusesAPayloadForAnotherProject(t *testing.T) {
	e := newSyncEnv(t)
	r := e.tx(t, "x")
	before := len(e.sheets.Requests())
	err := e.syncer.Sync(e.ctx, uuid.New(), []opensheetsync.Ref{r})
	require.True(t, opensheetsync.IsScopeMismatchError(err))
	require.Len(t, e.sheets.Requests(), before)
}

func TestSync_StopsWhenTheBudgetIsNearlyOut(t *testing.T) {
	e := newSyncEnv(t)
	r := e.tx(t, "x")
	ctx, cancel := context.WithTimeout(e.ctx, 5*time.Second)
	defer cancel()
	require.NoError(t, e.syncer.Sync(ctx, e.tc.ProjectID, []opensheetsync.Ref{r}))
	st, _ := e.store.State(e.tc.OrgID, r.Entity, r.ID)
	require.True(t, st.Dirty(), "left for the reconciler")
}

func TestSync_ARowRefusalSkipsThatRowWithBackoffAndNeverPenalisesTheLink(t *testing.T) {
	e := newSyncEnv(t)
	bad, good := e.tx(t, "bad"), e.tx(t, "good")
	e.sheets.FailNext(http.MethodPatch, "yasaku-transactions", http.StatusBadRequest, "SHT016", 1)
	require.NoError(t, e.syncer.Sync(e.ctx, e.tc.ProjectID, []opensheetsync.Ref{bad, good}), "a refused row is not a failed job")
	_, ok := e.sheets.Row("yasaku-transactions", good.ID.String())
	require.True(t, ok, "the next ref still goes through")
	st, _ := e.store.State(e.tc.OrgID, bad.Entity, bad.ID)
	require.Equal(t, 1, st.Attempts)
	require.Contains(t, st.LastError, "SHT016")
	l := e.link(t)
	require.Zero(t, l.FailureStreak)
	require.True(t, l.Enabled)
	st2, err := e.svc.Status(e.ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, st2.Failing)

	posts := len(e.sheets.Requests())
	require.NoError(t, e.syncer.Sync(e.ctx, e.tc.ProjectID, []opensheetsync.Ref{bad}))
	require.Len(t, e.sheets.Requests(), posts, "held back by its backoff")
	e.now = e.now.Add(opensheetsync.RowBackoff(1) + time.Second)
	require.NoError(t, e.syncer.Sync(e.ctx, e.tc.ProjectID, []opensheetsync.Ref{bad}))
	_, ok = e.sheets.Row("yasaku-transactions", bad.ID.String())
	require.True(t, ok, "it goes through once the backoff is over and opensheet accepts it")
}

func TestSync_AFullBatchFollowsUpWithOneJobWhenQueued(t *testing.T) {
	e := newSyncEnv(t)
	follow := &fakes.Queue{}
	syncer := e.newSyncer(follow, true)
	refs := make([]opensheetsync.Ref, 0, 70)
	for range 70 {
		refs = append(refs, e.tx(t, "x"))
	}
	require.NoError(t, syncer.Sync(e.ctx, e.tc.ProjectID, refs[60:]))
	require.Empty(t, follow.Recorded(), "a short batch is the end of the backlog, even with rows still dirty")

	require.NoError(t, syncer.Sync(e.ctx, e.tc.ProjectID, refs[:50]))
	calls := follow.Recorded()
	require.Len(t, calls, 1, "one follow-up per full batch")
	require.Equal(t, opensheetsync.SyncJob(), calls[0].Job)
	next := calls[0].Data.(opensheetsync.SyncPayload)
	require.Equal(t, e.tc.ProjectID, next.ProjectID)
	require.Len(t, next.Refs, 10)

	follow.Reset()
	require.NoError(t, syncer.Sync(e.ctx, e.tc.ProjectID, refs[:50]), "a redelivered full batch")
	require.Empty(t, follow.Recorded(), "a full batch that pushed nothing does not chain, with rows still dirty")
	for _, r := range refs[:50] {
		require.NoError(t, e.mirror.Mark(e.ctx, r))
	}
	inline := &fakes.Queue{}
	require.NoError(t, e.newSyncer(inline, false).Sync(e.ctx, e.tc.ProjectID, refs[:50]), "a full batch that pushes, with rows still dirty")
	require.Empty(t, inline.Recorded(), "inline: never chains, even with a job submitter")
}

type shrinkingDeadline struct {
	context.Context
	at atomic.Pointer[time.Time]
}

func (c *shrinkingDeadline) Deadline() (time.Time, bool) { return *c.at.Load(), true }

func TestSync_ABatchCutShortByTheBudgetDoesNotFollowUp(t *testing.T) {
	e := newSyncEnv(t)
	follow := &fakes.Queue{}
	syncer := e.newSyncer(follow, true)
	refs := make([]opensheetsync.Ref, 0, 60)
	for range 60 {
		refs = append(refs, e.tx(t, "x"))
	}
	ctx := &shrinkingDeadline{Context: e.ctx}
	far := time.Now().Add(time.Hour)
	ctx.at.Store(&far)
	e.source.onRead = func() { near := time.Now().Add(time.Second); ctx.at.Store(&near) }
	require.NoError(t, syncer.Sync(ctx, e.tc.ProjectID, refs[:50]))
	st, _ := e.store.State(e.tc.OrgID, refs[1].Entity, refs[1].ID)
	require.True(t, st.Dirty(), "the budget stopped the batch after its first row")
	require.Empty(t, follow.Recorded(), "a batch the budget cut short leaves the rest to the reconciler")
}

type deadlineQueue struct {
	mu       sync.Mutex
	deadline []bool
	done     []bool
}

func (q *deadlineQueue) Submit(ctx context.Context, _ queue.Job, _ any) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	_, ok := ctx.Deadline()
	q.deadline = append(q.deadline, ok)
	q.done = append(q.done, ctx.Err() != nil)
	return nil
}

func TestSync_TheFollowUpSubmitIsBoundedAndDetached(t *testing.T) {
	e := newSyncEnv(t)
	q := &deadlineQueue{}
	syncer := e.newSyncer(q, true)
	refs := make([]opensheetsync.Ref, 0, 51)
	for range 51 {
		refs = append(refs, e.tx(t, "x"))
	}
	ctx, cancel := context.WithCancel(e.ctx)
	defer cancel()
	n := 0
	e.source.onRead = func() {
		if n++; n == 50 {
			cancel()
		}
	}
	err := syncer.Sync(ctx, e.tc.ProjectID, refs[:50])
	require.ErrorIs(t, err, context.Canceled, "the last push ran out of time")
	require.Empty(t, q.deadline, "a batch that did not finish does not follow up")

	e.source.onRead = nil
	ctx2, cancel2 := context.WithTimeout(e.ctx, time.Hour)
	defer cancel2()
	require.NoError(t, syncer.Sync(ctx2, e.tc.ProjectID, refs[:50]))
	q.mu.Lock()
	defer q.mu.Unlock()
	require.Equal(t, []bool{true}, q.deadline, "the follow-up submit has its own bound")
	require.Equal(t, []bool{false}, q.done)
}

func TestSync_WithoutATenantIsAScopeMismatch(t *testing.T) {
	e := newSyncEnv(t)
	r := e.tx(t, "x")
	before := len(e.sheets.Requests())
	err := e.syncer.Sync(context.Background(), e.tc.ProjectID, []opensheetsync.Ref{r})
	require.True(t, opensheetsync.IsScopeMismatchError(err), "%v", err)
	require.Contains(t, err.Error(), "no tenant")
	require.NotContains(t, err.Error(), uuid.Nil.String())
	require.Len(t, e.sheets.Requests(), before)
}

// NOTE: ctxStore honours ctx on the bookkeeping writes like the real stores do, so a write left on the handler's ctx fails.
type ctxStore struct {
	*fakes.OpensheetSync
	beforeSettle func()
}

func (s ctxStore) Settle(ctx context.Context, orgID, projectID uuid.UUID, ref opensheetsync.Ref, version int64, token uuid.UUID) (bool, bool, error) {
	if s.beforeSettle != nil {
		s.beforeSettle()
	}
	if err := ctx.Err(); err != nil {
		return false, false, err
	}
	return s.OpensheetSync.Settle(ctx, orgID, projectID, ref, version, token)
}

func (s ctxStore) Release(ctx context.Context, orgID, projectID uuid.UUID, ref opensheetsync.Ref, token uuid.UUID, f opensheetsync.Failure) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.OpensheetSync.Release(ctx, orgID, projectID, ref, token, f)
}

func (s ctxStore) SaveOutcome(ctx context.Context, orgID, projectID uuid.UUID, o opensheetsync.Outcome) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return s.OpensheetSync.SaveOutcome(ctx, orgID, projectID, o)
}

func TestSync_ACancelledPushIsStillRecordedAndNeverReportedAsAFault(t *testing.T) {
	for name, honorCtx := range map[string]bool{"cancelled during the PATCH": false, "cancelled during the source read": true} {
		t.Run(name, func(t *testing.T) {
			e := newSyncEnv(t)
			r := e.tx(t, "x")
			reports := 0
			syncer := opensheetsync.NewSyncer(ctxStore{OpensheetSync: e.store}, slog.New(slog.NewTextHandler(io.Discard, nil)),
				func(_ context.Context, _ string, err error, _ ...any) *apperror.AppError {
					reports++
					return apperror.New("yasaku.unexpected", err.Error(), codes.Internal, nil)
				}, opensheetsync.SyncerDeps{
					Sealer: e.sealer, Endpoint: opensheetsync.Endpoint{BaseURL: e.sheets.URL(), AllowPrivateHosts: true, Timeout: 2 * time.Second},
					Source: e.source, Now: func() time.Time { return e.now },
				})
			ctx, cancel := context.WithCancel(e.ctx)
			e.source.onRead, e.source.honorCtx = cancel, honorCtx
			err := syncer.Sync(ctx, e.tc.ProjectID, []opensheetsync.Ref{r})
			require.ErrorIs(t, err, context.Canceled)
			require.False(t, opensheetsync.IsSyncRefusedError(err))
			require.Zero(t, reports, "running out of time is not a fault")
			st, _ := e.store.State(e.tc.OrgID, r.Entity, r.ID)
			require.Contains(t, st.LastError, "context canceled", "the release ran detached from the cancelled ctx")
			require.Zero(t, st.Attempts)
			_, ok, err := e.store.Claim(e.ctx, e.tc.OrgID, e.tc.ProjectID, r, uuid.New(), e.now, time.Minute)
			require.NoError(t, err)
			require.True(t, ok, "the lease was released")
		})
	}
}

func TestSync_TheOutcomeAndTheSettleOfAPushThatUsedTheWholeBudgetAreStillRecorded(t *testing.T) {
	e := newSyncEnv(t)
	r := e.tx(t, "x")
	ctx, cancel := context.WithCancel(e.ctx)
	defer cancel()
	syncer := opensheetsync.NewSyncer(ctxStore{OpensheetSync: e.store, beforeSettle: cancel}, slog.New(slog.DiscardHandler), nil, opensheetsync.SyncerDeps{
		Sealer: e.sealer, Endpoint: opensheetsync.Endpoint{BaseURL: e.sheets.URL(), AllowPrivateHosts: true, Timeout: 2 * time.Second},
		Source: e.source, Now: func() time.Time { return e.now },
	})
	require.NoError(t, syncer.Sync(ctx, e.tc.ProjectID, []opensheetsync.Ref{r}))
	st, _ := e.store.State(e.tc.OrgID, r.Entity, r.ID)
	require.False(t, st.Dirty(), "the settle ran detached")
	require.NotNil(t, e.link(t).LastSyncedAt, "the outcome ran detached")
}

func TestConsumer_MapsFailuresToPermanentOrRetry(t *testing.T) {
	require.Empty(t, opensheetsync.NewConsumer(nil).ConsumerHandlers(), "an unmounted module declares no job")
	e := newSyncEnv(t)
	hs := opensheetsync.NewConsumer(e.syncer).ConsumerHandlers()
	require.Len(t, hs, 1)
	h := hs[0]
	require.Equal(t, queue.Job{Name: "opensheet.sync", Version: 1}, h.Job)
	require.True(t, h.SuppressReport)
	require.Nil(t, h.OnDeadLetter)

	msg := func(v any) queue.Message {
		b, err := json.Marshal(v)
		require.NoError(t, err)
		return queue.Message{ID: uuid.New(), Name: h.Job.Name, Version: 1, Data: b, NumDelivered: 1}
	}
	require.True(t, queue.IsPermanentError(h.Handle(e.ctx, queue.Message{Data: []byte("{")})), "a payload that does not decode")
	require.True(t, queue.IsPermanentError(h.Handle(e.ctx, msg(opensheetsync.SyncPayload{ProjectID: e.tc.ProjectID,
		Refs: []opensheetsync.SyncPayloadRef{{Entity: "period", ID: uuid.New()}}}))))
	require.True(t, queue.IsPermanentError(h.Handle(e.ctx, msg(opensheetsync.SyncPayload{ProjectID: uuid.New()}))), "another project")
	noTenant := h.Handle(context.Background(), msg(opensheetsync.SyncPayload{ProjectID: e.tc.ProjectID}))
	require.True(t, queue.IsPermanentError(noTenant), "no tenant")
	require.True(t, opensheetsync.IsScopeMismatchError(noTenant))

	r := e.tx(t, "x")
	e.sheets.FailNext(http.MethodPatch, "yasaku-transactions", http.StatusBadGateway, "GSH502", 1)
	err := h.Handle(e.ctx, msg(opensheetsync.SyncPayload{ProjectID: e.tc.ProjectID, Refs: []opensheetsync.SyncPayloadRef{{Entity: "transaction", ID: r.ID}}}))
	require.Error(t, err)
	require.False(t, queue.IsPermanentError(err), "a 5xx is retried")
	require.NoError(t, h.Handle(tenant.Into(context.Background(), e.tc), msg(opensheetsync.SyncPayload{ProjectID: e.tc.ProjectID,
		Refs: []opensheetsync.SyncPayloadRef{{Entity: "transaction", ID: r.ID}}})))
}

func TestSync_ARowRefusalDuringANewEditDoesNotChargeIt(t *testing.T) {
	e := newSyncEnv(t)
	r := e.tx(t, "x")
	once := sync.Once{}
	e.source.onRead = func() { once.Do(func() { require.NoError(t, e.mirror.Mark(e.ctx, r)) }) }
	e.sheets.FailNext(http.MethodPatch, "yasaku-transactions", http.StatusBadRequest, "SHT016", 1)
	require.NoError(t, e.syncer.Sync(e.ctx, e.tc.ProjectID, []opensheetsync.Ref{r}), "a refused row is not a failed job")
	st, _ := e.store.State(e.tc.OrgID, r.Entity, r.ID)
	require.EqualValues(t, 2, st.Version)
	require.Zero(t, st.Attempts, "the refusal was of version 1; version 2 has not been tried")
	require.Contains(t, st.LastError, "SHT016")
	_, ok, err := e.store.Claim(e.ctx, e.tc.OrgID, e.tc.ProjectID, r, uuid.New(), e.now, time.Minute)
	require.NoError(t, err)
	require.True(t, ok, "the new edit is claimable at once, with no retry_after")
}

// SECURITY: last_error is readable by any yasaku:read key, so a refusal stores the typed message and opensheet's codes, never a cause's hosts or URLs; the full error goes to the logs.
func TestSync_LastErrorNeverCarriesHostsAndTheLogsKeepTheFullError(t *testing.T) {
	e := newSyncEnv(t)
	r := e.tx(t, "x")
	logs := &lockedBuffer{}
	sy := opensheetsync.NewSyncer(e.store, slog.New(slog.NewTextHandler(logs, nil)),
		apperror.NewReporter(slog.New(slog.DiscardHandler), false).Unexpected, opensheetsync.SyncerDeps{
			Sealer:   e.sealer,
			Endpoint: opensheetsync.Endpoint{BaseURL: e.sheets.URL(), Timeout: 2 * time.Second},
			Source:   e.source, Now: func() time.Time { return e.now },
		})
	err := sy.Sync(e.ctx, e.tc.ProjectID, []opensheetsync.Ref{r})
	require.True(t, opensheetsync.IsSyncRefusedError(err), "%v", err)

	host := strings.TrimPrefix(e.sheets.URL(), "http://")
	l := e.link(t)
	require.NotEmpty(t, l.LastError)
	require.NotContains(t, l.LastError, host)
	require.NotContains(t, l.LastError, "http")
	require.Contains(t, l.LastError, "config")
	require.Contains(t, logs.String(), host, "the full error goes to the logs")
}

func TestRefusalText_KeepsCodesAndOpensheetsMessageButNoCauseText(t *testing.T) {
	transport := &url.Error{Op: "Patch", URL: "http://opensheet.railway.internal:8080/api/v1/acme/home/sheets/t/rows/1", Err: errors.New("dial tcp 10.0.0.7:8080: connection refused")}
	for _, tt := range []struct {
		name string
		err  error
		want string
	}{
		{"opensheet's own message", &opensheetsync.SyncRefusedError{Sheet: "yasaku-transactions", Code: "SHT014",
			Cause: &opensheet.APIError{Status: http.StatusBadRequest, Code: "SHT014", Message: "unknown column note"}},
			"opensheet refused a write to sheet yasaku-transactions: SHT014 unknown column note"},
		{"a message that only repeats the code", &opensheetsync.SyncRefusedError{Sheet: "yasaku-transactions", Code: "SHT014",
			Cause: &opensheet.APIError{Status: http.StatusBadRequest, Code: "SHT014", Message: "SHT014"}},
			"opensheet refused a write to sheet yasaku-transactions (SHT014)"},
		{"the 404 mask", &opensheetsync.SyncRefusedError{Sheet: "yasaku-wallets", Code: "SHT001", Cause: &opensheet.NotFoundError{Slug: "yasaku-wallets", Code: "SHT001"}},
			"opensheet refused a write to sheet yasaku-wallets (SHT001)"},
		{"a transport cause", &opensheetsync.SyncRefusedError{Sheet: "yasaku-transactions", Code: "SHT009", Cause: transport},
			"opensheet refused a write to sheet yasaku-transactions (SHT009)"},
		{"a bad config", &opensheetsync.SyncRefusedError{Code: "config", Cause: transport}, "opensheet refused the sync (config)"},
		{"an unreadable key", &opensheetsync.SyncRefusedError{Code: "key", Cause: &opensheetsync.KeyUnreadableError{Cause: errors.New("sealer: open failed: cipher: message authentication failed")}},
			"The saved opensheet API key can no longer be read; enter it again and save (OSL015)"},
		{"not a typed error", transport, "opensheet refused the sync"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := opensheetsync.RefusalText(tt.err)
			require.Equal(t, tt.want, got)
			for _, leak := range []string{"railway.internal", "10.0.0.7", "http", "cipher", "sealer"} {
				require.NotContains(t, got, leak)
			}
		})
	}
}

// SECURITY: a row's last_error follows the link's rule: no cause text, so no host or URL; the full error goes to the logs with the entity and id.
func TestSync_ARowFailureStoresNoHostAndTheLogsKeepTheFullError(t *testing.T) {
	e := newSyncEnv(t)
	r := e.tx(t, "x")
	logs := &lockedBuffer{}
	sy := opensheetsync.NewSyncer(e.store, slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		apperror.NewReporter(slog.New(slog.DiscardHandler), false).Unexpected, opensheetsync.SyncerDeps{
			Sealer:   e.sealer,
			Endpoint: opensheetsync.Endpoint{BaseURL: "http://127.0.0.1:1", AllowPrivateHosts: true, Timeout: 2 * time.Second},
			Source:   e.source, Now: func() time.Time { return e.now },
		})
	err := sy.Sync(e.ctx, e.tc.ProjectID, []opensheetsync.Ref{r})
	require.Error(t, err)
	require.False(t, opensheetsync.IsSyncRefusedError(err), "an unreachable opensheet is retried: %v", err)

	st, _ := e.store.State(e.tc.OrgID, r.Entity, r.ID)
	require.NotEmpty(t, st.LastError)
	require.NotContains(t, st.LastError, "127.0.0.1")
	require.NotContains(t, st.LastError, "http")
	require.Contains(t, logs.String(), "127.0.0.1", "the full error goes to the logs")
	require.Contains(t, logs.String(), r.ID.String(), "the log names the row")
	require.Contains(t, logs.String(), "level=DEBUG")
	require.NotContains(t, logs.String(), "level=WARN", "a retried failure is the runner's to warn about, not the row's")
}

func TestSync_ARefusedRowLogsAWarnNamingTheRow(t *testing.T) {
	e := newSyncEnv(t)
	r := e.tx(t, "x")
	logs := &lockedBuffer{}
	sy := opensheetsync.NewSyncer(e.store, slog.New(slog.NewTextHandler(logs, nil)),
		apperror.NewReporter(slog.New(slog.DiscardHandler), false).Unexpected, opensheetsync.SyncerDeps{
			Sealer:   e.sealer,
			Endpoint: opensheetsync.Endpoint{BaseURL: e.sheets.URL(), AllowPrivateHosts: true, Timeout: 2 * time.Second},
			Source:   e.source, Now: func() time.Time { return e.now },
		})
	e.sheets.FailNext(http.MethodPatch, "yasaku-transactions", http.StatusBadRequest, "SHT016", 1)
	require.NoError(t, sy.Sync(e.ctx, e.tc.ProjectID, []opensheetsync.Ref{r}))
	require.Contains(t, logs.String(), "level=WARN")
	require.Contains(t, logs.String(), "entity_id="+r.ID.String())
	require.Contains(t, logs.String(), "SHT016")
}

func TestFailureText_KeepsCodesAndOpensheetsMessageButNoCauseText(t *testing.T) {
	transport := &url.Error{Op: "Patch", URL: "http://opensheet.railway.internal:8080/api/v1/acme/home/sheets/t/rows/1", Err: errors.New("dial tcp 10.0.0.7:8080: connection refused")}
	for _, tt := range []struct {
		name string
		err  error
		want string
	}{
		{"a row refusal with opensheet's message", &opensheetsync.RowRefusedError{Sheet: "yasaku-transactions", Code: "SHT016",
			Cause: &opensheet.ValidationError{Code: "SHT016", Message: "empty patch"}},
			"opensheet refused a row of sheet yasaku-transactions: SHT016 empty patch"},
		{"a row refusal whose cause carries a host", &opensheetsync.RowRefusedError{Sheet: "yasaku-transactions", Code: "SHT016", Cause: transport},
			"opensheet refused a row of sheet yasaku-transactions (SHT016)"},
		{"a link refusal", &opensheetsync.SyncRefusedError{Sheet: "yasaku-wallets", Code: "SHT009", Cause: transport},
			"opensheet refused a write to sheet yasaku-wallets (SHT009)"},
		{"a transport failure", fmt.Errorf("opensheetsync: sheet yasaku-transactions: %w", transport), "opensheet could not be reached; the row is retried"},
		{"a 5xx", fmt.Errorf("opensheetsync: sheet t: %w", &opensheet.APIError{Status: http.StatusBadGateway, Code: "GEN502", Message: "upstream 10.0.0.9 failed"}),
			"opensheet failed (GEN502); the row is retried"},
		{"a cancellation", fmt.Errorf("read row: %w", context.Canceled), "the sync ran out of time (context canceled); the row is retried"},
		{"a deadline", fmt.Errorf("read row: %w", context.DeadlineExceeded), "the sync ran out of time (context deadline exceeded); the row is retried"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := opensheetsync.FailureText(tt.err)
			require.Equal(t, tt.want, got)
			for _, leak := range []string{"railway.internal", "10.0.0.7", "http", "dial"} {
				require.NotContains(t, got, leak)
			}
		})
	}
}

func TestSync_APrivateHostNameIsALinkLevelConfigRefusal(t *testing.T) {
	e := newSyncEnv(t)
	r := e.tx(t, "x")
	base := localhostURL(e.sheets.URL())
	syncer := opensheetsync.NewSyncer(e.store, slog.New(slog.DiscardHandler), nil, opensheetsync.SyncerDeps{
		Sealer: e.sealer, Endpoint: opensheetsync.Endpoint{BaseURL: base, Timeout: 2 * time.Second},
		Source: e.source, Now: func() time.Time { return e.now },
	})
	for i := 1; i <= opensheetsync.DisableAfter; i++ {
		err := syncer.Sync(e.ctx, e.tc.ProjectID, []opensheetsync.Ref{r})
		require.True(t, opensheetsync.IsSyncRefusedError(err), "attempt %d: %v", i, err)
		require.True(t, opensheetsync.IsPrivateEndpointError(err), "%v", err)
		sr, _ := errors.AsType[*opensheetsync.SyncRefusedError](err)
		require.Equal(t, "config", sr.Code)
		st, _ := e.store.State(e.tc.OrgID, r.Entity, r.ID)
		require.Zero(t, st.Attempts, "the deployment's fault, not the row's")
		require.True(t, st.Dirty())
	}
	l := e.link(t)
	require.False(t, l.Enabled, "a bad config turns the link off after DisableAfter refusals")
	require.Contains(t, l.LastError, "YASAKU_OPENSHEET_ALLOW_PRIVATE_HOSTS=true")
	require.Contains(t, l.LastError, apperror.CodeOpensheetPrivateEndpoint)
	host := strings.TrimPrefix(base, "http://")
	require.NotContains(t, l.LastError, host)
	require.NotContains(t, l.LastError, "localhost")
	require.NotContains(t, l.LastError, "::1")
	st, _ := e.store.State(e.tc.OrgID, r.Entity, r.ID)
	require.NotContains(t, st.LastError, "localhost")
	require.Zero(t, e.sheets.CountRequests(http.MethodPatch, "yasaku-transactions"))
}
