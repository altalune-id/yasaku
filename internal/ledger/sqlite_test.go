package ledger_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	"altalune.id/yasaku/internal/ledger"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/db"
	sqliteent "altalune.id/yasaku/internal/platform/db/entity/sqlite"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/money"
	"altalune.id/yasaku/schema"
)

func newSQLiteDB(t *testing.T) (*sql.DB, *config.Config) {
	t.Helper()
	ctx := context.Background()
	cfg := config.Defaults()
	cfg.DB.Driver = db.DriverSQLite
	cfg.DB.DSN = filepath.Join(t.TempDir(), "ledger.db")

	sqlDB, err := db.Open(ctx, cfg.DB, nil)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := schema.MigrateUp(ctx, sqlDB, cfg); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	return sqlDB, cfg
}

func newSQLiteStoreForTest(t *testing.T) (ledger.Store, tenant.Context) {
	t.Helper()
	store, _, _, tc := newSQLiteFixture(t)
	return store, tc
}

func newSQLiteFixture(t *testing.T) (ledger.Store, *sql.DB, string, tenant.Context) {
	t.Helper()
	sqlDB, cfg := newSQLiteDB(t)
	uid, oid, pid := seedTenant(t, sqlDB, cfg.DB.TablePrefix)
	store := ledger.NewStore(
		db.DBConfig{Driver: db.DriverSQLite, TablePrefix: cfg.DB.TablePrefix},
		db.Pool{W: sqlDB, R: sqlDB},
		nil,
	)
	return store, sqlDB, cfg.DB.TablePrefix, tenant.Context{OrgID: oid, ProjectID: pid, UserID: uid}
}

