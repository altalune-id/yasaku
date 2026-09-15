//go:build integration

package user_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/testutil/pgtest"
	"altalune.id/yasaku/internal/user"
	"altalune.id/yasaku/schema"
)

func newPostgresStoreForTest(t *testing.T) user.Store {
	t.Helper()
	h := pgtest.New(t)
	sqlDB := h.OpenDB(t)

	cfg := config.Defaults()
	cfg.DB.Driver = "postgres"
	cfg.DB.DSN = h.DSN
	cfg.DB.Schema = h.Schema
	cfg.DB.AllowBypassRLS = true

	require.NoError(t, schema.MigrateUp(t.Context(), sqlDB, cfg))

	return user.NewStore(db.DBConfig{Driver: db.DriverPostgres, Schema: h.Schema, TablePrefix: cfg.DB.TablePrefix}, db.Pool{W: sqlDB, R: sqlDB})
}

func TestPostgres_User_SaveAndLookup(t *testing.T) {
	store := newPostgresStoreForTest(t)
	ctx := t.Context()

	u, err := user.New("alice@example.com", "Alice", user.SourceLocal)
	require.NoError(t, err)
	u.PasswordHash = "$argon2id$stub"
	require.NoError(t, store.Save(ctx, u))

	byID, err := store.ByID(ctx, u.ID)
	require.NoError(t, err)
	assert.Equal(t, "alice@example.com", byID.Email)
	assert.Equal(t, "Alice", byID.Name)

	byEmail, err := store.ByEmail(ctx, "alice@example.com")
	require.NoError(t, err)
	assert.Equal(t, u.ID, byEmail.ID)
}

func TestPostgres_User_NotFound(t *testing.T) {
	store := newPostgresStoreForTest(t)
	ctx := t.Context()

	u, err := user.New("solo@example.com", "Solo", user.SourceLocal)
	require.NoError(t, err)

	_, err = store.ByID(ctx, u.ID)
	assert.True(t, user.IsNotFoundError(err), "want NotFoundError, got %T: %v", err, err)

	_, err = store.ByEmail(ctx, "missing@example.com")
	assert.True(t, user.IsNotFoundError(err), "want NotFoundError, got %T: %v", err, err)
}

func TestPostgres_User_HasLocalUsers(t *testing.T) {
	store := newPostgresStoreForTest(t)
	ctx := t.Context()

	ok, err := store.HasLocalUsers(ctx)
	require.NoError(t, err)
	assert.False(t, ok)

	u, err := user.New("local@example.com", "Local", user.SourceLocal)
	require.NoError(t, err)
	u.PasswordHash = "hash"
	require.NoError(t, store.Save(ctx, u))

	ok, err = store.HasLocalUsers(ctx)
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestPostgres_User_UpdateLocale(t *testing.T) {
	store := newPostgresStoreForTest(t)
	ctx := t.Context()

	u, err := user.New("loc@example.com", "Loc", user.SourceLocal)
	require.NoError(t, err)
	require.NoError(t, store.Save(ctx, u))

	require.NoError(t, store.UpdateLocale(ctx, u.ID, "id-ID"))

	got, err := store.ByID(ctx, u.ID)
	require.NoError(t, err)
	assert.Equal(t, "id-ID", got.Locale)
}

func TestPostgres_User_SaveIsUpsert(t *testing.T) {
	store := newPostgresStoreForTest(t)
	ctx := t.Context()

	u, err := user.New("up@example.com", "First", user.SourceLocal)
	require.NoError(t, err)
	require.NoError(t, store.Save(ctx, u))

	u.Name = "Renamed"
	u.TermsAcceptedAt = ptrTime(time.Now().UTC())
	require.NoError(t, store.Save(ctx, u))

	got, err := store.ByID(ctx, u.ID)
	require.NoError(t, err)
	assert.Equal(t, "Renamed", got.Name)
	require.NotNil(t, got.TermsAcceptedAt)
}

func ptrTime(t time.Time) *time.Time { return &t }

func TestPostgres_User_ByIDP(t *testing.T) {
	store := newPostgresStoreForTest(t)
	ctx := t.Context()

	u, err := user.New("oidc@example.com", "OIDC", user.SourceOIDC)
	require.NoError(t, err)
	u.IDPIssuer = "https://idp.example"
	u.IDPSubject = "sub-42"
	require.NoError(t, store.Save(ctx, u))

	got, err := store.ByIDP(ctx, "https://idp.example", "sub-42")
	require.NoError(t, err)
	assert.Equal(t, u.ID, got.ID)
	assert.Equal(t, "https://idp.example", got.IDPIssuer)
	assert.Equal(t, "sub-42", got.IDPSubject)
	assert.Equal(t, user.SourceOIDC, got.Source)

	_, err = store.ByIDP(ctx, "https://idp.example", "nope")
	assert.True(t, user.IsNotFoundError(err), "want NotFoundError, got %v", err)
}

func TestPostgres_User_PasswordUsersWriteNullIDP(t *testing.T) {
	store := newPostgresStoreForTest(t)
	ctx := t.Context()

	for _, email := range []string{"local1@example.com", "local2@example.com"} {
		u, err := user.New(email, "Local", user.SourceLocal)
		require.NoError(t, err)
		u.PasswordHash = "$argon2id$stub"
		require.NoError(t, store.Save(ctx, u), "second password user must not collide on users_idp_idx")

		got, err := store.ByID(ctx, u.ID)
		require.NoError(t, err)
		assert.Empty(t, got.IDPIssuer)
		assert.Empty(t, got.IDPSubject)
	}
}

func TestPostgres_User_DuplicateIDPReportsIDPField(t *testing.T) {
	store := newPostgresStoreForTest(t)
	ctx := t.Context()

	first, err := user.New("first@example.com", "First", user.SourceOIDC)
	require.NoError(t, err)
	first.IDPIssuer = "https://idp.example"
	first.IDPSubject = "sub-1"
	require.NoError(t, store.Save(ctx, first))

	second, err := user.New("second@example.com", "Second", user.SourceOIDC)
	require.NoError(t, err)
	second.IDPIssuer = "https://idp.example"
	second.IDPSubject = "sub-1"

	var dup *user.AlreadyExistsError
	require.ErrorAs(t, store.Save(ctx, second), &dup)
	assert.Equal(t, "idp_subject", dup.Field)
	assert.Equal(t, "sub-1", dup.Value)
}

func TestPostgres_EnsureFromOIDC_EmailChangedAtIDP(t *testing.T) {
	store := newPostgresStoreForTest(t)
	svc := user.NewService(store, user.GenesisConfig{}, newTestLogger(), noopUnexpected())
	ctx := t.Context()

	first, err := svc.EnsureFromOIDC(ctx, user.Claims{Issuer: "https://idp", Subject: "sub-1", Email: "old@x.id", Name: "A"})
	require.NoError(t, err)

	second, err := svc.EnsureFromOIDC(ctx, user.Claims{Issuer: "https://idp", Subject: "sub-1", Email: "new@x.id", Name: "A"})
	require.NoError(t, err, "a changed IdP email must resolve via users_idp_idx, not collide on it")
	assert.Equal(t, first.ID, second.ID)
	assert.Equal(t, "new@x.id", second.Email)

	byIDP, err := store.ByIDP(ctx, "https://idp", "sub-1")
	require.NoError(t, err)
	assert.Equal(t, first.ID, byIDP.ID)
	assert.Equal(t, "new@x.id", byIDP.Email)

	_, err = store.ByEmail(ctx, "old@x.id")
	assert.True(t, user.IsNotFoundError(err), "the old address must no longer resolve, got %v", err)
}
