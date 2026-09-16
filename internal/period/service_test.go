package period_test

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/civil"
	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/period"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/testutil/fakes"
	"altalune.id/yasaku/money"
)

type fakeSettings struct {
	loc      *time.Location
	startDay int
	locErr   error
	dayErr   error
}

func (f *fakeSettings) Location(_ context.Context, _, _ uuid.UUID) (*time.Location, error) {
	return f.loc, f.locErr
}

func (f *fakeSettings) StartDay(_ context.Context, _, _ uuid.UUID) (int, error) {
	return f.startDay, f.dayErr
}

type fakeSnapshotter struct {
	mu    sync.Mutex
	snap  period.Snapshot
	err   error
	calls int
	hook  func()
}

func (f *fakeSnapshotter) Snapshot(_ context.Context, _, _, _ uuid.UUID) (period.Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.hook != nil {
		f.hook()
	}
	return f.snap, f.err
}

func (f *fakeSnapshotter) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type fixture struct {
	svc           *period.Service
	store         *fakes.Period
	settings      *fakeSettings
	snap          *fakeSnapshotter
	uowCalls      int
	insideUOW     bool
	snapInsideUOW bool
	beforeUOW     func()
	ctx           context.Context //nolint:containedctx // test fixture convenience
	tc            tenant.Context
	now           time.Time
}

func jakarta(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Jakarta")
	require.NoError(t, err)
	return loc
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	loc := jakarta(t)
	f := &fixture{
		store:    fakes.NewPeriod(),
		settings: &fakeSettings{loc: loc, startDay: 25},
		snap: &fakeSnapshotter{snap: period.Snapshot{
			Currency: money.IDR,
			Income:   5_000_000,
			Expense:  1_250_000,
			Net:      3_750_000,
			TxCount:  7,
			Wallets: []period.WalletClosing{
				{WalletID: uuid.New(), Name: "Dompet", Closing: 3_750_000},
			},
			ComputedAt: time.Date(2026, 9, 24, 17, 0, 0, 0, time.UTC),
		}},
		tc:  tenant.Context{OrgID: uuid.New(), ProjectID: uuid.New(), UserID: uuid.New()},
		now: time.Date(2026, 9, 15, 10, 0, 0, 0, loc),
	}
	f.ctx = tenant.Into(t.Context(), f.tc)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	unexpected := apperror.NewReporter(log, false).Unexpected
	f.snap.hook = func() { f.snapInsideUOW = f.insideUOW }
	uow := func(ctx context.Context, fn func(ctx context.Context) error) error {
		f.uowCalls++
		if f.beforeUOW != nil {
			f.beforeUOW()
		}
		f.insideUOW = true
		defer func() { f.insideUOW = false }()
		return fn(ctx)
	}
	f.svc = period.NewService(f.store, log, unexpected, f.settings, f.snap, uow, func() time.Time { return f.now })
	return f
}

func (f *fixture) setNow(t *testing.T, y int, m time.Month, d int) {
	t.Helper()
	f.now = time.Date(y, m, d, 10, 0, 0, 0, jakarta(t))
}

func date(y int, m time.Month, d int) civil.Date { return civil.Date{Year: y, Month: m, Day: d} }

func (f *fixture) currentCount(t *testing.T) int {
	t.Helper()
	all, err := f.store.List(f.ctx, f.tc.OrgID, f.tc.ProjectID, period.ListOpts{})
	require.NoError(t, err)
	n := 0
	for _, p := range all {
		if p.IsCurrent() {
			n++
		}
	}
	return n
}

func TestEnsureCurrent_DerivesFirstPeriodFromStartDay(t *testing.T) {
	f := newFixture(t)

	p, err := f.svc.EnsureCurrent(f.ctx)
	require.NoError(t, err)
	assert.Equal(t, date(2026, 8, 25), p.StartDate)
	assert.Equal(t, "Aug 2026", p.Name)
	assert.True(t, p.IsCurrent())
	assert.Equal(t, period.StatusOpen, p.Status)
	assert.Equal(t, f.tc.OrgID, p.OrgID)
	assert.Equal(t, f.tc.ProjectID, p.ProjectID)

	again, err := f.svc.EnsureCurrent(f.ctx)
	require.NoError(t, err)
	assert.Equal(t, p.ID, again.ID, "EnsureCurrent must be idempotent")
	assert.Equal(t, 1, f.currentCount(t))
}

