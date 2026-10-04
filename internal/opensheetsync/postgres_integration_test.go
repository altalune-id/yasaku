//go:build integration

package opensheetsync_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/opensheetsync"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/testutil/pgtest"
	"altalune.id/yasaku/schema"
)

// NOTE: AllowBypassRLS makes the explicit org predicate the only guard, so the hijack cases in the contract prove the code, not RLS.
func TestPostgresStore_Contract(t *testing.T) {
	runStoreContract(t, newPgFixture)
}

func TestPostgresStore_ConcurrentClaimSettleAndMark(t *testing.T) {
	f, pc := newPgFixtureWithConn(t)
	runConcurrentClaimSettleAndMark(t, f, func(ctx context.Context, fn func(context.Context) error) error {
		return tenant.RunInTx(ctx, pc, f.tc, fn)
	})
}

// NOTE: deterministic: the write holds its unit of work open between LinkEnabled and Mark while Remove runs; the share lock is what makes Remove wait.
func TestPostgresStore_LinkEnabledInAUnitOfWorkHoldsOffRemove(t *testing.T) {
	f, pc := newPgFixtureWithConn(t)
	ctx := tenant.Into(context.Background(), f.tc)
	l := linkFor(f.tc, time.Now())
	require.NoError(t, l.Enable(time.Now()))
	require.NoError(t, f.store.SaveLink(ctx, l))

	removed := make(chan error, 1)
	err := tenant.RunInTx(ctx, pc, f.tc, func(ctx context.Context) error {
		on, err := f.store.LinkEnabled(ctx, f.tc.OrgID, f.tc.ProjectID)
		if err != nil || !on {
			return fmt.Errorf("link enabled = %v: %w", on, err)
		}
		go func() {
			removed <- f.store.DeleteLink(tenant.Into(context.Background(), f.tc), f.tc.OrgID, f.tc.ProjectID)
		}()
		select {
		case err := <-removed:
			return fmt.Errorf("remove did not wait for the write: %w", err)
		case <-time.After(300 * time.Millisecond):
		}
		return f.store.Mark(ctx, f.tc.OrgID, f.tc.ProjectID, []opensheetsync.Ref{ref(opensheetsync.EntityWallet, uuid.New())}, time.Now())
	})
	require.NoError(t, err, "the write commits with its state row")
	require.NoError(t, <-removed, "then Remove proceeds and cascades the new row")
	b, err := f.store.Backlog(ctx, f.tc.OrgID, f.tc.ProjectID)
	require.NoError(t, err)
	require.Zero(t, b.Pending)
}

// NOTE: writers hand Mark the same rows in opposite orders; Mark locks them in LockOrder, so no round deadlocks (40P01).
func TestPostgresStore_MarkInOppositeOrdersNeverDeadlocks(t *testing.T) {
	f, pc := newPgFixtureWithConn(t)
	ctx := f.ctx(f.tc)
	at := time.Now().UTC()
	require.NoError(t, f.store.SaveLink(ctx, linkFor(f.tc, at)))
	asc := make([]opensheetsync.Ref, 0, 24)
	for _, e := range []opensheetsync.Entity{opensheetsync.EntityTransaction, opensheetsync.EntityCategory, opensheetsync.EntityWallet} {
		for range 8 {
			asc = append(asc, ref(e, uuid.New()))
		}
	}
	require.NoError(t, f.store.Mark(ctx, f.tc.OrgID, f.tc.ProjectID, asc, at))
	desc := slices.Clone(asc)
	slices.Reverse(desc)

	const writers, rounds = 4, 20
	errs := make([]error, writers)
	var wg sync.WaitGroup
	for w := range writers {
		refs := asc
		if w%2 == 1 {
			refs = desc
		}
		wg.Go(func() {
			for range rounds {
				err := tenant.RunInTx(ctx, pc, f.tc, func(ctx context.Context) error {
					if _, err := f.store.LinkEnabled(ctx, f.tc.OrgID, f.tc.ProjectID); err != nil {
						return err
					}
					return f.store.Mark(ctx, f.tc.OrgID, f.tc.ProjectID, refs, time.Now())
				})
				if err != nil {
					errs[w] = err
					return
				}
			}
		})
	}
	wg.Wait()
	for w, err := range errs {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			require.NoError(t, err, "writer %d: SQLSTATE %s", w, pgErr.Code)
		}
		require.NoError(t, err, "writer %d", w)
	}
}

func TestPostgres_TheReconcilerDrainsWhatAnOutageOrALostKickLeftDirty(t *testing.T) {
	runReconcileScenario(t, newPgFixture(t))
}

