package report_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"altalune.id/yasaku/civil"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/db"
	sqliteent "altalune.id/yasaku/internal/platform/db/entity/sqlite"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/report"
	"altalune.id/yasaku/money"
	"altalune.id/yasaku/schema"
)

// seed is the fixture every reader test shares: two IDR wallets, two categories, two periods and
// nine transactions covering every kind, one carried to the next period and one left unassigned.
type seed struct {
	reader report.Reader
	tc     tenant.Context

	walletCash uuid.UUID
	walletBank uuid.UUID
	catFood    uuid.UUID
	catSalary  uuid.UUID
	periodAug  uuid.UUID
	periodSep  uuid.UUID

	// sibling is a second project inside the SAME org; foreign is a second org entirely.
	sibling neighbour
	foreign neighbour
}

// neighbour is a full set of rows the reader must never return for the fixture's own scope.
type neighbour struct {
	orgID     uuid.UUID
	projectID uuid.UUID
	walletID  uuid.UUID
	category  uuid.UUID
	periodID  uuid.UUID
}

func (s seed) ctx() context.Context { return tenant.Into(context.Background(), s.tc) }

func (s seed) augStartUTC(t *testing.T) time.Time {
	t.Helper()
	return civil.Date{Year: 2026, Month: time.August, Day: 1}.In(jakarta(t))
}

func (s seed) sepStartUTC(t *testing.T) time.Time {
	t.Helper()
	return civil.Date{Year: 2026, Month: time.September, Day: 1}.In(jakarta(t))
}

func jakarta(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Jakarta")
	require.NoError(t, err)
	return loc
}

func idr(minor int64) money.Amount { return money.New(minor, money.IDR) }

func newSQLiteSeed(t *testing.T) seed {
	t.Helper()
	sqlDB, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	_, err = sqlDB.Exec("PRAGMA foreign_keys = ON;")
	require.NoError(t, err)

	cfg := config.Defaults()
	require.NoError(t, schema.MigrateUp(context.Background(), sqlDB, cfg))
	prefix := cfg.DB.TablePrefix

	s := seedRows(t, sqlDB, prefix)
	s.reader = report.NewReader(
		db.DBConfig{Driver: db.DriverSQLite, TablePrefix: prefix},
		db.Pool{W: sqlDB, R: sqlDB},
		nil,
	)
	return s
}

func TestSQLiteReader_Period(t *testing.T) {
	s := newSQLiteSeed(t)
	ref, err := s.reader.Period(s.ctx(), s.tc.OrgID, s.tc.ProjectID, s.periodAug)
	require.NoError(t, err)

	assert.Equal(t, s.periodAug, ref.ID)
	assert.Equal(t, "Aug 2026", ref.Name)
	assert.Equal(t, civil.Date{Year: 2026, Month: time.August, Day: 1}, ref.Start)
	require.NotNil(t, ref.End)
	assert.Equal(t, civil.Date{Year: 2026, Month: time.August, Day: 31}, *ref.End)

	sep, err := s.reader.Period(s.ctx(), s.tc.OrgID, s.tc.ProjectID, s.periodSep)
	require.NoError(t, err)
	assert.Nil(t, sep.End, "the current period has no end date")
}

func TestSQLiteReader_Period_RefusesAnotherProject(t *testing.T) {
	s := newSQLiteSeed(t)
	_, err := s.reader.Period(s.ctx(), s.tc.OrgID, uuid.New(), s.periodAug)
	require.Error(t, err)
}

