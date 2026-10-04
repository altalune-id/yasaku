package opensheetsync_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	sqlitedrv "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"altalune.id/yasaku/internal/opensheetsync"
	"altalune.id/yasaku/internal/platform/db"
)

func sqliteBusy(err error) (int, bool) {
	var se *sqlitedrv.Error
	if !errors.As(err, &se) {
		return 0, false
	}
	return se.Code(), se.Code()&0xff == sqlite3.SQLITE_BUSY || se.Code()&0xff == sqlite3.SQLITE_LOCKED
}

// NOTE: jobs claim, settle and release on their own deferred transactions while writers mark inside IMMEDIATE units of work, as Record does in production; a store method that read before it wrote would fail here with SQLITE_BUSY_SNAPSHOT (517).
func TestSQLiteStore_ConcurrentClaimSettleAndMarkNeverBusy(t *testing.T) {
	f, pool := newSQLiteFixture(t)
	runConcurrentClaimSettleAndMark(t, f, func(ctx context.Context, fn func(context.Context) error) error {
		return db.RunInTx(ctx, pool, fn)
	})
}

// NOTE: unitOfWork is the driver's write transaction, the one Record runs Mark in.
func runConcurrentClaimSettleAndMark(t *testing.T, f storeFixture, unitOfWork func(context.Context, func(context.Context) error) error) {
	t.Helper()
	ctx := f.ctx(f.tc)
	at := time.Now().UTC()
	l := linkFor(f.tc, at)
	require.NoError(t, l.Enable(at))
	require.NoError(t, f.store.SaveLink(ctx, l))
	refs := make([]opensheetsync.Ref, 12)
	for i := range refs {
		refs[i] = ref(opensheetsync.EntityWallet, uuid.New())
	}
	require.NoError(t, f.store.Mark(ctx, f.tc.OrgID, f.tc.ProjectID, refs, at))

	const jobs, writers, rounds = 8, 4, 25
	var (
		mu   sync.Mutex
		errs []error
		wg   sync.WaitGroup
	)
	fail := func(who string, err error) {
		if err == nil {
			return
		}
		if code, busy := sqliteBusy(err); busy {
			err = fmt.Errorf("%s: sqlite busy (extended code %d): %w", who, code, err)
		}
		mu.Lock()
		defer mu.Unlock()
		errs = append(errs, err)
	}
	for j := range jobs {
		wg.Go(func() {
			for r := range rounds {
				for i, rf := range refs {
					tok := uuid.New()
					st, ok, err := f.store.Claim(ctx, f.tc.OrgID, f.tc.ProjectID, rf, tok, time.Now(), time.Minute)
					if err != nil {
						fail("claim", err)
						return
					}
					if !ok {
						continue
					}
					if (i+j+r)%5 == 0 {
						fail("release", f.store.Release(ctx, f.tc.OrgID, f.tc.ProjectID, rf, tok, opensheetsync.Failure{Reason: "transient", At: time.Now()}))
						continue
					}
					_, _, err = f.store.Settle(ctx, f.tc.OrgID, f.tc.ProjectID, rf, st.Version, tok)
					fail("settle", err)
				}
			}
		})
	}
	for w := range writers {
		wg.Go(func() {
			for r := range rounds {
				err := unitOfWork(ctx, func(ctx context.Context) error {
					on, err := f.store.LinkEnabled(ctx, f.tc.OrgID, f.tc.ProjectID)
					if err != nil || !on {
						return fmt.Errorf("link enabled = %v: %w", on, err)
					}
					return f.store.Mark(ctx, f.tc.OrgID, f.tc.ProjectID, []opensheetsync.Ref{refs[(w+r)%len(refs)], refs[(w+2*r+1)%len(refs)]}, time.Now())
				})
				fail("mark", err)
			}
		})
	}
	wg.Wait()
	require.Empty(t, errs)

	for range 3 {
		for _, rf := range refs {
			tok := uuid.New()
			st, ok, err := f.store.Claim(ctx, f.tc.OrgID, f.tc.ProjectID, rf, tok, time.Now(), time.Minute)
			require.NoError(t, err)
			if ok {
				_, _, err = f.store.Settle(ctx, f.tc.OrgID, f.tc.ProjectID, rf, st.Version, tok)
				require.NoError(t, err)
			}
		}
	}
	b, err := f.store.Backlog(ctx, f.tc.OrgID, f.tc.ProjectID)
	require.NoError(t, err)
	require.Zero(t, b.Pending, "every row drains once the writers stop")
}