func TestEnsureCurrent_UsesThisMonthWhenTodayIsOnOrAfterStartDay(t *testing.T) {
	tests := []struct {
		name     string
		today    civil.Date
		startDay int
		want     civil.Date
	}{
		{"before start day", date(2026, 9, 15), 25, date(2026, 8, 25)},
		{"on start day", date(2026, 9, 25), 25, date(2026, 9, 25)},
		{"after start day", date(2026, 9, 26), 25, date(2026, 9, 25)},
		{"january rolls back to december", date(2026, 1, 3), 25, date(2025, 12, 25)},
		{"start day clamped to 28", date(2026, 3, 30), 31, date(2026, 3, 28)},
		{"start day clamped up to 1", date(2026, 3, 30), 0, date(2026, 3, 1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			f.settings.startDay = tt.startDay
			f.setNow(t, tt.today.Year, tt.today.Month, tt.today.Day)

			p, err := f.svc.EnsureCurrent(f.ctx)
			require.NoError(t, err)
			assert.Equal(t, tt.want, p.StartDate)
		})
	}
}

func TestCurrent_IsReadOnly(t *testing.T) {
	f := newFixture(t)

	_, err := f.svc.Current(f.ctx)
	assert.True(t, period.IsNotFoundError(err), "got %T: %v", err, err)
	assert.Equal(t, 0, f.currentCount(t), "Current must not create a period")

	created, err := f.svc.EnsureCurrent(f.ctx)
	require.NoError(t, err)
	got, err := f.svc.Current(f.ctx)
	require.NoError(t, err)
	assert.Equal(t, created.ID, got.ID)
}

func TestClose_SnapshotsAndOpensTheNextPeriod(t *testing.T) {
	f := newFixture(t)
	p1, err := f.svc.EnsureCurrent(f.ctx)
	require.NoError(t, err)

	f.setNow(t, 2026, 9, 25)
	closed, err := f.svc.Close(f.ctx, p1.ID, date(2026, 9, 24), f.tc.UserID)
	require.NoError(t, err)

	assert.Equal(t, period.StatusClosed, closed.Status)
	assert.True(t, closed.IsLocked())
	assert.False(t, closed.IsCurrent())
	require.NotNil(t, closed.EndDate)
	assert.Equal(t, date(2026, 9, 24), *closed.EndDate)
	require.NotNil(t, closed.ClosedAt)
	assert.Equal(t, f.now.UTC(), *closed.ClosedAt)
	require.NotNil(t, closed.Snapshot)
	assert.Equal(t, int64(3_750_000), closed.Snapshot.Net)
	assert.Equal(t, money.IDR, closed.Snapshot.Currency)

	closings, err := f.svc.Closings(f.ctx, p1.ID)
	require.NoError(t, err)
	require.Len(t, closings, 1)
	assert.Equal(t, f.tc.UserID, closings[0].ClosedBy)
	assert.Equal(t, int64(3_750_000), closings[0].Snapshot.Net)

	next, err := f.svc.Current(f.ctx)
	require.NoError(t, err)
	assert.NotEqual(t, p1.ID, next.ID)
	assert.Equal(t, date(2026, 9, 25), next.StartDate)
	assert.Equal(t, "Sep 2026", next.Name)
	assert.Equal(t, 1, f.currentCount(t))
	assert.Equal(t, 1, f.uowCalls, "close must run inside the injected unit of work")
}

