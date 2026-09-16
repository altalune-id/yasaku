//go:build integration

package report_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/civil"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/report"
	"altalune.id/yasaku/internal/testutil/pgtest"
	"altalune.id/yasaku/money"
	"altalune.id/yasaku/schema"
)

func newPgSeed(t *testing.T) seed {
	t.Helper()
	h := pgtest.New(t)
	sqlDB := h.OpenDB(t)

	cfg := config.Defaults()
	cfg.DB.Driver = "postgres"
	cfg.DB.DSN = h.DSN
	cfg.DB.Schema = h.Schema
	cfg.DB.AllowBypassRLS = true
	require.NoError(t, schema.MigrateUp(t.Context(), sqlDB, cfg))

	prefix := cfg.DB.TablePrefix
	s := seedPgRows(t, sqlDB, prefix)
	s.reader = report.NewReader(
		db.DBConfig{Driver: db.DriverPostgres, Schema: h.Schema, TablePrefix: prefix},
		db.Pool{W: sqlDB, R: sqlDB},
		tenant.NewPgConn(sqlDB),
	)
	return s
}

func TestPgReader_Period(t *testing.T) {
	s := newPgSeed(t)
	ref, err := s.reader.Period(s.ctx(), s.tc.OrgID, s.tc.ProjectID, s.periodAug)
	require.NoError(t, err)
	assert.Equal(t, "Aug 2026", ref.Name)
	assert.Equal(t, civil.Date{Year: 2026, Month: time.August, Day: 1}, ref.Start)
	require.NotNil(t, ref.End)
	assert.Equal(t, civil.Date{Year: 2026, Month: time.August, Day: 31}, *ref.End)
}

func TestPgReader_Summary_FirstPeriod(t *testing.T) {
	s := newPgSeed(t)
	got, err := s.reader.Summary(s.ctx(), s.tc.OrgID, s.tc.ProjectID, s.periodAug, money.IDR, s.augStartUTC(t))
	require.NoError(t, err)

	assert.Equal(t, idr(500_000), got.Income)
	assert.Equal(t, idr(260_000), got.Expense)
	assert.Equal(t, idr(240_000), got.Net)
	assert.Equal(t, 3, got.TxCount)

	lines := byWallet(t, got.Wallets)
	assert.Equal(t, idr(0), lines[s.walletCash].Opening)
	assert.Equal(t, idr(1_500_000), lines[s.walletCash].In)
	assert.Equal(t, idr(550_000), lines[s.walletCash].Out)
	assert.Equal(t, idr(950_000), lines[s.walletCash].Closing)
	assert.Equal(t, idr(300_000), lines[s.walletBank].In)
	assert.Equal(t, idr(10_000), lines[s.walletBank].Out)
	assert.Equal(t, idr(290_000), lines[s.walletBank].Closing)

	assert.Equal(t, idr(950_000), got.SpendableTotal)
	assert.Equal(t, idr(1_240_000), got.Total)
}

func TestPgReader_Summary_SecondPeriodCarriesOpening(t *testing.T) {
	s := newPgSeed(t)
	got, err := s.reader.Summary(s.ctx(), s.tc.OrgID, s.tc.ProjectID, s.periodSep, money.IDR, s.sepStartUTC(t))
	require.NoError(t, err)

	assert.Equal(t, idr(5_000), got.Income)
	assert.Equal(t, idr(25_000), got.Expense)
	assert.Equal(t, 1, got.TxCount)

	lines := byWallet(t, got.Wallets)
	assert.Equal(t, idr(1_050_000), lines[s.walletCash].Opening)
	assert.Equal(t, idr(1_055_000), lines[s.walletCash].Closing)
	assert.Equal(t, idr(290_000), lines[s.walletBank].Opening)
	assert.Equal(t, idr(265_000), lines[s.walletBank].Closing)
	assert.Equal(t, idr(1_320_000), got.Total)
}

func TestPgReader_SpendByCategory(t *testing.T) {
	s := newPgSeed(t)
	got, err := s.reader.SpendByCategory(s.ctx(), s.tc.OrgID, s.tc.ProjectID, s.periodAug, money.IDR)
	require.NoError(t, err)
	require.Len(t, got, 2)

	require.NotNil(t, got[0].CategoryID)
	assert.Equal(t, s.catFood, *got[0].CategoryID)
	assert.Equal(t, idr(200_000), got[0].Amount)
	assert.InDelta(t, 0.8, got[0].Share, 1e-9)
	assert.Nil(t, got[1].CategoryID)
	assert.Empty(t, got[1].Name)
	assert.Equal(t, idr(50_000), got[1].Amount)
}

