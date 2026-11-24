package audit_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"altalune.id/yasaku/internal/platform/audit"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/schema"
)

func newSQLiteRecorder(t *testing.T) (*audit.Recorder, db.Pool, string) {
	t.Helper()
	conn, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "audit.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	cfg := config.Defaults()
	require.NoError(t, schema.MigrateUp(context.Background(), conn, cfg))
	pool := db.Pool{W: conn, R: conn}
	return audit.NewRecorder(db.DBConfig{Driver: db.DriverSQLite, TablePrefix: cfg.DB.TablePrefix}, pool, nil), pool, cfg.DB.TablePrefix
}

func scoped(orgID uuid.UUID) context.Context {
	ctx := tenant.Into(context.Background(), tenant.Context{OrgID: orgID})
	return session.PrincipalInto(ctx, session.Principal{UserID: uuid.New(), Email: "owner@example.com"})
}

func TestRecord_RefusesOutsideUnitOfWork(t *testing.T) {
	rec, _, _ := newSQLiteRecorder(t)
	err := rec.Record(scoped(uuid.New()), audit.Event{Action: "invite.sent"})
	assert.True(t, audit.IsNoUnitOfWorkError(err), "got %T: %v", err, err)
}

func TestRecord_RedactsDetail(t *testing.T) {
	rec, pool, _ := newSQLiteRecorder(t)
	orgID := uuid.New()
	ctx := scoped(orgID)
	require.NoError(t, db.RunInTx(ctx, pool, func(ctx context.Context) error {
		return rec.Record(ctx, audit.Event{Action: "apikey.created", TargetKind: "api_key", TargetID: "k1",
			Detail: map[string]string{"name": "ci", "secret": "sk_live_123", "token": "abc"}})
	}))
	entries, err := rec.List(ctx, audit.Filter{})
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, map[string]string{"name": "ci"}, entries[0].Detail, "keys off the allowlist never reach the row")
	assert.Equal(t, audit.ActorUser, entries[0].ActorKind)
	assert.Equal(t, "owner@example.com", entries[0].ActorEmail)
}

func TestAuditEvents_AppendOnlySQLite(t *testing.T) {
	rec, pool, prefix := newSQLiteRecorder(t)
	ctx := scoped(uuid.New())
	require.NoError(t, db.RunInTx(ctx, pool, func(ctx context.Context) error {
		return rec.Record(ctx, audit.Event{Action: "org.renamed"})
	}))
	_, err := pool.W.ExecContext(ctx, "UPDATE "+prefix+"audit_events SET action = 'tampered'")
	require.ErrorContains(t, err, "append-only")
	_, err = pool.W.ExecContext(ctx, "DELETE FROM "+prefix+"audit_events")
	require.ErrorContains(t, err, "append-only")
}

func TestList_OwnOrgOnlyNewestFirst(t *testing.T) {
	rec, pool, _ := newSQLiteRecorder(t)
	own, other := uuid.New(), uuid.New()
	for _, a := range []string{"first", "second"} {
		require.NoError(t, db.RunInTx(scoped(own), pool, func(ctx context.Context) error {
			return rec.Record(ctx, audit.Event{Action: a})
		}))
	}
	require.NoError(t, db.RunInTx(scoped(other), pool, func(ctx context.Context) error {
		return rec.Record(ctx, audit.Event{Action: "foreign"})
	}))
	entries, err := rec.List(scoped(own), audit.Filter{})
	require.NoError(t, err)
	require.Len(t, entries, 2)
	assert.Equal(t, "second", entries[0].Action)
}