func seedTenant(t *testing.T, sqlDB *sql.DB, prefix string) (userID, orgID, projID uuid.UUID) { //nolint:nonamedreturns // triple
	t.Helper()
	userID = uuid.New()
	orgID = uuid.New()
	projID = uuid.New()
	now := sqliteent.SQLiteTime(time.Now())
	if _, err := sqlDB.Exec(
		"INSERT INTO "+prefix+"users (id, email, name, avatar_url, is_admin, created_at, updated_at) "+
			"VALUES (?, ?, '', '', 0, ?, ?)",
		userID.String(), userID.String()+"@x.com", now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlDB.Exec(
		"INSERT INTO "+prefix+"orgs (id, slug, name, created_by, created_at, updated_at) "+
			"VALUES (?, ?, 'Org', ?, ?, ?)",
		orgID.String(), orgID.String()[:8], userID.String(), now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlDB.Exec(
		"INSERT INTO "+prefix+"projects (id, org_id, slug, name, created_by, created_at, updated_at) "+
			"VALUES (?, ?, ?, 'Web', ?, ?, ?)",
		projID.String(), orgID.String(), projID.String()[:8], userID.String(), now, now); err != nil {
		t.Fatal(err)
	}
	return
}

func TestSQLiteStore_SaveAndByProject(t *testing.T) {
	store, tc := newSQLiteStoreForTest(t)
	ctx := tenant.Into(context.Background(), tc)

	want := ledger.Defaults(tc.OrgID, tc.ProjectID)
	if err := want.Apply(ledger.Patch{
		Timezone:       ptr("Asia/Tokyo"),
		Currency:       ptr(money.Currency("USD")),
		PeriodStartDay: ptr(25),
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(ctx, want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := store.ByProject(ctx, tc.OrgID, tc.ProjectID)
	if err != nil {
		t.Fatalf("ByProject: %v", err)
	}
	if got.OrgID != want.OrgID || got.ProjectID != want.ProjectID {
		t.Errorf("scope round-trip: %+v", *got)
	}
	if got.Timezone != "Asia/Tokyo" || got.Currency != money.Currency("USD") || got.PeriodStartDay != 25 {
		t.Errorf("value round-trip: %+v", *got)
	}
	if !got.UpdatedAt.Equal(want.UpdatedAt) {
		t.Errorf("UpdatedAt round-trip: got=%v want=%v", got.UpdatedAt, want.UpdatedAt)
	}
}

func TestSQLiteStore_SaveIsUpsert(t *testing.T) {
	store, tc := newSQLiteStoreForTest(t)
	ctx := tenant.Into(context.Background(), tc)

	st := ledger.Defaults(tc.OrgID, tc.ProjectID)
	if err := st.Apply(ledger.Patch{Timezone: ptr("Asia/Jakarta")}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(ctx, st); err != nil {
		t.Fatalf("first Save: %v", err)
	}
	first := st.UpdatedAt

	if err := st.Apply(ledger.Patch{Timezone: ptr("Europe/Berlin"), PeriodStartDay: ptr(10)}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(ctx, st); err != nil {
		t.Fatalf("second Save: %v", err)
	}

	got, err := store.ByProject(ctx, tc.OrgID, tc.ProjectID)
	if err != nil {
		t.Fatalf("ByProject: %v", err)
	}
	if got.Timezone != "Europe/Berlin" {
		t.Errorf("Timezone=%q want Europe/Berlin", got.Timezone)
	}
	if got.PeriodStartDay != 10 {
		t.Errorf("PeriodStartDay=%d want 10", got.PeriodStartDay)
	}
	if got.UpdatedAt.Before(first) {
		t.Errorf("UpdatedAt went backwards on upsert: got=%v first=%v", got.UpdatedAt, first)
	}
}

func TestSQLiteStore_ByProject_NotFound(t *testing.T) {
	store, tc := newSQLiteStoreForTest(t)
	ctx := tenant.Into(context.Background(), tc)

	_, err := store.ByProject(ctx, tc.OrgID, uuid.New())
	if !ledger.IsNotFoundError(err) {
		t.Errorf("want IsNotFoundError, got %T %v", err, err)
	}
}

func TestSQLiteStore_ByProject_ForeignOrgIsInvisible(t *testing.T) {
	store, tc := newSQLiteStoreForTest(t)
	ctx := tenant.Into(context.Background(), tc)
	seeded := ledger.Defaults(tc.OrgID, tc.ProjectID)
	if err := seeded.Apply(ledger.Patch{Timezone: ptr("Asia/Jakarta")}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(ctx, seeded); err != nil {
		t.Fatal(err)
	}

	_, err := store.ByProject(ctx, uuid.New(), tc.ProjectID)
	if !ledger.IsNotFoundError(err) {
		t.Errorf("another org's read: want IsNotFoundError, got %T %v", err, err)
	}
}

func TestSQLiteStore_TenantMissing(t *testing.T) {
	store, tc := newSQLiteStoreForTest(t)

	if err := store.Save(context.Background(), ledger.Defaults(tc.OrgID, tc.ProjectID)); !tenant.IsMissingError(err) {
		t.Errorf("Save without tenant: want MissingError, got %T %v", err, err)
	}
	if _, err := store.ByProject(context.Background(), tc.OrgID, tc.ProjectID); !tenant.IsMissingError(err) {
		t.Errorf("ByProject without tenant: want MissingError, got %T %v", err, err)
	}
}

// TestSQLiteStore_Save_EnrollsInTheCallersUnitOfWork drives Save inside a real db.RunInTx whose
// later step fails. If Save opened its own transaction instead of enrolling, its row would have
// been committed independently and would survive the unit of work's rollback.
func TestSQLiteStore_Save_EnrollsInTheCallersUnitOfWork(t *testing.T) {
	store, sqlDB, prefix, tc := newSQLiteFixture(t)
	ctx := tenant.Into(context.Background(), tc)

	st := ledger.Defaults(tc.OrgID, tc.ProjectID)
	if err := st.Apply(ledger.Patch{Timezone: ptr("Asia/Tokyo"), PeriodStartDay: ptr(25)}); err != nil {
		t.Fatal(err)
	}

	laterStepFailed := errors.New("a later step in the unit of work failed")
	err := db.RunInTx(ctx, db.Pool{W: sqlDB, R: sqlDB}, func(ctx context.Context) error {
		if saveErr := store.Save(ctx, st); saveErr != nil {
			return saveErr
		}
		tx, ok := db.CurrentTx(ctx)
		if !ok {
			t.Fatal("RunInTx did not expose a unit of work on the context")
		}
		// period_start_day violates the table's CHECK, so this write really does fail.
		_, probeErr := tx.ExecContext(ctx,
			"INSERT INTO "+prefix+"ledger_settings (project_id, org_id, timezone, currency, period_start_day, updated_at) "+
				"VALUES (?, ?, 'Asia/Jakarta', 'IDR', 99, '2026-01-01T00:00:00.000000000Z')",
			uuid.New().String(), tc.OrgID.String())
		if probeErr == nil {
			t.Fatal("the probe write must fail, otherwise this test proves nothing about rollback")
		}
		return laterStepFailed
	})
	if !errors.Is(err, laterStepFailed) {
		t.Fatalf("RunInTx err = %v, want %v", err, laterStepFailed)
	}

	if _, byErr := store.ByProject(ctx, tc.OrgID, tc.ProjectID); !ledger.IsNotFoundError(byErr) {
		t.Fatalf("the settings row survived the unit of work's rollback, so Save did not enroll in it: got %T %v", byErr, byErr)
	}
}

// TestSQLiteStore_Save_CommitsWithTheCallersUnitOfWork is the positive control: enrolling must
// not stop a successful unit of work from persisting the row.
func TestSQLiteStore_Save_CommitsWithTheCallersUnitOfWork(t *testing.T) {
	store, sqlDB, _, tc := newSQLiteFixture(t)
	ctx := tenant.Into(context.Background(), tc)

	st := ledger.Defaults(tc.OrgID, tc.ProjectID)
	if err := st.Apply(ledger.Patch{Timezone: ptr("Asia/Tokyo"), PeriodStartDay: ptr(25)}); err != nil {
		t.Fatal(err)
	}

	if err := db.RunInTx(ctx, db.Pool{W: sqlDB, R: sqlDB}, func(ctx context.Context) error {
		return store.Save(ctx, st)
	}); err != nil {
		t.Fatalf("RunInTx: %v", err)
	}

	got, err := store.ByProject(ctx, tc.OrgID, tc.ProjectID)
	if err != nil {
		t.Fatalf("ByProject after a committed unit of work: %v", err)
	}
	if got.Timezone != "Asia/Tokyo" || got.PeriodStartDay != 25 {
		t.Errorf("round-trip through the unit of work: %+v", *got)
	}
}

// TestSQLiteStore_ByProject_ReadsTheCallersUncommittedWrite pins that the read path enrolls too:
// a read inside the unit of work must see that unit's own not-yet-committed write.
func TestSQLiteStore_ByProject_ReadsTheCallersUncommittedWrite(t *testing.T) {
	store, sqlDB, _, tc := newSQLiteFixture(t)
	ctx := tenant.Into(context.Background(), tc)

	st := ledger.Defaults(tc.OrgID, tc.ProjectID)
	if err := st.Apply(ledger.Patch{Timezone: ptr("Asia/Tokyo")}); err != nil {
		t.Fatal(err)
	}

	if err := db.RunInTx(ctx, db.Pool{W: sqlDB, R: sqlDB}, func(ctx context.Context) error {
		if saveErr := store.Save(ctx, st); saveErr != nil {
			return saveErr
		}
		got, byErr := store.ByProject(ctx, tc.OrgID, tc.ProjectID)
		if byErr != nil {
			return fmt.Errorf("ByProject inside the unit of work: %w", byErr)
		}
		if got.Timezone != "Asia/Tokyo" {
			return fmt.Errorf("read its own write as %q, want Asia/Tokyo", got.Timezone)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
