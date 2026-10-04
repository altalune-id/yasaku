package opensheetsync_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	apperrorv1 "altalune.id/yasaku/gen/go/apperror/v1"
	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/opensheetsync"
	"altalune.id/yasaku/internal/platform/queue"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/testutil/fakes"
	"altalune.id/yasaku/scheduler"
)

type mirrorEnv struct {
	store   *fakes.OpensheetSync
	jobs    *fakes.Queue
	mirror  *opensheetsync.Mirror
	tc      tenant.Context
	ctx     context.Context
	now     time.Time
	reports *int
}

func newMirrorEnv(t *testing.T) *mirrorEnv {
	t.Helper()
	e := &mirrorEnv{
		store: fakes.NewOpensheetSync(), jobs: &fakes.Queue{},
		tc:  tenant.Context{OrgID: uuid.New(), ProjectID: uuid.New(), UserID: uuid.New()},
		now: time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC),
	}
	n := 0
	e.reports = &n
	unexpected := func(_ context.Context, _ string, err error, _ ...any) *apperror.AppError {
		n++
		return apperror.New("yasaku.unexpected", err.Error(), codes.Internal, &apperrorv1.ErrorDetail{Code: "yasaku.unexpected"}).WithCause(err)
	}
	e.mirror = opensheetsync.NewMirror(e.store, e.jobs, false, slog.New(slog.NewTextHandler(io.Discard, nil)), unexpected, func() time.Time { return e.now })
	e.ctx = tenant.Into(context.Background(), e.tc)
	return e
}

func (e *mirrorEnv) link(t *testing.T, tc tenant.Context, enabled bool) {
	t.Helper()
	l := linkFor(tc, e.now)
	if enabled {
		require.NoError(t, l.Enable(e.now))
	}
	require.NoError(t, e.store.SaveLink(tenant.Into(context.Background(), tc), l))
}

func (e *mirrorEnv) payloads(t *testing.T) []opensheetsync.SyncPayload {
	t.Helper()
	var out []opensheetsync.SyncPayload
	for _, c := range e.jobs.Recorded() {
		require.Equal(t, opensheetsync.SyncJob(), c.Job)
		out = append(out, c.Data.(opensheetsync.SyncPayload))
	}
	return out
}

func TestMark_DoesNothingWithoutAnEnabledLink(t *testing.T) {
	e := newMirrorEnv(t)
	w := ref(opensheetsync.EntityWallet, uuid.New())
	require.NoError(t, e.mirror.Mark(e.ctx, w))
	_, ok := e.store.State(e.tc.OrgID, w.Entity, w.ID)
	require.False(t, ok, "no link: nothing is marked")

	e.link(t, e.tc, false)
	require.NoError(t, e.mirror.Mark(e.ctx, w))
	_, ok = e.store.State(e.tc.OrgID, w.Entity, w.ID)
	require.False(t, ok, "a link that is off marks nothing")
}

func TestMark_BumpsDedupedRefsAndCascadesARename(t *testing.T) {
	e := newMirrorEnv(t)
	e.link(t, e.tc, true)
	wallet, tx1, tx2 := uuid.New(), uuid.New(), uuid.New()
	e.store.SeedTransaction(e.tc.OrgID, e.tc.ProjectID, tx1, wallet)
	e.store.SeedTransaction(e.tc.OrgID, e.tc.ProjectID, tx2, uuid.New())

	require.NoError(t, e.mirror.Mark(e.ctx,
		opensheetsync.Ref{Entity: opensheetsync.EntityWallet, ID: wallet},
		opensheetsync.Ref{Entity: opensheetsync.EntityWallet, ID: wallet, Cascade: true}))
	st, ok := e.store.State(e.tc.OrgID, opensheetsync.EntityWallet, wallet)
	require.True(t, ok)
	require.EqualValues(t, 1, st.Version, "two refs to one row mark it once")
	_, ok = e.store.State(e.tc.OrgID, opensheetsync.EntityTransaction, tx1)
	require.True(t, ok, "the rename re-marks the transaction showing the wallet's name")
	_, ok = e.store.State(e.tc.OrgID, opensheetsync.EntityTransaction, tx2)
	require.False(t, ok)
}

