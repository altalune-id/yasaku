//go:build integration

package category_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/category"
	"altalune.id/yasaku/internal/platform/tenant"
)

func TestPostgres_Category_WritesRejectSiblingProject(t *testing.T) {
	f := newPgFixture(t)
	ownerCtx := tenant.Into(t.Context(), f.tc)
	victim := pgNew(t, f.tc, "project A's category", category.KindExpense)
	require.NoError(t, f.store.Save(ownerCtx, victim))

	siblingProj := seedPgProject(t, f.sqlDB, f.prefix, f.tc.UserID, f.tc.OrgID, "sibling")
	siblingCtx := tenant.Into(t.Context(), tenant.Context{OrgID: f.tc.OrgID, ProjectID: siblingProj, UserID: f.tc.UserID})

	attack := *victim
	attack.Name = "Hijacked"
	err := f.store.Save(siblingCtx, &attack)
	assert.True(t, category.IsNotFoundError(err), "sibling-project Save: got %T: %v", err, err)
	err = f.store.Delete(siblingCtx, victim.ID)
	assert.True(t, category.IsNotFoundError(err), "sibling-project Delete: got %T: %v", err, err)

	got, err := f.store.ByID(ownerCtx, victim.ID)
	require.NoError(t, err)
	assert.Equal(t, "project A's category", got.Name)
}