func TestClose_RejectsInvalidRanges(t *testing.T) {
	tests := []struct {
		name string
		end  civil.Date
	}{
		{"end before start", date(2026, 8, 24)},
		{"end after today", date(2026, 9, 16)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			p1, err := f.svc.EnsureCurrent(f.ctx)
			require.NoError(t, err)

			_, err = f.svc.Close(f.ctx, p1.ID, tt.end, f.tc.UserID)
			assert.True(t, period.IsInvalidRangeError(err), "got %T: %v", err, err)

			reread, err := f.svc.ByID(f.ctx, p1.ID)
			require.NoError(t, err)
			assert.True(t, reread.IsCurrent(), "a rejected close must leave the period current")
			assert.Equal(t, 0, f.snap.callCount(), "a rejected close must not compute a snapshot")
		})
	}
}

func TestClose_AcceptsEndEqualToStartAndToToday(t *testing.T) {
	tests := []struct {
		name string
		end  civil.Date
		next civil.Date
	}{
		{"end equals today", date(2026, 9, 15), date(2026, 9, 16)},
		{"end equals the start date", date(2026, 8, 25), date(2026, 8, 26)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			p1, err := f.svc.EnsureCurrent(f.ctx)
			require.NoError(t, err)

			closed, err := f.svc.Close(f.ctx, p1.ID, tt.end, f.tc.UserID)
			require.NoError(t, err)
			require.NotNil(t, closed.EndDate)
			assert.Equal(t, tt.end, *closed.EndDate)

			next, err := f.svc.Current(f.ctx)
			require.NoError(t, err)
			assert.Equal(t, tt.next, next.StartDate)
		})
	}
}

// TestEnsureCurrent_RereadsAfterLosingTheCreateRace drives the currentAfterRace branch with a competing creator that wins the partial unique index.
func TestEnsureCurrent_RereadsAfterLosingTheCreateRace(t *testing.T) {
	f := newFixture(t)
	winner, err := period.New(f.tc.OrgID, f.tc.ProjectID, date(2026, 8, 25), "")
	require.NoError(t, err)

	saves := 0
	f.store.SaveFn = func(ctx context.Context, p *period.Period) error {
		saves++
		f.store.SaveFn = nil
		require.NoError(t, f.store.Save(ctx, winner))
		return &period.OverlapError{Start: p.StartDate.String()}
	}

	got, err := f.svc.EnsureCurrent(f.ctx)
	require.NoError(t, err, "losing the race must re-read, not error")
	assert.Equal(t, winner.ID, got.ID)
	assert.Equal(t, 1, saves, "the race branch must be the one that ran")
	assert.Equal(t, 1, f.currentCount(t))
}

// TestEnsureCurrent_SurfacesTheRereadFailure covers the currentAfterRace branch whose re-read still finds no current period.
func TestEnsureCurrent_SurfacesTheRereadFailure(t *testing.T) {
	f := newFixture(t)
	f.store.SaveFn = func(_ context.Context, p *period.Period) error {
		return &period.OverlapError{Start: p.StartDate.String()}
	}

	_, err := f.svc.EnsureCurrent(f.ctx)
	assert.True(t, period.IsNotFoundError(err), "got %T: %v", err, err)
}

func TestClose_RejectsAlreadyClosed(t *testing.T) {
	f := newFixture(t)
	p1, err := f.svc.EnsureCurrent(f.ctx)
	require.NoError(t, err)
	_, err = f.svc.Close(f.ctx, p1.ID, date(2026, 9, 14), f.tc.UserID)
	require.NoError(t, err)

	_, err = f.svc.Close(f.ctx, p1.ID, date(2026, 9, 14), f.tc.UserID)
	assert.True(t, period.IsAlreadyClosedError(err), "got %T: %v", err, err)
}