func TestMark_PropagatesAStoreFailure(t *testing.T) {
	e := newMirrorEnv(t)
	e.link(t, e.tc, true)
	e.store.MarkErr = errors.New("disk full")
	require.Error(t, e.mirror.Mark(e.ctx, ref(opensheetsync.EntityWallet, uuid.New())))
}

func TestKick_SubmitsOneJobOfAtMostFiftyRefsOnlyForAnEnabledLink(t *testing.T) {
	e := newMirrorEnv(t)
	refs := make([]opensheetsync.Ref, 0, 51)
	for range 51 {
		refs = append(refs, ref(opensheetsync.EntityTransaction, uuid.New()))
	}
	e.mirror.Kick(e.ctx, refs...)
	require.Empty(t, e.jobs.Recorded(), "no link: no job")

	e.link(t, e.tc, true)
	e.mirror.Kick(e.ctx, refs...)
	got := e.payloads(t)
	require.Len(t, got, 1, "one job per kick, however big the write; the rest stay dirty")
	require.Len(t, got[0].Refs, 50)
	require.Equal(t, e.tc.ProjectID, got[0].ProjectID)
}

func TestKick_SendsRefsInLockOrder(t *testing.T) {
	e := newMirrorEnv(t)
	e.link(t, e.tc, true)
	a, b := uuid.MustParse("00000000-0000-7000-8000-000000000001"), uuid.MustParse("00000000-0000-7000-8000-000000000002")
	e.mirror.Kick(e.ctx, ref(opensheetsync.EntityWallet, b), ref(opensheetsync.EntityCategory, a), ref(opensheetsync.EntityTransaction, a), ref(opensheetsync.EntityWallet, a))
	require.Equal(t, []opensheetsync.SyncPayloadRef{{Entity: "transaction", ID: a}, {Entity: "category", ID: a}, {Entity: "wallet", ID: a}, {Entity: "wallet", ID: b}}, e.payloads(t)[0].Refs,
		"transactions, then categories, then wallets: the order a rename's cascade already locks in")
}

func TestMark_CascadesBeforeTheDirectRefs(t *testing.T) {
	e := newMirrorEnv(t)
	e.link(t, e.tc, true)
	wallet, tx := uuid.New(), uuid.New()
	e.store.SeedTransaction(e.tc.OrgID, e.tc.ProjectID, tx, wallet)
	before := len(e.store.Calls())
	require.NoError(t, e.mirror.Mark(e.ctx, opensheetsync.Ref{Entity: opensheetsync.EntityWallet, ID: wallet, Cascade: true}))
	require.Equal(t, []string{"LinkEnabled", "MarkReferencing", "Mark"}, e.store.Calls()[before:],
		"the cascade locks the transactions before the wallet row, the order every writer uses")
}

func TestKick_ReportsASubmitFailureAndStillReturns(t *testing.T) {
	e := newMirrorEnv(t)
	e.link(t, e.tc, true)
	e.jobs.Err = errors.New("nats down")
	e.mirror.Kick(e.ctx, ref(opensheetsync.EntityWallet, uuid.New()))
	require.Equal(t, 1, *e.reports)
}

func TestKickDirty_SendsTheBackfillsFirstPage(t *testing.T) {
	e := newMirrorEnv(t)
	e.link(t, e.tc, true)
	for range 60 {
		e.store.SeedEntity(e.tc.OrgID, e.tc.ProjectID, opensheetsync.EntityWallet, uuid.New())
	}
	_, err := e.store.MarkAll(e.ctx, e.tc.OrgID, e.tc.ProjectID, e.now)
	require.NoError(t, err)
	e.mirror.KickDirty(e.ctx)
	got := e.payloads(t)
	require.Len(t, got, 1, "one job now; the reconciler drains the rest")
	require.Len(t, got[0].Refs, 50)
}