func TestPgReader_IncomeByCategory(t *testing.T) {
	s := newPgSeed(t)
	got, err := s.reader.IncomeByCategory(s.ctx(), s.tc.OrgID, s.tc.ProjectID, s.periodAug, money.IDR)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, idr(500_000), got[0].Amount)
	assert.Equal(t, "Salary", got[0].Name)
}

func TestPgReader_Flows(t *testing.T) {
	s := newPgSeed(t)
	got, err := s.reader.Flows(s.ctx(), s.tc.OrgID, s.tc.ProjectID, s.periodAug, money.IDR)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, s.walletCash, got[0].WalletID)
	assert.Equal(t, "Food", got[0].CategoryName)
	assert.Equal(t, idr(200_000), got[0].Amount)
	assert.Nil(t, got[1].CategoryID)
}

func TestPgReader_Cashflow_KeepsTheRequestedOrder(t *testing.T) {
	s := newPgSeed(t)
	got, err := s.reader.Cashflow(s.ctx(), s.tc.OrgID, s.tc.ProjectID,
		[]uuid.UUID{s.periodSep, s.periodAug}, money.IDR)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, s.periodSep, got[0].Period.ID)
	assert.Equal(t, idr(-20_000), got[0].Net)
	assert.Equal(t, s.periodAug, got[1].Period.ID)
	assert.Equal(t, idr(240_000), got[1].Net)
}

func TestPgReader_WalletBalances(t *testing.T) {
	s := newPgSeed(t)
	got, err := s.reader.WalletBalances(s.ctx(), s.tc.OrgID, s.tc.ProjectID)
	require.NoError(t, err)
	require.Len(t, got, 2)

	lines := byWallet(t, got)
	assert.Equal(t, idr(0), lines[s.walletCash].Opening)
	assert.Equal(t, idr(1_605_000), lines[s.walletCash].In)
	assert.Equal(t, idr(550_000), lines[s.walletCash].Out)
	assert.Equal(t, idr(1_055_000), lines[s.walletCash].Closing)
	assert.Equal(t, idr(265_000), lines[s.walletBank].Closing)
}

func seedPgRows(t *testing.T, sqlDB *sql.DB, prefix string) seed {
	t.Helper()
	userID, orgID, projID := uuid.New(), uuid.New(), uuid.New()
	now := time.Now().UTC()
	mustExecPg(t, sqlDB,
		"INSERT INTO "+prefix+"users (id, email, name, avatar_url, is_admin, created_at, updated_at) VALUES ($1, $2, '', '', false, $3, $3)",
		userID, userID.String()+"@x.co", now)
	mustExecPg(t, sqlDB,
		"INSERT INTO "+prefix+"orgs (id, slug, name, created_by, created_at, updated_at) VALUES ($1, $2, 'Acme', $3, $4, $4)",
		orgID, orgID.String()[:8], userID, now)
	mustExecPg(t, sqlDB,
		"INSERT INTO "+prefix+"projects (id, org_id, slug, name, created_by, created_at, updated_at) VALUES ($1, $2, $3, 'Cash', $4, $5, $5)",
		projID, orgID, projID.String()[:8], userID, now)

	s := seed{tc: tenant.Context{OrgID: orgID, ProjectID: projID, UserID: userID}}
	s.walletCash = seedPgWalletRow(t, sqlDB, prefix, orgID, projID, "Cash", "cash", false)
	s.walletBank = seedPgWalletRow(t, sqlDB, prefix, orgID, projID, "Bank", "bank", true)
	s.catFood = seedPgCategoryRow(t, sqlDB, prefix, orgID, projID, "Food", "expense", "utensils", "chart-1")
	s.catSalary = seedPgCategoryRow(t, sqlDB, prefix, orgID, projID, "Salary", "income", "banknote", "chart-4")
	s.periodAug = seedPgPeriodRow(t, sqlDB, prefix, orgID, projID, "Aug 2026", "2026-08-01", "2026-08-31")
	s.periodSep = seedPgPeriodRow(t, sqlDB, prefix, orgID, projID, "Sep 2026", "2026-09-01", "")

	aug, sep := &s.periodAug, &s.periodSep
	for _, r := range []txnRow{
		{wallet: s.walletCash, kind: "opening", minor: 1_000_000, period: aug, at: "2026-08-01T01:00:00Z"},
		{wallet: s.walletCash, kind: "income", minor: 500_000, category: &s.catSalary, period: aug, at: "2026-08-05T03:00:00Z"},
		{wallet: s.walletCash, kind: "expense", minor: 200_000, category: &s.catFood, period: aug, at: "2026-08-06T03:00:00Z"},
		{wallet: s.walletCash, kind: "expense", minor: 50_000, period: aug, at: "2026-08-07T03:00:00Z"},
		{wallet: s.walletCash, toWallet: &s.walletBank, kind: "transfer", minor: 300_000, period: aug, at: "2026-08-10T03:00:00Z"},
		{wallet: s.walletBank, kind: "adjustment_out", minor: 10_000, period: aug, at: "2026-08-20T03:00:00Z"},
		{wallet: s.walletCash, kind: "income", minor: 100_000, category: &s.catSalary, at: "2026-08-15T03:00:00Z"},
		{wallet: s.walletBank, kind: "expense", minor: 25_000, category: &s.catFood, period: sep, at: "2026-09-03T03:00:00Z"},
		{wallet: s.walletCash, kind: "adjustment_in", minor: 5_000, period: sep, at: "2026-09-05T03:00:00Z"},
	} {
		seedPgTxn(t, sqlDB, prefix, orgID, projID, userID, r)
	}
	return s
}

