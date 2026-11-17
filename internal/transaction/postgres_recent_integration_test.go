//go:build integration

package transaction_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/platform/tenant"
)

func newPgRecentScenario(t *testing.T) (pgFixture, recentScenario) {
	t.Helper()
	f := newPgFixture(t)
	sibProj := uuid.New()
	_, err := f.db.ExecContext(t.Context(),
		"INSERT INTO "+f.prefix+"projects (id, org_id, slug, name, created_by, created_at, updated_at) VALUES ($1, $2, $3, 'Sibling', $4, $5, $5)",
		sibProj, f.tc.OrgID, sibProj.String()[:8], f.tc.UserID, time.Now().UTC())
	require.NoError(t, err)
	sc := recentScenario{
		tc:        f.tc,
		sibling:   tenant.Context{OrgID: f.tc.OrgID, ProjectID: sibProj, UserID: f.tc.UserID},
		walletA:   f.walletA,
		walletB:   f.walletB,
		sibWallet: seedPgWallet(t, f.db, f.prefix, f.tc.OrgID, sibProj, "Sib", "IDR"),
		food:      f.category,
		coffee:    seedPgCategory(t, f.db, f.prefix, f.tc.OrgID, f.tc.ProjectID, "Coffee", "expense"),
		salary:    seedPgCategory(t, f.db, f.prefix, f.tc.OrgID, f.tc.ProjectID, "Salary", "income"),
		sibCat:    seedPgCategory(t, f.db, f.prefix, f.tc.OrgID, sibProj, "SibFood", "expense"),
		since:     time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
	}
	return f, sc
}

func TestPostgres_Transaction_TopCategories(t *testing.T) {
	f, sc := newPgRecentScenario(t)
	sc.checkTopCategories(t, f.store)
}

func TestPostgres_Transaction_RecentByCategory(t *testing.T) {
	f, sc := newPgRecentScenario(t)
	sc.checkRecentByCategory(t, f.store)
}