func TestReconcile_ResubmitsStaleDirtyRowsOfEveryEnabledLink(t *testing.T) {
	e := newMirrorEnv(t)
	sibling := tenant.Context{OrgID: e.tc.OrgID, ProjectID: uuid.New(), UserID: e.tc.UserID}
	off := tenant.Context{OrgID: e.tc.OrgID, ProjectID: uuid.New(), UserID: e.tc.UserID}
	e.link(t, e.tc, true)
	e.link(t, sibling, true)
	e.link(t, off, false)
	stale, fresh, other := ref(opensheetsync.EntityWallet, uuid.New()), ref(opensheetsync.EntityWallet, uuid.New()), ref(opensheetsync.EntityCategory, uuid.New())
	require.NoError(t, e.store.Mark(e.ctx, e.tc.OrgID, e.tc.ProjectID, []opensheetsync.Ref{stale}, e.now.Add(-2*time.Minute)))
	require.NoError(t, e.store.Mark(e.ctx, e.tc.OrgID, e.tc.ProjectID, []opensheetsync.Ref{fresh}, e.now.Add(-10*time.Second)))
	require.NoError(t, e.store.Mark(e.ctx, e.tc.OrgID, sibling.ProjectID, []opensheetsync.Ref{other}, e.now.Add(-time.Hour)))
	require.NoError(t, e.store.Mark(e.ctx, e.tc.OrgID, off.ProjectID, []opensheetsync.Ref{ref(opensheetsync.EntityWallet, uuid.New())}, e.now.Add(-time.Hour)))

	orgCtx := tenant.Into(context.Background(), tenant.Context{OrgID: e.tc.OrgID})
	n, err := e.mirror.Reconcile(orgCtx)
	require.NoError(t, err)
	require.Equal(t, 2, n)
	byProject := map[uuid.UUID][]opensheetsync.SyncPayloadRef{}
	for _, p := range e.payloads(t) {
		byProject[p.ProjectID] = append(byProject[p.ProjectID], p.Refs...)
	}
	require.Equal(t, []opensheetsync.SyncPayloadRef{{Entity: "wallet", ID: stale.ID}}, byProject[e.tc.ProjectID], "a row marked under a minute ago waits")
	require.Equal(t, []opensheetsync.SyncPayloadRef{{Entity: "category", ID: other.ID}}, byProject[sibling.ProjectID])
	require.NotContains(t, byProject, off.ProjectID, "a link that is off is skipped")
}

func TestReconcile_StopsSubmittingOnceItsContextIsDoneOrNearlyOut(t *testing.T) {
	e := newMirrorEnv(t)
	e.link(t, e.tc, true)
	require.NoError(t, e.store.Mark(e.ctx, e.tc.OrgID, e.tc.ProjectID, []opensheetsync.Ref{ref(opensheetsync.EntityWallet, uuid.New())}, e.now.Add(-time.Hour)))
	orgCtx := tenant.Into(context.Background(), tenant.Context{OrgID: e.tc.OrgID})

	cancelled, cancel := context.WithCancel(orgCtx)
	cancel()
	n, err := e.mirror.Reconcile(cancelled)
	require.NoError(t, err)
	require.Zero(t, n)

	short, cancel2 := context.WithTimeout(orgCtx, 10*time.Second)
	defer cancel2()
	n, err = e.mirror.Reconcile(short)
	require.NoError(t, err)
	require.Zero(t, n, "under 25s left: an inline job could outlive the scheduler's timeout")
	require.Empty(t, e.jobs.Recorded())
}

func TestScheduler_DeclaresTheReconcilerOnlyWhenMounted(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	require.Empty(t, opensheetsync.NewScheduler(nil, log).SchedulerJobs())
	jobs := opensheetsync.NewScheduler(newMirrorEnv(t).mirror, log).SchedulerJobs()
	require.Len(t, jobs, 1)
	require.Equal(t, "opensheet-reconcile", jobs[0].Name)
	require.Equal(t, scheduler.ScopeTenant, jobs[0].Scope)
	require.True(t, jobs[0].Singleton)
	require.Equal(t, time.Minute, jobs[0].Timeout, "the scheduler applies it per tenant")
}