func seedPgTxn(t *testing.T, sqlDB *sql.DB, prefix string, orgID, projID, userID uuid.UUID, r txnRow) {
	t.Helper()
	at, err := time.Parse(time.RFC3339, r.at)
	require.NoError(t, err)
	mustExecPg(t, sqlDB,
		"INSERT INTO "+prefix+"transactions (id, org_id, project_id, wallet_id, to_wallet_id, kind, amount_minor, currency, "+
			"category_id, period_id, note, note_norm, occurred_at, created_by, created_at, updated_at) "+
			"VALUES ($1, $2, $3, $4, $5, $6, $7, 'IDR', $8, $9, '', '', $10, $11, $10, $10)",
		uuid.New(), orgID, projID, r.wallet, pgUUID(r.toWallet), r.kind, r.minor,
		pgUUID(r.category), pgUUID(r.period), at, userID)
}

func pgUUID(id *uuid.UUID) any {
	if id == nil {
		return nil
	}
	return *id
}

func seedPgWalletRow(t *testing.T, sqlDB *sql.DB, prefix string, orgID, projID uuid.UUID, name, kind string, exclude bool) uuid.UUID {
	t.Helper()
	id := uuid.New()
	now := time.Now().UTC()
	mustExecPg(t, sqlDB,
		"INSERT INTO "+prefix+"wallets (id, org_id, project_id, name, kind, provider, currency, exclude_from_total, archived_at, created_at, updated_at) "+
			"VALUES ($1, $2, $3, $4, $5, '', 'IDR', $6, NULL, $7, $7)",
		id, orgID, projID, name, kind, exclude, now)
	return id
}

func seedPgCategoryRow(t *testing.T, sqlDB *sql.DB, prefix string, orgID, projID uuid.UUID, name, kind, icon, color string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	now := time.Now().UTC()
	mustExecPg(t, sqlDB,
		"INSERT INTO "+prefix+"categories (id, org_id, project_id, name, kind, icon, color, sort_order, archived_at, created_at, updated_at) "+
			"VALUES ($1, $2, $3, $4, $5, $6, $7, 0, NULL, $8, $8)",
		id, orgID, projID, name, kind, icon, color, now)
	return id
}

func seedPgPeriodRow(t *testing.T, sqlDB *sql.DB, prefix string, orgID, projID uuid.UUID, name, start, end string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	now := time.Now().UTC()
	var endArg any
	status := "open"
	if end != "" {
		endArg = end
		status = "closed"
	}
	mustExecPg(t, sqlDB,
		"INSERT INTO "+prefix+"periods (id, org_id, project_id, name, start_date, end_date, status, closed_at, snapshot, created_at, updated_at) "+
			"VALUES ($1, $2, $3, $4, $5, $6, $7, NULL, NULL, $8, $8)",
		id, orgID, projID, name, start, endArg, status, now)
	return id
}

func mustExecPg(t *testing.T, sqlDB *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := sqlDB.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}