func TestSQLiteReader_Summary_FirstPeriod(t *testing.T) {
	s := newSQLiteSeed(t)
	got, err := s.reader.Summary(s.ctx(), s.tc.OrgID, s.tc.ProjectID, s.periodAug, money.IDR, s.augStartUTC(t))
	require.NoError(t, err)

	assert.Equal(t, money.IDR, got.Currency)
	assert.Equal(t, s.periodAug, got.Period.ID)
	assert.Equal(t, idr(500_000), got.Income)
	assert.Equal(t, idr(260_000), got.Expense, "expense + adjustment_out, transfers excluded")
	assert.Equal(t, idr(240_000), got.Net)
	assert.Equal(t, 3, got.TxCount, "income and expense rows only")

	require.Len(t, got.Wallets, 2, "only this project's IDR wallets")
	lines := byWallet(t, got.Wallets)
	cash := lines[s.walletCash]
	assert.Equal(t, idr(0), cash.Opening)
	assert.Equal(t, idr(1_500_000), cash.In, "opening + income")
	assert.Equal(t, idr(550_000), cash.Out, "two expenses plus the outgoing transfer")
	assert.Equal(t, idr(950_000), cash.Closing)
	assert.False(t, cash.ExcludeFromTotal)

	bank := lines[s.walletBank]
	assert.Equal(t, idr(0), bank.Opening)
	assert.Equal(t, idr(300_000), bank.In, "the incoming transfer")
	assert.Equal(t, idr(10_000), bank.Out, "the adjustment out")
	assert.Equal(t, idr(290_000), bank.Closing)
	assert.True(t, bank.ExcludeFromTotal)

	assert.Equal(t, idr(950_000), got.SpendableTotal, "the excluded wallet does not count")
	assert.Equal(t, idr(1_240_000), got.Total)
}

func TestSQLiteReader_Summary_SecondPeriodCarriesOpening(t *testing.T) {
	s := newSQLiteSeed(t)
	got, err := s.reader.Summary(s.ctx(), s.tc.OrgID, s.tc.ProjectID, s.periodSep, money.IDR, s.sepStartUTC(t))
	require.NoError(t, err)

	assert.Equal(t, idr(5_000), got.Income, "the adjustment in")
	assert.Equal(t, idr(25_000), got.Expense)
	assert.Equal(t, idr(-20_000), got.Net)
	assert.Equal(t, 1, got.TxCount)

	lines := byWallet(t, got.Wallets)
	cash := lines[s.walletCash]
	assert.Equal(t, idr(1_050_000), cash.Opening, "August closing plus the unassigned row before the September start")
	assert.Equal(t, idr(5_000), cash.In)
	assert.Equal(t, idr(0), cash.Out)
	assert.Equal(t, idr(1_055_000), cash.Closing)

	bank := lines[s.walletBank]
	assert.Equal(t, idr(290_000), bank.Opening)
	assert.Equal(t, idr(0), bank.In)
	assert.Equal(t, idr(25_000), bank.Out)
	assert.Equal(t, idr(265_000), bank.Closing)

	assert.Equal(t, idr(1_055_000), got.SpendableTotal)
	assert.Equal(t, idr(1_320_000), got.Total)
}

func TestSQLiteReader_Summary_OpeningOfTheNextPeriodEqualsTheClosingOfThePrevious(t *testing.T) {
	s := newSQLiteSeed(t)
	aug, err := s.reader.Summary(s.ctx(), s.tc.OrgID, s.tc.ProjectID, s.periodAug, money.IDR, s.augStartUTC(t))
	require.NoError(t, err)
	sep, err := s.reader.Summary(s.ctx(), s.tc.OrgID, s.tc.ProjectID, s.periodSep, money.IDR, s.sepStartUTC(t))
	require.NoError(t, err)

	augLines, sepLines := byWallet(t, aug.Wallets), byWallet(t, sep.Wallets)
	assert.Equal(t, augLines[s.walletBank].Closing, sepLines[s.walletBank].Opening)

	unassigned := idr(100_000)
	assert.Equal(t,
		augLines[s.walletCash].Closing.Add(unassigned),
		sepLines[s.walletCash].Opening,
		"a row with no period still lands in the opening of the period that contains its instant")
}

func TestSQLiteReader_Summary_OtherCurrencyIsEmpty(t *testing.T) {
	s := newSQLiteSeed(t)
	got, err := s.reader.Summary(s.ctx(), s.tc.OrgID, s.tc.ProjectID, s.periodAug, "USD", s.augStartUTC(t))
	require.NoError(t, err)

	assert.Equal(t, money.New(0, "USD"), got.Income)
	assert.Equal(t, money.New(0, "USD"), got.Expense)
	assert.Equal(t, 0, got.TxCount)
	assert.Empty(t, got.Wallets, "no USD wallet exists in the fixture")
}