func newPgFixture(t *testing.T) storeFixture {
	f, _ := newPgFixtureWithConn(t)
	return f
}

func newPgFixtureWithConn(t *testing.T) (storeFixture, *tenant.PgConn) {
	t.Helper()
	h := pgtest.New(t)
	sqlDB := h.OpenDB(t)
	cfg := config.Defaults()
	cfg.DB.Driver = db.DriverPostgres
	cfg.DB.DSN = h.DSN
	cfg.DB.Schema = h.Schema
	cfg.DB.AllowBypassRLS = true
	require.NoError(t, schema.MigrateUp(t.Context(), sqlDB, cfg))
	prefix := cfg.DB.TablePrefix
	q := func(table string) string { return h.Schema + "." + prefix + table }
	tc, sibling := seedPgTenant(t, sqlDB, q)
	other, _ := seedPgTenant(t, sqlDB, q)
	now := time.Now().UTC()
	exec := func(t *testing.T, stmt string, args ...any) {
		t.Helper()
		_, err := sqlDB.Exec(stmt, args...)
		require.NoError(t, err)
	}
	pc := tenant.NewPgConn(sqlDB)
	return storeFixture{
		store: opensheetsync.NewStore(db.DBConfig{Driver: db.DriverPostgres, Schema: h.Schema, TablePrefix: prefix},
			db.Pool{W: sqlDB, R: sqlDB}, pc),
		tc: tc, other: other, sibling: sibling,
		wallet: func(t *testing.T, tc tenant.Context) uuid.UUID {
			id := uuid.New()
			exec(t, "INSERT INTO "+q("wallets")+" (id, org_id, project_id, name, kind, currency, created_at, updated_at) VALUES ($1, $2, $3, $4, 'cash', 'IDR', $5, $5)",
				id, tc.OrgID, tc.ProjectID, "w-"+id.String()[:8], now)
			return id
		},
		category: func(t *testing.T, tc tenant.Context) uuid.UUID {
			id := uuid.New()
			exec(t, "INSERT INTO "+q("categories")+" (id, org_id, project_id, name, kind, created_at, updated_at) VALUES ($1, $2, $3, $4, 'expense', $5, $5)",
				id, tc.OrgID, tc.ProjectID, "c-"+id.String()[:8], now)
			return id
		},
		transaction: func(t *testing.T, tc tenant.Context, w, to, cat uuid.UUID) uuid.UUID {
			id := uuid.New()
			kind, toArg, catArg := "expense", any(nil), any(nil)
			if to != uuid.Nil {
				kind, toArg = "transfer", to
			}
			if cat != uuid.Nil {
				catArg = cat
			}
			exec(t, "INSERT INTO "+q("transactions")+" (id, org_id, project_id, wallet_id, to_wallet_id, kind, amount_minor, currency, category_id, occurred_at, created_by, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, 100, 'IDR', $7, $8, $9, $8, $8)",
				id, tc.OrgID, tc.ProjectID, w, toArg, kind, catArg, now, tc.UserID)
			return id
		},
		remove: func(t *testing.T, tc tenant.Context, e opensheetsync.Entity, id uuid.UUID) {
			exec(t, "DELETE FROM "+q(entityTable(e))+" WHERE id = $1 AND org_id = $2", id, tc.OrgID)
		},
	}, pc
}

func seedPgTenant(t *testing.T, sqlDB *sql.DB, q func(string) string) (tenant.Context, uuid.UUID) {
	t.Helper()
	userID, orgID, projID, sibling := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	now := time.Now().UTC()
	_, err := sqlDB.Exec("INSERT INTO "+q("users")+" (id, email, name, avatar_url, is_admin, created_at, updated_at) VALUES ($1, $2, '', '', false, $3, $3)",
		userID, userID.String()+"@example.com", now)
	require.NoError(t, err)
	_, err = sqlDB.Exec("INSERT INTO "+q("orgs")+" (id, slug, name, created_by, created_at, updated_at) VALUES ($1, $2, 'Org', $3, $4, $4)",
		orgID, orgID.String()[:8], userID, now)
	require.NoError(t, err)
	for _, p := range []uuid.UUID{projID, sibling} {
		_, err = sqlDB.Exec("INSERT INTO "+q("projects")+" (id, org_id, slug, name, created_by, created_at, updated_at) VALUES ($1, $2, $3, 'P', $4, $5, $5)",
			p, orgID, p.String()[:8], userID, now)
		require.NoError(t, err)
	}
	return tenant.Context{OrgID: orgID, ProjectID: projID, UserID: userID}, sibling
}