func TestReopenThenReclose(t *testing.T) {
	f := newFixture(t)
	p1, err := f.svc.EnsureCurrent(f.ctx)
	require.NoError(t, err)
	f.setNow(t, 2026, 9, 25)
	_, err = f.svc.Close(f.ctx, p1.ID, date(2026, 9, 24), f.tc.UserID)
	require.NoError(t, err)

	reopened, err := f.svc.Reopen(f.ctx, p1.ID)
	require.NoError(t, err)
	assert.Equal(t, period.StatusOpen, reopened.Status)
	assert.False(t, reopened.IsLocked())
	require.NotNil(t, reopened.EndDate, "reopen keeps the end date")
	assert.Equal(t, date(2026, 9, 24), *reopened.EndDate)
	require.NotNil(t, reopened.Snapshot, "reopen keeps the now-stale snapshot")
	assert.Equal(t, 1, f.currentCount(t), "reopening a past period must not make it current")

	_, err = f.svc.Close(f.ctx, p1.ID, date(2026, 9, 23), f.tc.UserID)
	assert.True(t, period.IsInvalidRangeError(err), "got %T: %v", err, err)

	f.snap.snap.Net = 4_000_000
	reclosed, err := f.svc.Close(f.ctx, p1.ID, date(2026, 9, 24), f.tc.UserID)
	require.NoError(t, err)
	assert.Equal(t, period.StatusClosed, reclosed.Status)
	require.NotNil(t, reclosed.Snapshot)
	assert.Equal(t, int64(4_000_000), reclosed.Snapshot.Net, "re-close recomputes the snapshot")

	closings, err := f.svc.Closings(f.ctx, p1.ID)
	require.NoError(t, err)
	assert.Len(t, closings, 2, "each close appends a closing")
	assert.Equal(t, 1, f.currentCount(t), "re-close must not open another period")

	all, err := f.svc.List(f.ctx, period.ListOpts{})
	require.NoError(t, err)
	assert.Len(t, all, 2, "re-close creates no extra period")
}

func TestReopen_RejectsWhenALaterClosedPeriodExists(t *testing.T) {
	f := newFixture(t)
	p1, err := f.svc.EnsureCurrent(f.ctx)
	require.NoError(t, err)
	f.setNow(t, 2026, 9, 25)
	_, err = f.svc.Close(f.ctx, p1.ID, date(2026, 9, 24), f.tc.UserID)
	require.NoError(t, err)

	p2, err := f.svc.Current(f.ctx)
	require.NoError(t, err)
	f.setNow(t, 2026, 10, 25)
	_, err = f.svc.Close(f.ctx, p2.ID, date(2026, 10, 24), f.tc.UserID)
	require.NoError(t, err)

	_, err = f.svc.Reopen(f.ctx, p1.ID)
	assert.True(t, period.IsNotLatestClosedError(err), "got %T: %v", err, err)

	_, err = f.svc.Reopen(f.ctx, p2.ID)
	require.NoError(t, err, "the greatest-start closed period reopens")

	_, err = f.svc.Reopen(f.ctx, p1.ID)
	assert.True(t, period.IsNotLatestClosedError(err),
		"only one past period may be open at a time; got %T: %v", err, err)
}

func TestReopen_RejectsAnOpenPeriod(t *testing.T) {
	f := newFixture(t)
	p1, err := f.svc.EnsureCurrent(f.ctx)
	require.NoError(t, err)

	_, err = f.svc.Reopen(f.ctx, p1.ID)
	assert.True(t, period.IsNotClosedError(err), "got %T: %v", err, err)
}

