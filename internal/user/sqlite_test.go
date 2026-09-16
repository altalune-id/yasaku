package user_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	_ "modernc.org/sqlite"

	"altalune.id/yasaku/internal/platform/config"
	pdb "altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/user"
	"altalune.id/yasaku/schema"
)

func openMemSQLite(t *testing.T) (*sql.DB, pdb.DBConfig) {
	t.Helper()
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	cfg := config.Defaults()
	if err := schema.MigrateUp(context.Background(), db, cfg); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db, pdb.DBConfig{
		Driver:      pdb.DriverSQLite,
		DSN:         ":memory:",
		TablePrefix: cfg.DB.TablePrefix,
	}
}

func TestSQLiteStore_SaveAndLookup(t *testing.T) {
	t.Parallel()
	db, dbcfg := openMemSQLite(t)
	store := user.NewStore(dbcfg, pdb.Pool{W: db, R: db})

	u, err := user.New("Alice@Example.com", "Alice", user.SourceGenesis)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(context.Background(), u); err != nil {
		t.Fatalf("Save: %v", err)
	}

	byID, err := store.ByID(context.Background(), u.ID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if byID.Email != "alice@example.com" {
		t.Errorf("email=%q", byID.Email)
	}
	if byID.Source != user.SourceGenesis {
		t.Errorf("source=%q", byID.Source)
	}

	byEmail, err := store.ByEmail(context.Background(), "Alice@Example.com")
	if err != nil {
		t.Fatalf("ByEmail: %v", err)
	}
	if byEmail.ID != u.ID {
		t.Errorf("ID mismatch")
	}
}

func TestSQLiteStore_NotFound(t *testing.T) {
	t.Parallel()
	db, dbcfg := openMemSQLite(t)
	store := user.NewStore(dbcfg, pdb.Pool{W: db, R: db})

	_, err := store.ByEmail(context.Background(), "missing@example.com")
	if err == nil {
		t.Fatal("expected NotFoundError")
	}
	if !user.IsNotFoundError(err) {
		t.Errorf("want IsNotFoundError, got %T: %v", err, err)
	}
}

func TestSQLiteStore_UpsertPreservesID(t *testing.T) {
	t.Parallel()
	db, dbcfg := openMemSQLite(t)
	store := user.NewStore(dbcfg, pdb.Pool{W: db, R: db})

	u, _ := user.New("a@b.co", "old", user.SourceGenesis)
	if err := store.Save(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	if err := u.Rename("new"); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(context.Background(), u); err != nil {
		t.Fatal(err)
	}

	got, err := store.ByID(context.Background(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "new" {
		t.Errorf("name not upserted: %q", got.Name)
	}
}

func TestSQLiteStore_UniqueEmailViolation(t *testing.T) {
	t.Parallel()
	db, dbcfg := openMemSQLite(t)
	store := user.NewStore(dbcfg, pdb.Pool{W: db, R: db})

	a, _ := user.New("dup@example.com", "A", user.SourceGenesis)
	b, _ := user.New("dup@example.com", "B", user.SourceOIDC)
	if err := store.Save(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	err := store.Save(context.Background(), b)
	if err == nil {
		t.Fatal("expected AlreadyExistsError on duplicate email")
	}
	if !user.IsAlreadyExistsError(err) {
		t.Errorf("want IsAlreadyExistsError, got %T: %v", err, err)
	}
}

func TestSQLiteStore_ByIDP(t *testing.T) {
	t.Parallel()
	db, dbcfg := openMemSQLite(t)
	store := user.NewStore(dbcfg, pdb.Pool{W: db, R: db})
	ctx := context.Background()

	u, err := user.New("oidc@example.com", "OIDC", user.SourceOIDC)
	if err != nil {
		t.Fatal(err)
	}
	u.IDPIssuer = "https://idp.example"
	u.IDPSubject = "sub-42"
	if err := store.Save(ctx, u); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := store.ByIDP(ctx, "https://idp.example", "sub-42")
	if err != nil {
		t.Fatalf("ByIDP: %v", err)
	}
	if got.ID != u.ID {
		t.Errorf("ID = %v, want %v", got.ID, u.ID)
	}
	if got.IDPIssuer != "https://idp.example" || got.IDPSubject != "sub-42" {
		t.Errorf("idp = %q/%q", got.IDPIssuer, got.IDPSubject)
	}

	if _, err := store.ByIDP(ctx, "https://idp.example", "nope"); !user.IsNotFoundError(err) {
		t.Errorf("ByIDP(missing) err = %v, want NotFoundError", err)
	}
}

func TestSQLiteStore_PasswordUsersWriteNullIDP(t *testing.T) {
	t.Parallel()
	db, dbcfg := openMemSQLite(t)
	store := user.NewStore(dbcfg, pdb.Pool{W: db, R: db})
	ctx := context.Background()

	for _, email := range []string{"local1@example.com", "local2@example.com"} {
		u, err := user.New(email, "Local", user.SourceLocal)
		if err != nil {
			t.Fatal(err)
		}
		u.PasswordHash = "$argon2id$stub"
		if err := store.Save(ctx, u); err != nil {
			t.Fatalf("Save(%s): %v", email, err)
		}
	}

	var nulls int
	countQ := "SELECT COUNT(*) FROM " + dbcfg.TablePrefix + "users WHERE idp_issuer IS NULL AND idp_subject IS NULL"
	if err := db.QueryRowContext(ctx, countQ).Scan(&nulls); err != nil {
		t.Fatalf("count: %v", err)
	}
	if nulls != 2 {
		t.Errorf("rows with NULL idp columns = %d, want 2", nulls)
	}
}

func TestSQLiteStore_DuplicateIDPReportsIDPField(t *testing.T) {
	t.Parallel()
	db, dbcfg := openMemSQLite(t)
	store := user.NewStore(dbcfg, pdb.Pool{W: db, R: db})
	ctx := context.Background()

	first, err := user.New("first@example.com", "First", user.SourceOIDC)
	if err != nil {
		t.Fatal(err)
	}
	first.IDPIssuer = "https://idp.example"
	first.IDPSubject = "sub-1"
	if err := store.Save(ctx, first); err != nil {
		t.Fatalf("Save first: %v", err)
	}

	second, err := user.New("second@example.com", "Second", user.SourceOIDC)
	if err != nil {
		t.Fatal(err)
	}
	second.IDPIssuer = "https://idp.example"
	second.IDPSubject = "sub-1"

	err = store.Save(ctx, second)
	var dup *user.AlreadyExistsError
	if !errors.As(err, &dup) {
		t.Fatalf("Save second err = %v, want *AlreadyExistsError", err)
	}
	if dup.Field != "idp_subject" {
		t.Errorf("Field = %q, want %q", dup.Field, "idp_subject")
	}
	if dup.Value != "sub-1" {
		t.Errorf("Value = %q, want %q", dup.Value, "sub-1")
	}
}

func TestSQLite_EnsureFromOIDC_EmailChangedAtIDP(t *testing.T) {
	t.Parallel()
	db, dbcfg := openMemSQLite(t)
	store := user.NewStore(dbcfg, pdb.Pool{W: db, R: db})
	svc := user.NewService(store, user.GenesisConfig{}, newTestLogger(), noopUnexpected())
	ctx := context.Background()

	first, err := svc.EnsureFromOIDC(ctx, user.Claims{Issuer: "https://idp", Subject: "sub-1", Email: "old@x.id", Name: "A"})
	if err != nil {
		t.Fatalf("first EnsureFromOIDC: %v", err)
	}

	second, err := svc.EnsureFromOIDC(ctx, user.Claims{Issuer: "https://idp", Subject: "sub-1", Email: "new@x.id", Name: "A"})
	if err != nil {
		t.Fatalf("second EnsureFromOIDC: %v", err)
	}
	if second.ID != first.ID {
		t.Errorf("ID = %v, want %v", second.ID, first.ID)
	}
	if second.Email != "new@x.id" {
		t.Errorf("Email = %q, want %q", second.Email, "new@x.id")
	}

	var rows int
	countQ := "SELECT COUNT(*) FROM " + dbcfg.TablePrefix + "users"
	if err := db.QueryRowContext(ctx, countQ).Scan(&rows); err != nil {
		t.Fatalf("count: %v", err)
	}
	if rows != 1 {
		t.Errorf("user rows = %d, want 1", rows)
	}

	byIDP, err := store.ByIDP(ctx, "https://idp", "sub-1")
	if err != nil {
		t.Fatalf("ByIDP: %v", err)
	}
	if byIDP.Email != "new@x.id" {
		t.Errorf("stored email = %q, want %q", byIDP.Email, "new@x.id")
	}
}