func TestReconcile_SendsOnePagePerProjectWhenQueuedAndUpToTwoInline(t *testing.T) {
	e := newMirrorEnv(t)
	e.link(t, e.tc, true)
	for range 120 {
		require.NoError(t, e.store.Mark(e.ctx, e.tc.OrgID, e.tc.ProjectID, []opensheetsync.Ref{ref(opensheetsync.EntityWallet, uuid.New())}, e.now.Add(-time.Hour)))
	}
	orgCtx := tenant.Into(context.Background(), tenant.Context{OrgID: e.tc.OrgID})
	n, err := e.mirror.Reconcile(orgCtx)
	require.NoError(t, err)
	require.Equal(t, 100, n)
	require.Len(t, e.payloads(t), 2, "inline: two pages, all one org's 60s timeout fits")

	e.jobs.Reset()
	queued := opensheetsync.NewMirror(e.store, e.jobs, true, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, func() time.Time { return e.now })
	n, err = queued.Reconcile(orgCtx)
	require.NoError(t, err)
	require.Equal(t, 50, n, "queued: one page; the sync job follows up")
	require.Len(t, e.payloads(t), 1)
}

func TestReconcile_StartsOneProjectLaterEachTick(t *testing.T) {
	e := newMirrorEnv(t)
	projects := []uuid.UUID{e.tc.ProjectID, uuid.New(), uuid.New()}
	for _, p := range projects {
		tc := tenant.Context{OrgID: e.tc.OrgID, ProjectID: p, UserID: e.tc.UserID}
		e.link(t, tc, true)
		require.NoError(t, e.store.Mark(e.ctx, e.tc.OrgID, p, []opensheetsync.Ref{ref(opensheetsync.EntityWallet, uuid.New())}, e.now.Add(-time.Hour)))
	}
	orgCtx := tenant.Into(context.Background(), tenant.Context{OrgID: e.tc.OrgID})
	first := map[uuid.UUID]bool{}
	for range projects {
		e.jobs.Reset()
		_, err := e.mirror.Reconcile(orgCtx)
		require.NoError(t, err)
		first[e.payloads(t)[0].ProjectID] = true
		e.now = e.now.Add(5 * time.Minute)
	}
	require.Len(t, first, len(projects), "over three ticks every project goes first once")
}

type detachedQueue struct {
	fakes.Queue
	cancelled, deadlined int
	tenants              []tenant.Context
}

func (q *detachedQueue) Submit(ctx context.Context, j queue.Job, data any) error {
	if ctx.Err() != nil {
		q.cancelled++
	}
	if _, ok := ctx.Deadline(); ok {
		q.deadlined++
	}
	tc, _ := tenant.From(ctx)
	q.tenants = append(q.tenants, tc)
	return q.Queue.Submit(ctx, j, data)
}

func (q *detachedQueue) requireTenantOfEachPayload(t *testing.T, orgID uuid.UUID) {
	t.Helper()
	calls := q.Recorded()
	require.Len(t, q.tenants, len(calls))
	for i, c := range calls {
		p := c.Data.(opensheetsync.SyncPayload)
		require.Equal(t, orgID, q.tenants[i].OrgID, "the job runs in the org it was submitted for")
		require.Equal(t, p.ProjectID, q.tenants[i].ProjectID, "the job's tenant names the project its payload syncs")
	}
}

func discardLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestKick_StillHandsTheJobOverWhenTheRequestEnded(t *testing.T) {
	e := newMirrorEnv(t)
	e.link(t, e.tc, true)
	q := &detachedQueue{}
	m := opensheetsync.NewMirror(e.store, q, false, discardLog(), nil, func() time.Time { return e.now })
	ended, cancel := context.WithCancel(e.ctx)
	cancel()
	m.Kick(ended, ref(opensheetsync.EntityWallet, uuid.New()))
	require.Len(t, q.Recorded(), 1)
	require.Zero(t, q.cancelled, "the job is submitted on a context the request's end cannot cancel")
	q.requireTenantOfEachPayload(t, e.tc.OrgID)
}