func TestContaining(t *testing.T) {
	f := newFixture(t)

	p1, err := f.svc.Containing(f.ctx, f.now)
	require.NoError(t, err, "Containing ensures the first period exists")
	assert.Equal(t, date(2026, 8, 25), p1.StartDate)

	inside, err := f.svc.Containing(f.ctx, time.Date(2026, 9, 1, 3, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	assert.Equal(t, p1.ID, inside.ID)

	onStart, err := f.svc.Containing(f.ctx, time.Date(2026, 8, 25, 0, 30, 0, 0, jakarta(t)))
	require.NoError(t, err)
	assert.Equal(t, p1.ID, onStart.ID)

	_, err = f.svc.Containing(f.ctx, time.Date(2026, 8, 24, 12, 0, 0, 0, jakarta(t)))
	assert.True(t, period.IsNotFoundError(err), "a date before the first period has none; got %T: %v", err, err)
}

func TestContaining_UsesProjectTimezone(t *testing.T) {
	f := newFixture(t)
	_, err := f.svc.EnsureCurrent(f.ctx)
	require.NoError(t, err)
	f.setNow(t, 2026, 9, 25)
	p1, err := f.svc.Current(f.ctx)
	require.NoError(t, err)
	_, err = f.svc.Close(f.ctx, p1.ID, date(2026, 9, 24), f.tc.UserID)
	require.NoError(t, err)

	// 2026-09-24T18:00Z is 2026-09-25T01:00 in Jakarta, so it belongs to the next period.
	got, err := f.svc.Containing(f.ctx, time.Date(2026, 9, 24, 18, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	assert.Equal(t, date(2026, 9, 25), got.StartDate)
}

// TestClose_ComputesTheSnapshotInsideTheUnitOfWork pins where the totals are frozen relative to the transaction that writes them.
func TestClose_ComputesTheSnapshotInsideTheUnitOfWork(t *testing.T) {
	f := newFixture(t)
	p1, err := f.svc.EnsureCurrent(f.ctx)
	require.NoError(t, err)
	f.setNow(t, 2026, 9, 25)

	_, err = f.svc.Close(f.ctx, p1.ID, date(2026, 9, 24), f.tc.UserID)
	require.NoError(t, err)
	assert.Equal(t, 1, f.snap.callCount())
	assert.True(t, f.snapInsideUOW, "the snapshot must be computed inside the unit of work that writes it")
}

// TestPreviewClose_StaysOutsideTheUnitOfWork is the counterpart: a read-only preview opens no transaction.
func TestPreviewClose_StaysOutsideTheUnitOfWork(t *testing.T) {
	f := newFixture(t)
	p1, err := f.svc.EnsureCurrent(f.ctx)
	require.NoError(t, err)

	_, err = f.svc.PreviewClose(f.ctx, p1.ID, date(2026, 9, 14))
	require.NoError(t, err)
	assert.Equal(t, 1, f.snap.callCount())
	assert.False(t, f.snapInsideUOW)
	assert.Equal(t, 0, f.uowCalls, "a preview must not open a unit of work")
}

func TestPreviewClose_DoesNotPersist(t *testing.T) {
	f := newFixture(t)
	p1, err := f.svc.EnsureCurrent(f.ctx)
	require.NoError(t, err)

	snap, err := f.svc.PreviewClose(f.ctx, p1.ID, date(2026, 9, 14))
	require.NoError(t, err)
	assert.Equal(t, int64(3_750_000), snap.Net)

	reread, err := f.svc.ByID(f.ctx, p1.ID)
	require.NoError(t, err)
	assert.True(t, reread.IsCurrent())
	assert.Nil(t, reread.Snapshot)

	_, err = f.svc.PreviewClose(f.ctx, p1.ID, date(2026, 9, 16))
	assert.True(t, period.IsInvalidRangeError(err), "got %T: %v", err, err)
}

func TestServiceRename(t *testing.T) {
	f := newFixture(t)
	p1, err := f.svc.EnsureCurrent(f.ctx)
	require.NoError(t, err)

	renamed, err := f.svc.Rename(f.ctx, p1.ID, "Gajian Agustus")
	require.NoError(t, err)
	assert.Equal(t, "Gajian Agustus", renamed.Name)

	reread, err := f.svc.ByID(f.ctx, p1.ID)
	require.NoError(t, err)
	assert.Equal(t, "Gajian Agustus", reread.Name)

	_, err = f.svc.Rename(f.ctx, p1.ID, "")
	assert.True(t, period.IsInvalidNameError(err), "got %T: %v", err, err)
}

func TestNeighbors(t *testing.T) {
	f := newFixture(t)
	p1, err := f.svc.EnsureCurrent(f.ctx)
	require.NoError(t, err)
	f.setNow(t, 2026, 9, 25)
	_, err = f.svc.Close(f.ctx, p1.ID, date(2026, 9, 24), f.tc.UserID)
	require.NoError(t, err)
	p2, err := f.svc.Current(f.ctx)
	require.NoError(t, err)

	prev, next, err := f.svc.Neighbors(f.ctx, p1.ID)
	require.NoError(t, err)
	assert.Nil(t, prev)
	require.NotNil(t, next)
	assert.Equal(t, p2.ID, next.ID)

	prev, next, err = f.svc.Neighbors(f.ctx, p2.ID)
	require.NoError(t, err)
	require.NotNil(t, prev)
	assert.Equal(t, p1.ID, prev.ID)
	assert.Nil(t, next)
}

func TestList_OrdersByStartDateDescending(t *testing.T) {
	f := newFixture(t)
	p1, err := f.svc.EnsureCurrent(f.ctx)
	require.NoError(t, err)
	f.setNow(t, 2026, 9, 25)
	_, err = f.svc.Close(f.ctx, p1.ID, date(2026, 9, 24), f.tc.UserID)
	require.NoError(t, err)

	all, err := f.svc.List(f.ctx, period.ListOpts{})
	require.NoError(t, err)
	require.Len(t, all, 2)
	assert.Equal(t, date(2026, 9, 25), all[0].StartDate)
	assert.Equal(t, date(2026, 8, 25), all[1].StartDate)

	limited, err := f.svc.List(f.ctx, period.ListOpts{Limit: 1})
	require.NoError(t, err)
	assert.Len(t, limited, 1)

	before := date(2026, 9, 25)
	older, err := f.svc.List(f.ctx, period.ListOpts{Before: &before})
	require.NoError(t, err)
	require.Len(t, older, 1)
	assert.Equal(t, p1.ID, older[0].ID)
}

// NOTE: the fake deliberately does not filter by scope, so the service's own guard is the only thing this test can detect the removal of.
func TestByID_RejectsSiblingProject(t *testing.T) {
	f := newFixture(t)
	p1, err := f.svc.EnsureCurrent(f.ctx)
	require.NoError(t, err)

	raw, err := f.store.ByID(f.ctx, p1.ID)
	require.NoError(t, err)
	require.NotNil(t, raw, "the fake store must not filter by scope, or this test proves nothing")

	sibling := tenant.Into(t.Context(), tenant.Context{
		OrgID: f.tc.OrgID, ProjectID: uuid.New(), UserID: f.tc.UserID,
	})
	_, err = f.svc.ByID(sibling, p1.ID)
	assert.True(t, period.IsNotFoundError(err), "got %T: %v", err, err)

	otherOrg := tenant.Into(t.Context(), tenant.Context{
		OrgID: uuid.New(), ProjectID: f.tc.ProjectID, UserID: f.tc.UserID,
	})
	_, err = f.svc.ByID(otherOrg, p1.ID)
	assert.True(t, period.IsNotFoundError(err), "got %T: %v", err, err)

	for _, tt := range []struct {
		name string
		call func(ctx context.Context) error
	}{
		{"Rename", func(ctx context.Context) error { _, e := f.svc.Rename(ctx, p1.ID, "x"); return e }},
		{"Close", func(ctx context.Context) error {
			_, e := f.svc.Close(ctx, p1.ID, date(2026, 9, 14), f.tc.UserID)
			return e
		}},
		{"Reopen", func(ctx context.Context) error { _, e := f.svc.Reopen(ctx, p1.ID); return e }},
		{"PreviewClose", func(ctx context.Context) error {
			_, e := f.svc.PreviewClose(ctx, p1.ID, date(2026, 9, 14))
			return e
		}},
		{"Neighbors", func(ctx context.Context) error { _, _, e := f.svc.Neighbors(ctx, p1.ID); return e }},
		{"Closings", func(ctx context.Context) error { _, e := f.svc.Closings(ctx, p1.ID); return e }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.True(t, period.IsNotFoundError(tt.call(sibling)), "%s leaks a sibling project's row", tt.name)
		})
	}
}

func TestEnsureCurrent_RequiresTenantScope(t *testing.T) {
	f := newFixture(t)
	_, err := f.svc.EnsureCurrent(t.Context())
	assert.Error(t, err)
}

func TestSuggestedEnd_HonoursTheConfiguredStartDay(t *testing.T) {
	f := newFixture(t)
	f.settings.startDay = 1
	f.setNow(t, 2026, 3, 1)
	p1, err := f.svc.EnsureCurrent(f.ctx)
	require.NoError(t, err)
	require.Equal(t, date(2026, 3, 1), p1.StartDate)

	f.setNow(t, 2026, 9, 16)
	got, err := f.svc.SuggestedEnd(f.ctx, p1.ID)
	require.NoError(t, err)
	assert.Equal(t, date(2026, 3, 31), got)

	f.settings.startDay = 25
	got, err = f.svc.SuggestedEnd(f.ctx, p1.ID)
	require.NoError(t, err)
	assert.Equal(t, date(2026, 3, 24), got,
		"moving the payday to the 25th must move the suggested close date with it")
}

func TestSuggestedEnd_ReturnsTheFrozenEndAfterReopen(t *testing.T) {
	f := newFixture(t)
	p1, err := f.svc.EnsureCurrent(f.ctx)
	require.NoError(t, err)
	f.setNow(t, 2026, 9, 25)
	_, err = f.svc.Close(f.ctx, p1.ID, date(2026, 9, 24), f.tc.UserID)
	require.NoError(t, err)
	_, err = f.svc.Reopen(f.ctx, p1.ID)
	require.NoError(t, err)

	got, err := f.svc.SuggestedEnd(f.ctx, p1.ID)
	require.NoError(t, err)
	assert.Equal(t, date(2026, 9, 24), got)
}

func TestReopenable_MatchesWhatReopenAccepts(t *testing.T) {
	f := newFixture(t)
	none, err := f.svc.Reopenable(f.ctx)
	require.NoError(t, err)
	assert.Nil(t, none)

	p1, err := f.svc.EnsureCurrent(f.ctx)
	require.NoError(t, err)
	f.setNow(t, 2026, 9, 25)
	_, err = f.svc.Close(f.ctx, p1.ID, date(2026, 9, 24), f.tc.UserID)
	require.NoError(t, err)

	got, err := f.svc.Reopenable(f.ctx)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, p1.ID, got.ID)

	p2, err := f.svc.Current(f.ctx)
	require.NoError(t, err)
	f.setNow(t, 2026, 10, 25)
	_, err = f.svc.Close(f.ctx, p2.ID, date(2026, 10, 24), f.tc.UserID)
	require.NoError(t, err)

	got, err = f.svc.Reopenable(f.ctx)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, p2.ID, got.ID)

	_, err = f.svc.Reopen(f.ctx, p2.ID)
	require.NoError(t, err)
	got, err = f.svc.Reopenable(f.ctx)
	require.NoError(t, err)
	assert.Nil(t, got, "nothing may be reopened while a past period is already open")
}

func TestClose_RecheckesInsideTheUnitOfWork(t *testing.T) {
	f := newFixture(t)
	p1, err := f.svc.EnsureCurrent(f.ctx)
	require.NoError(t, err)
	f.setNow(t, 2026, 9, 25)
	_, err = f.svc.Close(f.ctx, p1.ID, date(2026, 9, 24), f.tc.UserID)
	require.NoError(t, err)
	_, err = f.svc.Reopen(f.ctx, p1.ID)
	require.NoError(t, err)

	f.beforeUOW = func() {
		f.beforeUOW = nil
		_, raceErr := f.svc.Close(f.ctx, p1.ID, date(2026, 9, 24), f.tc.UserID)
		require.NoError(t, raceErr, "the winning close must commit before the loser re-reads")
	}
	_, err = f.svc.Close(f.ctx, p1.ID, date(2026, 9, 24), f.tc.UserID)
	assert.True(t, period.IsAlreadyClosedError(err),
		"a close that lost the race must be refused inside the unit of work; got %T: %v", err, err)

	closings, err := f.svc.Closings(f.ctx, p1.ID)
	require.NoError(t, err)
	assert.Len(t, closings, 2, "the losing request must not append a third closing")
}
