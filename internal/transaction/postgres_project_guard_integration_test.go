//go:build integration

package transaction_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/transaction"
	"altalune.id/yasaku/money"
)

func TestPostgres_Transaction_WritesRejectSiblingProject(t *testing.T) {
	f := newPgFixture(t)
	victim := f.save(t, transaction.NewParams{
		WalletID:   f.walletA,
		Kind:       transaction.KindExpense,
		Amount:     money.New(25_000, money.IDR),
		Note:       "project A's transaction",
		OccurredAt: time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC),
	})

	siblingProj := uuid.New()
	_, err := f.db.ExecContext(t.Context(),
		"INSERT INTO "+f.prefix+"projects (id, org_id, slug, name, created_by, created_at, updated_at) VALUES ($1, $2, $3, 'Sibling', $4, $5, $5)",
		siblingProj, f.tc.OrgID, siblingProj.String()[:8], f.tc.UserID, time.Now().UTC())
	require.NoError(t, err)
	siblingCtx := tenant.Into(t.Context(), tenant.Context{OrgID: f.tc.OrgID, ProjectID: siblingProj, UserID: f.tc.UserID})

	attack := *victim
	attack.Note = "Hijacked"
	err = f.store.Save(siblingCtx, &attack)
	assert.True(t, transaction.IsNotFoundError(err), "sibling-project Save: got %T: %v", err, err)
	err = f.store.Delete(siblingCtx, victim.ID)
	assert.True(t, transaction.IsNotFoundError(err), "sibling-project Delete: got %T: %v", err, err)

	got, err := f.store.ByID(f.ctx(t), victim.ID)
	require.NoError(t, err)
	assert.Equal(t, "project A's transaction", got.Note)
}