func TestKickDirty_StillHandsTheJobOverWhenTheRequestEnded(t *testing.T) {
	e := newMirrorEnv(t)
	e.link(t, e.tc, true)
	require.NoError(t, e.store.Mark(e.ctx, e.tc.OrgID, e.tc.ProjectID, []opensheetsync.Ref{ref(opensheetsync.EntityWallet, uuid.New())}, e.now))
	q := &detachedQueue{}
	m := opensheetsync.NewMirror(e.store, q, false, discardLog(), nil, func() time.Time { return e.now })
	ended, cancel := context.WithCancel(e.ctx)
	cancel()
	m.KickDirty(ended)
	require.Len(t, q.Recorded(), 1, "the backfill's first page is handed over after the request ended")
	require.Zero(t, q.cancelled)
	q.requireTenantOfEachPayload(t, e.tc.OrgID)
}

func TestReconcile_SubmitsDetachedFromTheTickDeadline(t *testing.T) {
	e := newMirrorEnv(t)
	sibling := tenant.Context{OrgID: e.tc.OrgID, ProjectID: uuid.New(), UserID: e.tc.UserID}
	for _, tc := range []tenant.Context{e.tc, sibling} {
		e.link(t, tc, true)
		require.NoError(t, e.store.Mark(e.ctx, tc.OrgID, tc.ProjectID, []opensheetsync.Ref{ref(opensheetsync.EntityWallet, uuid.New())}, e.now.Add(-time.Hour)))
	}
	q := &detachedQueue{}
	m := opensheetsync.NewMirror(e.store, q, true, discardLog(), nil, func() time.Time { return e.now })
	tick, cancel := context.WithTimeout(tenant.Into(context.Background(), tenant.Context{OrgID: e.tc.OrgID}), time.Minute)
	defer cancel()
	n, err := m.Reconcile(tick)
	require.NoError(t, err)
	require.Equal(t, 2, n)
	require.Zero(t, q.deadlined, "a submitted job is detached from the tick's deadline")
	q.requireTenantOfEachPayload(t, e.tc.OrgID)
}

type blockingReads struct {
	*fakes.OpensheetSync
}

func (b blockingReads) LinkEnabled(ctx context.Context, _, _ uuid.UUID) (bool, error) {
	<-ctx.Done()
	return false, ctx.Err()
}

func (b blockingReads) ListDirty(ctx context.Context, _, _ uuid.UUID, _, _ time.Time, _ int) ([]opensheetsync.Ref, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestKick_GivesUpOnAStuckStoreReadAfterThreeSeconds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newMirrorEnv(t)
		q := &detachedQueue{}
		reports := 0
		unexpected := func(_ context.Context, _ string, err error, _ ...any) *apperror.AppError {
			require.ErrorIs(t, err, context.DeadlineExceeded)
			reports++
			return nil
		}
		m := opensheetsync.NewMirror(blockingReads{e.store}, q, false, discardLog(), unexpected, func() time.Time { return e.now })
		start := time.Now()
		m.Kick(e.ctx, ref(opensheetsync.EntityWallet, uuid.New()))
		m.KickDirty(e.ctx)
		require.Equal(t, 6*time.Second, time.Since(start), "each detached read is bounded at 3s")
		require.Equal(t, 2, reports)
		require.Empty(t, q.Recorded())
	})
}

func TestKicks_WarnWithoutATenant(t *testing.T) {
	var buf bytes.Buffer
	e := newMirrorEnv(t)
	m := opensheetsync.NewMirror(e.store, e.jobs, false, slog.New(slog.NewTextHandler(&buf, nil)), nil, func() time.Time { return e.now })
	m.Kick(context.Background(), ref(opensheetsync.EntityWallet, uuid.New()))
	m.KickDirty(context.Background())
	require.Equal(t, 2, strings.Count(buf.String(), "opensheetsync: kick without a tenant"))
	require.Empty(t, e.jobs.Recorded())
}
