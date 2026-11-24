package apikey_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/apikey"
	"altalune.id/yasaku/internal/platform/authn"
	sqliteent "altalune.id/yasaku/internal/platform/db/entity/sqlite"
	"altalune.id/yasaku/internal/testutil/fakes"
)

// SECURITY: an org or project key acts for its creator, so deleting the creator revokes it; the key row survives, so rows it authored keep their reference.
func TestDeletingAUserRevokesTheirOrgAndProjectKeys(t *testing.T) {
	store, sqlDB, tc := newAPIKeyStoreForTest(t)
	creator := uuid.New()
	now := sqliteent.SQLiteTime(time.Now())
	_, err := sqlDB.Exec("INSERT INTO "+prefix+"users (id, email, name, avatar_url, is_admin, created_at, updated_at) VALUES (?, ?, '', '', 0, ?, ?)",
		creator.String(), creator.String()+"@x.com", now, now)
	require.NoError(t, err)
	ctx := fakes.Owner(t.Context(), tc)
	exp := time.Now().Add(time.Hour)

	projectKey, projectPlain, err := apikey.Scheme{}.Mint(tc.OrgID, tc.ProjectID, "ci", []string{authn.ScopeYasakuRead}, nil, &exp, time.Now().UTC())
	require.NoError(t, err)
	orgKey, _, err := apikey.Scheme{}.MintOrg(tc.OrgID, "ops", []string{authn.ScopeYasakuRead}, apikey.ProjectGrant{All: true}, &exp, time.Now().UTC())
	require.NoError(t, err)
	personal, _, err := apikey.Scheme{}.MintPersonal(tc.OrgID, creator, "laptop", []string{authn.ScopeYasakuRead}, apikey.ProjectGrant{All: true}, &exp, time.Now().UTC())
	require.NoError(t, err)
	for _, k := range []*apikey.APIKey{projectKey, orgKey, personal} {
		k.CreatedBy = creator
		require.NoError(t, store.Save(ctx, k))
	}

	_, err = sqlDB.Exec("DELETE FROM "+prefix+"users WHERE id = ?", creator.String())
	require.NoError(t, err, "deleting the creator succeeds")

	for name, k := range map[string]*apikey.APIKey{"project key": projectKey, "org key": orgKey} {
		got, err := store.ByID(ctx, k.ID)
		require.NoError(t, err, "%s row survives", name)
		assert.NotNil(t, got.RevokedAt, "%s is revoked", name)
	}
	got, err := store.ByID(ctx, personal.ID)
	require.NoError(t, err)
	assert.Nil(t, got.RevokedAt, "a personal key is left to its NULL owner, which refuses it already")

	members := fakes.NewMembers()
	_, err = apikey.NewAuthenticator(store, nil, apikey.Scheme{}, members).Authenticate(t.Context(), projectPlain)
	assert.True(t, authn.IsUnauthorizedError(err), "the revoked key is refused, got %v", err)
}
