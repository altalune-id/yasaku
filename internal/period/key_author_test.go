package period_test

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/civil"
	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/period"
	"altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/platform/tenant"
)

func newAuthorCloseService(t *testing.T, store period.Store, uow period.UnitOfWork) *period.Service {
	t.Helper()
	loc := jakarta(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return period.NewService(
		store, log, apperror.NewReporter(log, false).Unexpected,
		&fakeSettings{loc: loc, startDay: 25},
		&fakeSnapshotter{snap: sampleSnapshot()},
		uow,
		func() time.Time { return time.Date(2026, 9, 25, 10, 0, 0, 0, loc) },
	)
}

func keyPrincipalCtx(ctx context.Context, tc tenant.Context, keyID uuid.UUID) context.Context {
	ctx = session.PrincipalInto(ctx, session.Principal{KeyID: keyID, Source: session.SourceAPIKey})
	return tenant.Into(ctx, tenant.Context{OrgID: tc.OrgID, ProjectID: tc.ProjectID})
}

func personCtx(ctx context.Context, tc tenant.Context) context.Context {
	ctx = session.PrincipalInto(ctx, session.Principal{UserID: tc.UserID, Source: session.SourceLocal})
	return tenant.Into(ctx, tc)
}

type closingAuthor struct {
	closedBy sql.NullString
	keyID    sql.NullString
}

func readClosingAuthors(t *testing.T, sqlDB *sql.DB, query string, periodID uuid.UUID) []closingAuthor {
	t.Helper()
	rows, err := sqlDB.QueryContext(t.Context(), query, periodID.String())
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var out []closingAuthor
	for rows.Next() {
		var a closingAuthor
		require.NoError(t, rows.Scan(&a.closedBy, &a.keyID))
		out = append(out, a)
	}
	require.NoError(t, rows.Err())
	return out
}

func checkCloseKeyAuthor(t *testing.T, sqlDB *sql.DB, store period.Store, svc *period.Service, tc tenant.Context, keyID uuid.UUID, query string) {
	t.Helper()
	keyCtx := keyPrincipalCtx(t.Context(), tc, keyID)
	byKey := seedPeriod(keyCtx, t, store, tc, civil.Date{Year: 2026, Month: 7, Day: 25}, nil)
	_, err := svc.Close(keyCtx, byKey.ID, civil.Date{Year: 2026, Month: 8, Day: 24}, uuid.Nil)
	require.NoError(t, err)

	got := readClosingAuthors(t, sqlDB, query, byKey.ID)
	require.Len(t, got, 1)
	assert.False(t, got[0].closedBy.Valid, "a key close must have closed_by NULL, got %q", got[0].closedBy.String)
	require.True(t, got[0].keyID.Valid, "a key close must name its key")
	assert.Equal(t, keyID.String(), got[0].keyID.String)

	closings, err := store.ListClosings(keyCtx, tc.OrgID, tc.ProjectID, byKey.ID)
	require.NoError(t, err)
	require.Len(t, closings, 1)
	assert.Equal(t, keyID, closings[0].ClosedByKeyID, "the store must read the key author back")
	assert.Equal(t, uuid.Nil, closings[0].ClosedBy)

	pCtx := personCtx(t.Context(), tc)
	current, err := store.Current(pCtx, tc.OrgID, tc.ProjectID)
	require.NoError(t, err)
	_, err = svc.Close(pCtx, current.ID, civil.Date{Year: 2026, Month: 9, Day: 24}, tc.UserID)
	require.NoError(t, err)

	got = readClosingAuthors(t, sqlDB, query, current.ID)
	require.Len(t, got, 1)
	require.True(t, got[0].closedBy.Valid, "a person close must name its user")
	assert.Equal(t, tc.UserID.String(), got[0].closedBy.String)
	assert.False(t, got[0].keyID.Valid, "a person close must have closed_by_key_id NULL, got %q", got[0].keyID.String)
}

func TestSQLite_Period_CloseKeyAuthor(t *testing.T) {
	sqlDB, cfg := newSQLiteDB(t)
	prefix := cfg.DB.TablePrefix
	tc := seedTenant(t, sqlDB, prefix)
	keyID := uuid.New()
	_, err := sqlDB.Exec(
		"INSERT INTO "+prefix+"api_keys (id, org_id, project_id, name, secret_hash, created_at) VALUES (?, ?, ?, 'ci', x'01', ?)",
		keyID.String(), tc.OrgID.String(), tc.ProjectID.String(), "2026-08-01T00:00:00Z")
	require.NoError(t, err)
	pool := db.Pool{W: sqlDB, R: sqlDB}
	store := period.NewStore(db.DBConfig{Driver: db.DriverSQLite, TablePrefix: prefix}, pool, nil)
	svc := newAuthorCloseService(t, store, func(ctx context.Context, fn func(ctx context.Context) error) error {
		return db.RunInTx(ctx, pool, fn)
	})
	checkCloseKeyAuthor(t, sqlDB, store, svc, tc, keyID,
		"SELECT closed_by, closed_by_key_id FROM "+prefix+"period_closings WHERE period_id = ?")
}

func checkLegacyNilAuthorClosing(t *testing.T, sqlDB *sql.DB, store period.Store, svc *period.Service, tc tenant.Context, insertLegacy func(periodID uuid.UUID)) {
	t.Helper()
	pCtx := personCtx(t.Context(), tc)
	legacy := seedPeriod(pCtx, t, store, tc, civil.Date{Year: 2026, Month: 6, Day: 25}, &civil.Date{Year: 2026, Month: 7, Day: 24})
	insertLegacy(legacy.ID)
	closings, err := store.ListClosings(pCtx, tc.OrgID, tc.ProjectID, legacy.ID)
	require.NoError(t, err, "a legacy closing authored by the nil uuid must stay readable")
	require.Len(t, closings, 1)
	assert.Equal(t, uuid.Nil, closings[0].ClosedBy)
	assert.Equal(t, uuid.Nil, closings[0].ClosedByKeyID)

	open := seedPeriod(pCtx, t, store, tc, civil.Date{Year: 2026, Month: 7, Day: 25}, nil)
	noAuthor := tenant.Into(t.Context(), tenant.Context{OrgID: tc.OrgID, ProjectID: tc.ProjectID})
	_, err = svc.Close(noAuthor, open.ID, civil.Date{Year: 2026, Month: 8, Day: 24}, uuid.Nil)
	require.Error(t, err)
	assert.True(t, period.IsAuthorMissingError(err), "want AuthorMissingError, got %v", err)
	got, err := store.ByID(pCtx, open.ID)
	require.NoError(t, err)
	assert.Equal(t, period.StatusOpen, got.Status, "a refused close must roll back")
}

func TestSQLite_Period_CloseKeyAuthorLegacyNilAuthor(t *testing.T) {
	sqlDB, cfg := newSQLiteDB(t)
	prefix := cfg.DB.TablePrefix
	tc := seedTenant(t, sqlDB, prefix)
	pool := db.Pool{W: sqlDB, R: sqlDB}
	store := period.NewStore(db.DBConfig{Driver: db.DriverSQLite, TablePrefix: prefix}, pool, nil)
	svc := newAuthorCloseService(t, store, func(ctx context.Context, fn func(ctx context.Context) error) error {
		return db.RunInTx(ctx, pool, fn)
	})
	checkLegacyNilAuthorClosing(t, sqlDB, store, svc, tc, func(periodID uuid.UUID) {
		_, err := sqlDB.Exec(
			"INSERT INTO "+prefix+"period_closings (id, org_id, project_id, period_id, closed_at, closed_by, snapshot) VALUES (?, ?, ?, ?, ?, ?, '{}')",
			uuid.NewString(), tc.OrgID.String(), tc.ProjectID.String(), periodID.String(), "2026-07-25T03:00:00Z", uuid.Nil.String())
		require.NoError(t, err)
	})
}