func TestSQLiteReader_Summary_RefusesAnotherOrg(t *testing.T) {
	s := newSQLiteSeed(t)
	_, err := s.reader.Summary(s.ctx(), uuid.New(), s.tc.ProjectID, s.periodAug, money.IDR, s.augStartUTC(t))
	require.Error(t, err, "an org that disagrees with the request scope must match no period")
}

// SECURITY: SQLite has no RLS, so the explicit org and project predicates are the only protection.
// Every period-keyed read takes the period id from its caller, so a caller naming a neighbour's
// period must still come back empty.
func TestSQLiteReader_PeriodKeyedReads_RefuseANeighboursPeriod(t *testing.T) {
	s := newSQLiteSeed(t)
	for name, n := range map[string]neighbour{"sibling project": s.sibling, "foreign org": s.foreign} {
		t.Run(name, func(t *testing.T) {
			_, err := s.reader.Period(s.ctx(), s.tc.OrgID, s.tc.ProjectID, n.periodID)
			require.Error(t, err)

			_, err = s.reader.Summary(s.ctx(), s.tc.OrgID, s.tc.ProjectID, n.periodID, money.IDR, s.augStartUTC(t))
			require.Error(t, err)

			spend, err := s.reader.SpendByCategory(s.ctx(), s.tc.OrgID, s.tc.ProjectID, n.periodID, money.IDR)
			require.NoError(t, err)
			assert.Empty(t, spend)

			income, err := s.reader.IncomeByCategory(s.ctx(), s.tc.OrgID, s.tc.ProjectID, n.periodID, money.IDR)
			require.NoError(t, err)
			assert.Empty(t, income)

			flows, err := s.reader.Flows(s.ctx(), s.tc.OrgID, s.tc.ProjectID, n.periodID, money.IDR)
			require.NoError(t, err)
			assert.Empty(t, flows)
		})
	}
}

func TestSQLiteReader_Cashflow_DropsPeriodsOutsideTheProject(t *testing.T) {
	s := newSQLiteSeed(t)
	got, err := s.reader.Cashflow(s.ctx(), s.tc.OrgID, s.tc.ProjectID,
		[]uuid.UUID{s.periodAug, s.sibling.periodID, s.foreign.periodID}, money.IDR)
	require.NoError(t, err)
	require.Len(t, got, 1, "only the caller's own period resolves")
	assert.Equal(t, s.periodAug, got[0].Period.ID)
	assert.Equal(t, idr(500_000), got[0].Income)
	assert.Equal(t, idr(260_000), got[0].Expense)
}

func TestSQLiteReader_WalletBalances_ExcludeNeighbours(t *testing.T) {
	s := newSQLiteSeed(t)
	got, err := s.reader.WalletBalances(s.ctx(), s.tc.OrgID, s.tc.ProjectID)
	require.NoError(t, err)
	require.Len(t, got, 2, "only this project's two wallets")

	lines := byWallet(t, got)
	assert.NotContains(t, lines, s.sibling.walletID, "a sibling project's wallet must not appear")
	assert.NotContains(t, lines, s.foreign.walletID, "another org's wallet must not appear")
}

