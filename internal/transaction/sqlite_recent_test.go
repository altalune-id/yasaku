package transaction_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	sqliteent "altalune.id/yasaku/internal/platform/db/entity/sqlite"
	"altalune.id/yasaku/internal/platform/tenant"
)

func newSQLiteRecentScenario(t *testing.T) (sqliteFixture, recentScenario) {
	t.Helper()
	f := newSQLiteFixture(t)
	sibProj := uuid.New()
	now := sqliteent.SQLiteTime(time.Now())
	mustExec(t, f.db,
		"INSERT INTO "+f.prefix+"projects (id, org_id, slug, name, created_by, created_at, updated_at) VALUES (?, ?, ?, 'Sibling', ?, ?, ?)",
		sibProj.String(), f.tc.OrgID.String(), sibProj.String()[:8], f.tc.UserID.String(), now, now)
	sc := recentScenario{
		tc:        f.tc,
		sibling:   tenant.Context{OrgID: f.tc.OrgID, ProjectID: sibProj, UserID: f.tc.UserID},
		walletA:   f.walletA,
		walletB:   f.walletB,
		sibWallet: seedWallet(t, f.db, f.prefix, f.tc.OrgID, sibProj, "Sib", "IDR"),
		food:      f.category,
		coffee:    seedCategory(t, f.db, f.prefix, f.tc.OrgID, f.tc.ProjectID, "Coffee", "expense"),
		salary:    seedCategory(t, f.db, f.prefix, f.tc.OrgID, f.tc.ProjectID, "Salary", "income"),
		sibCat:    seedCategory(t, f.db, f.prefix, f.tc.OrgID, sibProj, "SibFood", "expense"),
		since:     time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
	}
	return f, sc
}

func TestSQLiteStore_TopCategories(t *testing.T) {
	f, sc := newSQLiteRecentScenario(t)
	sc.checkTopCategories(t, f.store)
}

func TestSQLiteStore_RecentByCategory(t *testing.T) {
	f, sc := newSQLiteRecentScenario(t)
	sc.checkRecentByCategory(t, f.store)
}
