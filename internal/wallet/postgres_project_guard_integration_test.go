//go:build integration

package wallet_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/wallet"
)

func (f pgFixture) siblingProject(t *testing.T) tenant.Context {
	t.Helper()
	projID := uuid.New()
	_, err := f.db.ExecContext(t.Context(),
		"INSERT INTO "+f.prefix+"projects (id, org_id, slug, name, created_by, created_at, updated_at) VALUES ($1, $2, $3, 'Sibling', $4, $5, $5)",
		projID, f.tc.OrgID, projID.String()[:8], f.tc.UserID, time.Now().UTC())
	require.NoError(t, err)
	return tenant.Context{OrgID: f.tc.OrgID, ProjectID: projID, UserID: f.tc.UserID}
}

func TestPostgres_Wallet_WritesRejectSiblingProject(t *testing.T) {
	f := newPgFixture(t)
	victim := newPgWallet(t, f, "project A's wallet")
	siblingCtx := tenant.Into(t.Context(), f.siblingProject(t))

	attack := *victim
	attack.Name = "Hijacked"
	err := f.store.Save(siblingCtx, &attack)
	assert.True(t, wallet.IsNotFoundError(err), "sibling-project Save: got %T: %v", err, err)
	err = f.store.Delete(siblingCtx, victim.ID)
	assert.True(t, wallet.IsNotFoundError(err), "sibling-project Delete: got %T: %v", err, err)

	got, err := f.store.ByID(tenant.Into(t.Context(), f.tc), victim.ID)
	require.NoError(t, err)
	assert.Equal(t, "project A's wallet", got.Name)
}