// SECURITY: the request's tenant scope is layered on top of the caller's arguments, so a caller
// asking for a scope it is not bound to matches nothing even when that scope really exists.
func TestSQLiteReader_RefusesAScopeOutsideTheRequestsTenant(t *testing.T) {
	s := newSQLiteSeed(t)
	n := s.foreign

	_, err := s.reader.Period(s.ctx(), n.orgID, n.projectID, n.periodID)
	require.Error(t, err)

	_, err = s.reader.Summary(s.ctx(), n.orgID, n.projectID, n.periodID, money.IDR, s.augStartUTC(t))
	require.Error(t, err)

	spend, err := s.reader.SpendByCategory(s.ctx(), n.orgID, n.projectID, n.periodID, money.IDR)
	require.NoError(t, err)
	assert.Empty(t, spend)

	flows, err := s.reader.Flows(s.ctx(), n.orgID, n.projectID, n.periodID, money.IDR)
	require.NoError(t, err)
	assert.Empty(t, flows)

	points, err := s.reader.Cashflow(s.ctx(), n.orgID, n.projectID, []uuid.UUID{n.periodID}, money.IDR)
	require.NoError(t, err)
	assert.Empty(t, points)

	balances, err := s.reader.WalletBalances(s.ctx(), n.orgID, n.projectID)
	require.NoError(t, err)
	assert.Empty(t, balances)
}

func TestSQLiteReader_SpendByCategory(t *testing.T) {
	s := newSQLiteSeed(t)
	got, err := s.reader.SpendByCategory(s.ctx(), s.tc.OrgID, s.tc.ProjectID, s.periodAug, money.IDR)
	require.NoError(t, err)
	require.Len(t, got, 2, "transfers and adjustments are not spend")

	require.NotNil(t, got[0].CategoryID)
	assert.Equal(t, s.catFood, *got[0].CategoryID)
	assert.Equal(t, "Food", got[0].Name)
	assert.Equal(t, "utensils", got[0].Icon)
	assert.Equal(t, "chart-1", got[0].Color)
	assert.Equal(t, idr(200_000), got[0].Amount)
	assert.Equal(t, 1, got[0].Count)
	assert.InDelta(t, 0.8, got[0].Share, 1e-9)

	assert.Nil(t, got[1].CategoryID, "the uncategorized slice")
	assert.Empty(t, got[1].Name, "the web layer labels the uncategorized slice")
	assert.Equal(t, idr(50_000), got[1].Amount)
	assert.InDelta(t, 0.2, got[1].Share, 1e-9)
}

func TestSQLiteReader_IncomeByCategory(t *testing.T) {
	s := newSQLiteSeed(t)
	got, err := s.reader.IncomeByCategory(s.ctx(), s.tc.OrgID, s.tc.ProjectID, s.periodAug, money.IDR)
	require.NoError(t, err)
	require.Len(t, got, 1, "the adjustment in is not income here")

	require.NotNil(t, got[0].CategoryID)
	assert.Equal(t, s.catSalary, *got[0].CategoryID)
	assert.Equal(t, "Salary", got[0].Name)
	assert.Equal(t, idr(500_000), got[0].Amount)
	assert.InDelta(t, 1.0, got[0].Share, 1e-9)
}

func TestSQLiteReader_Flows(t *testing.T) {
	s := newSQLiteSeed(t)
	got, err := s.reader.Flows(s.ctx(), s.tc.OrgID, s.tc.ProjectID, s.periodAug, money.IDR)
	require.NoError(t, err)
	require.Len(t, got, 2)

	assert.Equal(t, s.walletCash, got[0].WalletID)
	assert.Equal(t, "Cash", got[0].WalletName)
	require.NotNil(t, got[0].CategoryID)
	assert.Equal(t, s.catFood, *got[0].CategoryID)
	assert.Equal(t, "Food", got[0].CategoryName)
	assert.Equal(t, idr(200_000), got[0].Amount)

	assert.Nil(t, got[1].CategoryID)
	assert.Equal(t, idr(50_000), got[1].Amount)
}

func TestSQLiteReader_Cashflow_KeepsTheRequestedOrder(t *testing.T) {
	s := newSQLiteSeed(t)
	got, err := s.reader.Cashflow(s.ctx(), s.tc.OrgID, s.tc.ProjectID,
		[]uuid.UUID{s.periodSep, s.periodAug}, money.IDR)
	require.NoError(t, err)
	require.Len(t, got, 2)

	assert.Equal(t, s.periodSep, got[0].Period.ID)
	assert.Equal(t, "Sep 2026", got[0].Period.Name)
	assert.Equal(t, idr(5_000), got[0].Income)
	assert.Equal(t, idr(25_000), got[0].Expense)
	assert.Equal(t, idr(-20_000), got[0].Net)

	assert.Equal(t, s.periodAug, got[1].Period.ID)
	assert.Equal(t, idr(500_000), got[1].Income)
	assert.Equal(t, idr(260_000), got[1].Expense)
	assert.Equal(t, idr(240_000), got[1].Net)
}

func TestSQLiteReader_Cashflow_EmptyRequest(t *testing.T) {
	s := newSQLiteSeed(t)
	got, err := s.reader.Cashflow(s.ctx(), s.tc.OrgID, s.tc.ProjectID, nil, money.IDR)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestSQLiteReader_WalletBalances(t *testing.T) {
	s := newSQLiteSeed(t)
	got, err := s.reader.WalletBalances(s.ctx(), s.tc.OrgID, s.tc.ProjectID)
	require.NoError(t, err)
	require.Len(t, got, 2)

	lines := byWallet(t, got)
	cash := lines[s.walletCash]
	assert.Equal(t, idr(0), cash.Opening, "live balances have no opening")
	assert.Equal(t, idr(1_605_000), cash.In)
	assert.Equal(t, idr(550_000), cash.Out)
	assert.Equal(t, idr(1_055_000), cash.Closing)
	assert.Equal(t, "cash", cash.Kind)

	bank := lines[s.walletBank]
	assert.Equal(t, idr(300_000), bank.In)
	assert.Equal(t, idr(35_000), bank.Out)
	assert.Equal(t, idr(265_000), bank.Closing)
	assert.Equal(t, "bank", bank.Kind)
}

func byWallet(t *testing.T, lines []report.WalletLine) map[uuid.UUID]report.WalletLine {
	t.Helper()
	out := make(map[uuid.UUID]report.WalletLine, len(lines))
	for _, l := range lines {
		out[l.WalletID] = l
	}
	return out
}

func seedRows(t *testing.T, sqlDB *sql.DB, prefix string) seed {
	t.Helper()
	userID, orgID, projID := uuid.New(), uuid.New(), uuid.New()
	now := sqliteent.SQLiteTime(time.Now())
	mustExec(t, sqlDB,
		"INSERT INTO "+prefix+"users (id, email, name, avatar_url, is_admin, created_at, updated_at) VALUES (?, ?, '', '', 0, ?, ?)",
		userID.String(), userID.String()+"@x.com", now, now)
	mustExec(t, sqlDB,
		"INSERT INTO "+prefix+"orgs (id, slug, name, created_by, created_at, updated_at) VALUES (?, ?, 'Org', ?, ?, ?)",
		orgID.String(), orgID.String()[:8], userID.String(), now, now)
	mustExec(t, sqlDB,
		"INSERT INTO "+prefix+"projects (id, org_id, slug, name, created_by, created_at, updated_at) VALUES (?, ?, ?, 'Cash', ?, ?, ?)",
		projID.String(), orgID.String(), projID.String()[:8], userID.String(), now, now)

	s := seed{tc: tenant.Context{OrgID: orgID, ProjectID: projID, UserID: userID}}
	s.walletCash = seedWallet(t, sqlDB, prefix, orgID, projID, "Cash", "cash", false)
	s.walletBank = seedWallet(t, sqlDB, prefix, orgID, projID, "Bank", "bank", true)
	s.catFood = seedCategory(t, sqlDB, prefix, orgID, projID, "Food", "expense", "utensils", "chart-1")
	s.catSalary = seedCategory(t, sqlDB, prefix, orgID, projID, "Salary", "income", "banknote", "chart-4")
	s.periodAug = seedPeriod(t, sqlDB, prefix, orgID, projID, "Aug 2026", "2026-08-01", "2026-08-31")
	s.periodSep = seedPeriod(t, sqlDB, prefix, orgID, projID, "Sep 2026", "2026-09-01", "")

	aug, sep := &s.periodAug, &s.periodSep
	seedTxn(t, sqlDB, prefix, orgID, projID, userID, txnRow{
		wallet: s.walletCash, kind: "opening", minor: 1_000_000, period: aug, at: "2026-08-01T01:00:00Z"})
	seedTxn(t, sqlDB, prefix, orgID, projID, userID, txnRow{
		wallet: s.walletCash, kind: "income", minor: 500_000, category: &s.catSalary, period: aug, at: "2026-08-05T03:00:00Z"})
	seedTxn(t, sqlDB, prefix, orgID, projID, userID, txnRow{
		wallet: s.walletCash, kind: "expense", minor: 200_000, category: &s.catFood, period: aug, at: "2026-08-06T03:00:00Z"})
	seedTxn(t, sqlDB, prefix, orgID, projID, userID, txnRow{
		wallet: s.walletCash, kind: "expense", minor: 50_000, period: aug, at: "2026-08-07T03:00:00Z"})
	seedTxn(t, sqlDB, prefix, orgID, projID, userID, txnRow{
		wallet: s.walletCash, toWallet: &s.walletBank, kind: "transfer", minor: 300_000, period: aug, at: "2026-08-10T03:00:00Z"})
	seedTxn(t, sqlDB, prefix, orgID, projID, userID, txnRow{
		wallet: s.walletBank, kind: "adjustment_out", minor: 10_000, period: aug, at: "2026-08-20T03:00:00Z"})
	seedTxn(t, sqlDB, prefix, orgID, projID, userID, txnRow{
		wallet: s.walletCash, kind: "income", minor: 100_000, category: &s.catSalary, at: "2026-08-15T03:00:00Z"})
	seedTxn(t, sqlDB, prefix, orgID, projID, userID, txnRow{
		wallet: s.walletBank, kind: "expense", minor: 25_000, category: &s.catFood, period: sep, at: "2026-09-03T03:00:00Z"})
	seedTxn(t, sqlDB, prefix, orgID, projID, userID, txnRow{
		wallet: s.walletCash, kind: "adjustment_in", minor: 5_000, period: sep, at: "2026-09-05T03:00:00Z"})

	s.sibling = seedNeighbour(t, sqlDB, prefix, orgID, userID, 777_000)
	s.foreign = seedNeighbour(t, sqlDB, prefix, uuid.Nil, userID, 888_000)
	return s
}

// seedNeighbour builds a whole project the fixture's scope must never see. A zero orgID means a new org;
// otherwise the project is a sibling inside the given org, so only the project predicate can exclude it.
func seedNeighbour(t *testing.T, sqlDB *sql.DB, prefix string, orgID, userID uuid.UUID, minor int64) neighbour {
	t.Helper()
	now := sqliteent.SQLiteTime(time.Now())
	if orgID == uuid.Nil {
		orgID = uuid.New()
		mustExec(t, sqlDB,
			"INSERT INTO "+prefix+"orgs (id, slug, name, created_by, created_at, updated_at) VALUES (?, ?, 'Other', ?, ?, ?)",
			orgID.String(), orgID.String()[:8], userID.String(), now, now)
	}
	projID := uuid.New()
	mustExec(t, sqlDB,
		"INSERT INTO "+prefix+"projects (id, org_id, slug, name, created_by, created_at, updated_at) VALUES (?, ?, ?, 'Other', ?, ?, ?)",
		projID.String(), orgID.String(), projID.String()[:8], userID.String(), now, now)

	n := neighbour{orgID: orgID, projectID: projID}
	// NOTE: the same wallet and category names as the fixture, so no test can pass on a name difference.
	n.walletID = seedWallet(t, sqlDB, prefix, orgID, projID, "Cash", "cash", false)
	n.category = seedCategory(t, sqlDB, prefix, orgID, projID, "Food", "expense", "utensils", "chart-1")
	n.periodID = seedPeriod(t, sqlDB, prefix, orgID, projID, "Aug 2026", "2026-08-01", "2026-08-31")

	period := &n.periodID
	seedTxn(t, sqlDB, prefix, orgID, projID, userID, txnRow{
		wallet: n.walletID, kind: "opening", minor: minor, period: period, at: "2026-08-01T01:00:00Z"})
	seedTxn(t, sqlDB, prefix, orgID, projID, userID, txnRow{
		wallet: n.walletID, kind: "expense", minor: minor, category: &n.category, period: period, at: "2026-08-06T03:00:00Z"})
	seedTxn(t, sqlDB, prefix, orgID, projID, userID, txnRow{
		wallet: n.walletID, kind: "income", minor: minor, period: period, at: "2026-08-07T03:00:00Z"})
	return n
}

type txnRow struct {
	wallet   uuid.UUID
	toWallet *uuid.UUID
	kind     string
	minor    int64
	category *uuid.UUID
	period   *uuid.UUID
	at       string
}

func seedTxn(t *testing.T, sqlDB *sql.DB, prefix string, orgID, projID, userID uuid.UUID, r txnRow) {
	t.Helper()
	at, err := time.Parse(time.RFC3339, r.at)
	require.NoError(t, err)
	stamp := sqliteent.SQLiteTime(at)
	mustExec(t, sqlDB,
		"INSERT INTO "+prefix+"transactions (id, org_id, project_id, wallet_id, to_wallet_id, kind, amount_minor, currency, "+
			"category_id, period_id, note, note_norm, occurred_at, created_by, created_at, updated_at) "+
			"VALUES (?, ?, ?, ?, ?, ?, ?, 'IDR', ?, ?, '', '', ?, ?, ?, ?)",
		uuid.New().String(), orgID.String(), projID.String(), r.wallet.String(), nullUUID(r.toWallet),
		r.kind, r.minor, nullUUID(r.category), nullUUID(r.period), stamp, userID.String(), stamp, stamp)
}

func nullUUID(id *uuid.UUID) any {
	if id == nil {
		return nil
	}
	return id.String()
}

func seedWallet(t *testing.T, sqlDB *sql.DB, prefix string, orgID, projID uuid.UUID, name, kind string, exclude bool) uuid.UUID {
	t.Helper()
	id := uuid.New()
	now := sqliteent.SQLiteTime(time.Now())
	flag := 0
	if exclude {
		flag = 1
	}
	mustExec(t, sqlDB,
		"INSERT INTO "+prefix+"wallets (id, org_id, project_id, name, kind, provider, currency, exclude_from_total, archived_at, created_at, updated_at) "+
			"VALUES (?, ?, ?, ?, ?, '', 'IDR', ?, NULL, ?, ?)",
		id.String(), orgID.String(), projID.String(), name, kind, flag, now, now)
	return id
}

func seedCategory(t *testing.T, sqlDB *sql.DB, prefix string, orgID, projID uuid.UUID, name, kind, icon, color string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	now := sqliteent.SQLiteTime(time.Now())
	mustExec(t, sqlDB,
		"INSERT INTO "+prefix+"categories (id, org_id, project_id, name, kind, icon, color, sort_order, archived_at, created_at, updated_at) "+
			"VALUES (?, ?, ?, ?, ?, ?, ?, 0, NULL, ?, ?)",
		id.String(), orgID.String(), projID.String(), name, kind, icon, color, now, now)
	return id
}

func seedPeriod(t *testing.T, sqlDB *sql.DB, prefix string, orgID, projID uuid.UUID, name, start, end string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	now := sqliteent.SQLiteTime(time.Now())
	var endArg any
	status := "open"
	if end != "" {
		endArg = end
		status = "closed"
	}
	mustExec(t, sqlDB,
		"INSERT INTO "+prefix+"periods (id, org_id, project_id, name, start_date, end_date, status, closed_at, snapshot, created_at, updated_at) "+
			"VALUES (?, ?, ?, ?, ?, ?, ?, NULL, NULL, ?, ?)",
		id.String(), orgID.String(), projID.String(), name, start, endArg, status, now, now)
	return id
}

func mustExec(t *testing.T, sqlDB *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := sqlDB.Exec(query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}
