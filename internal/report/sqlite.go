package report

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/go-jet/jet/v2/qrm"
	"github.com/go-jet/jet/v2/sqlite"
	"github.com/google/uuid"

	"altalune.id/yasaku/civil"
	pdb "altalune.id/yasaku/internal/platform/db"
	sqliteent "altalune.id/yasaku/internal/platform/db/entity/sqlite"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/money"
)

type sqliteReader struct {
	db     *sql.DB
	tables tableNames
}

func newSQLiteReader(sqlDB *sql.DB, tablePrefix string) *sqliteReader {
	return &sqliteReader{db: sqlDB, tables: newTableNames("", tablePrefix)}
}

var _ Reader = (*sqliteReader)(nil)

func (r *sqliteReader) txAcquire(ctx context.Context) (*sql.Tx, bool, tenant.Context, error) {
	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, false, tenant.Context{}, err
	}
	if tx, ok := pdb.CurrentTx(ctx); ok {
		return tx, false, tc, nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, tenant.Context{}, fmt.Errorf("report.sqlite: begin: %w", err)
	}
	return tx, true, tc, nil
}

type sqlitePeriodRow struct {
	ID    string  `alias:"period.id"`
	Name  string  `alias:"period.name"`
	Start string  `alias:"period.start"`
	End   *string `alias:"period.end"`
}

func (r *sqliteReader) Period(ctx context.Context, orgID, projectID, periodID uuid.UUID) (PeriodRef, error) {
	tx, owned, tc, err := r.txAcquire(ctx)
	if err != nil {
		return PeriodRef{}, err
	}
	if owned {
		defer func() { _ = tx.Rollback() }()
	}
	query := fmt.Sprintf(`
SELECT id         AS "period.id",
       name       AS "period.name",
       start_date AS "period.start",
       end_date   AS "period.end"
  FROM %s
 WHERE id = #periodID AND org_id = #orgID AND org_id = #scopeOrg AND project_id = #projectID
 LIMIT 1`, r.tables.periods)

	var row sqlitePeriodRow
	qErr := sqlite.RawStatement(query, sqlite.RawArgs{
		"#periodID":  periodID.String(),
		"#orgID":     orgID.String(),
		"#scopeOrg":  tc.OrgID.String(),
		"#projectID": projectID.String(),
	}).QueryContext(ctx, tx, &row)
	if qErr != nil {
		if errors.Is(qErr, qrm.ErrNoRows) || errors.Is(qErr, sql.ErrNoRows) {
			return PeriodRef{}, fmt.Errorf("report.sqlite.Period: %s: %w", periodID, errPeriodAbsent)
		}
		return PeriodRef{}, fmt.Errorf("report.sqlite.Period: %w", qErr)
	}
	return periodRef(row.ID, row.Name, row.Start, row.End)
}

type sqliteTotalsRow struct {
	Income  int64 `alias:"totals.income"`
	Expense int64 `alias:"totals.expense"`
	TxCount int   `alias:"totals.tx_count"`
}

type sqliteWalletRow struct {
	WalletID string `alias:"line.wallet_id"`
	Name     string `alias:"line.name"`
	Kind     string `alias:"line.kind"`
	Exclude  int    `alias:"line.exclude"`
	Opening  int64  `alias:"line.opening"`
	In       int64  `alias:"line.in"`
	Out      int64  `alias:"line.out"`
}

func (r *sqliteReader) Summary(ctx context.Context, orgID, projectID, periodID uuid.UUID, currency money.Currency, startUTC time.Time) (PeriodSummary, error) {
	ref, err := r.Period(ctx, orgID, projectID, periodID)
	if err != nil {
		return PeriodSummary{}, err
	}
	tx, owned, tc, err := r.txAcquire(ctx)
	if err != nil {
		return PeriodSummary{}, err
	}
	if owned {
		defer func() { _ = tx.Rollback() }()
	}
	args := sqlite.RawArgs{
		"#orgID":     orgID.String(),
		"#scopeOrg":  tc.OrgID.String(),
		"#projectID": projectID.String(),
		"#periodID":  periodID.String(),
		"#currency":  string(currency),
	}

	var totals sqliteTotalsRow
	if qErr := sqlite.RawStatement(summaryTotalsSQL(r.tables, false), args).QueryContext(ctx, tx, &totals); qErr != nil {
		if !errors.Is(qErr, qrm.ErrNoRows) && !errors.Is(qErr, sql.ErrNoRows) {
			return PeriodSummary{}, fmt.Errorf("report.sqlite.Summary: totals: %w", qErr)
		}
	}

	lineArgs := sqlite.RawArgs{"#startUTC": sqliteent.SQLiteTime(startUTC)}
	for k, v := range args {
		lineArgs[k] = v
	}
	var rows []sqliteWalletRow
	if qErr := sqlite.RawStatement(walletLinesSQL(r.tables, false), lineArgs).QueryContext(ctx, tx, &rows); qErr != nil {
		return PeriodSummary{}, fmt.Errorf("report.sqlite.Summary: wallets: %w", qErr)
	}
	moves := make([]walletMovement, 0, len(rows))
	for _, row := range rows {
		id, pErr := uuid.Parse(row.WalletID)
		if pErr != nil {
			return PeriodSummary{}, fmt.Errorf("report.sqlite.Summary: parse wallet_id: %w", pErr)
		}
		moves = append(moves, walletMovement{
			WalletID:         id,
			Name:             row.Name,
			Kind:             row.Kind,
			ExcludeFromTotal: row.Exclude != 0,
			Opening:          row.Opening,
			In:               row.In,
			Out:              row.Out,
		})
	}
	return assembleSummary(ref, currency, totals.Income, totals.Expense, totals.TxCount, moves), nil
}

type sqliteSliceRow struct {
	CategoryID *string `alias:"slice.category_id"`
	Name       string  `alias:"slice.name"`
	Icon       string  `alias:"slice.icon"`
	Color      string  `alias:"slice.color"`
	Amount     int64   `alias:"slice.amount"`
	Count      int     `alias:"slice.count"`
}

func (r *sqliteReader) SpendByCategory(ctx context.Context, orgID, projectID, periodID uuid.UUID, currency money.Currency) ([]CategorySlice, error) {
	return r.categorySlices(ctx, orgID, projectID, periodID, currency, kindExpense)
}

func (r *sqliteReader) IncomeByCategory(ctx context.Context, orgID, projectID, periodID uuid.UUID, currency money.Currency) ([]CategorySlice, error) {
	return r.categorySlices(ctx, orgID, projectID, periodID, currency, kindIncome)
}

func (r *sqliteReader) categorySlices(ctx context.Context, orgID, projectID, periodID uuid.UUID, currency money.Currency, kind string) ([]CategorySlice, error) {
	tx, owned, tc, err := r.txAcquire(ctx)
	if err != nil {
		return nil, err
	}
	if owned {
		defer func() { _ = tx.Rollback() }()
	}
	var rows []sqliteSliceRow
	qErr := sqlite.RawStatement(categorySlicesSQL(r.tables, false), sqlite.RawArgs{
		"#orgID":     orgID.String(),
		"#scopeOrg":  tc.OrgID.String(),
		"#projectID": projectID.String(),
		"#periodID":  periodID.String(),
		"#currency":  string(currency),
		"#kind":      kind,
	}).QueryContext(ctx, tx, &rows)
	if qErr != nil {
		return nil, fmt.Errorf("report.sqlite.categorySlices: %w", qErr)
	}
	out := make([]CategorySlice, 0, len(rows))
	for _, row := range rows {
		id, pErr := optionalUUID(row.CategoryID)
		if pErr != nil {
			return nil, fmt.Errorf("report.sqlite.categorySlices: %w", pErr)
		}
		out = append(out, CategorySlice{
			CategoryID: id,
			Name:       row.Name,
			Icon:       row.Icon,
			Color:      row.Color,
			Amount:     money.New(row.Amount, currency),
			Count:      row.Count,
		})
	}
	return assignShares(out), nil
}

type sqliteFlowRow struct {
	WalletID     string  `alias:"flow.wallet_id"`
	WalletName   string  `alias:"flow.wallet_name"`
	CategoryID   *string `alias:"flow.category_id"`
	CategoryName string  `alias:"flow.category_name"`
	Amount       int64   `alias:"flow.amount"`
}

func (r *sqliteReader) Flows(ctx context.Context, orgID, projectID, periodID uuid.UUID, currency money.Currency) ([]Flow, error) {
	tx, owned, tc, err := r.txAcquire(ctx)
	if err != nil {
		return nil, err
	}
	if owned {
		defer func() { _ = tx.Rollback() }()
	}
	var rows []sqliteFlowRow
	qErr := sqlite.RawStatement(flowsSQL(r.tables, false), sqlite.RawArgs{
		"#orgID":     orgID.String(),
		"#scopeOrg":  tc.OrgID.String(),
		"#projectID": projectID.String(),
		"#periodID":  periodID.String(),
		"#currency":  string(currency),
	}).QueryContext(ctx, tx, &rows)
	if qErr != nil {
		return nil, fmt.Errorf("report.sqlite.Flows: %w", qErr)
	}
	out := make([]Flow, 0, len(rows))
	for _, row := range rows {
		walletID, pErr := uuid.Parse(row.WalletID)
		if pErr != nil {
			return nil, fmt.Errorf("report.sqlite.Flows: parse wallet_id: %w", pErr)
		}
		categoryID, cErr := optionalUUID(row.CategoryID)
		if cErr != nil {
			return nil, fmt.Errorf("report.sqlite.Flows: %w", cErr)
		}
		out = append(out, Flow{
			WalletID:     walletID,
			WalletName:   row.WalletName,
			CategoryID:   categoryID,
			CategoryName: row.CategoryName,
			Amount:       money.New(row.Amount, currency),
		})
	}
	return out, nil
}

type sqlitePointRow struct {
	PeriodID string  `alias:"point.period_id"`
	Name     string  `alias:"point.name"`
	Start    string  `alias:"point.start"`
	End      *string `alias:"point.end"`
	Income   int64   `alias:"point.income"`
	Expense  int64   `alias:"point.expense"`
}

func (r *sqliteReader) Cashflow(ctx context.Context, orgID, projectID uuid.UUID, periodIDs []uuid.UUID, currency money.Currency) ([]CashflowPoint, error) {
	if len(periodIDs) == 0 {
		return nil, nil
	}
	tokens, err := periodTokens(periodIDs)
	if err != nil {
		return nil, err
	}
	tx, owned, tc, err := r.txAcquire(ctx)
	if err != nil {
		return nil, err
	}
	if owned {
		defer func() { _ = tx.Rollback() }()
	}
	args := sqlite.RawArgs{
		"#orgID":     orgID.String(),
		"#scopeOrg":  tc.OrgID.String(),
		"#projectID": projectID.String(),
		"#currency":  string(currency),
	}
	for token, id := range tokens.args {
		args[token] = id.String()
	}
	var rows []sqlitePointRow
	if qErr := sqlite.RawStatement(cashflowSQL(r.tables, false, tokens.list), args).QueryContext(ctx, tx, &rows); qErr != nil {
		return nil, fmt.Errorf("report.sqlite.Cashflow: %w", qErr)
	}
	found := make(map[uuid.UUID]CashflowPoint, len(rows))
	for _, row := range rows {
		ref, pErr := periodRef(row.PeriodID, row.Name, row.Start, row.End)
		if pErr != nil {
			return nil, fmt.Errorf("report.sqlite.Cashflow: %w", pErr)
		}
		found[ref.ID] = cashflowPoint(ref, row.Income, row.Expense, currency)
	}
	return orderCashflow(periodIDs, found), nil
}

type sqliteBalanceRow struct {
	WalletID string `alias:"line.wallet_id"`
	Name     string `alias:"line.name"`
	Kind     string `alias:"line.kind"`
	Exclude  int    `alias:"line.exclude"`
	Currency string `alias:"line.currency"`
	In       int64  `alias:"line.in"`
	Out      int64  `alias:"line.out"`
}

func (r *sqliteReader) WalletBalances(ctx context.Context, orgID, projectID uuid.UUID) ([]WalletLine, error) {
	tx, owned, tc, err := r.txAcquire(ctx)
	if err != nil {
		return nil, err
	}
	if owned {
		defer func() { _ = tx.Rollback() }()
	}
	var rows []sqliteBalanceRow
	qErr := sqlite.RawStatement(walletBalancesSQL(r.tables, false), sqlite.RawArgs{
		"#orgID":     orgID.String(),
		"#scopeOrg":  tc.OrgID.String(),
		"#projectID": projectID.String(),
	}).QueryContext(ctx, tx, &rows)
	if qErr != nil {
		return nil, fmt.Errorf("report.sqlite.WalletBalances: %w", qErr)
	}
	out := make([]WalletLine, 0, len(rows))
	for _, row := range rows {
		id, pErr := uuid.Parse(row.WalletID)
		if pErr != nil {
			return nil, fmt.Errorf("report.sqlite.WalletBalances: parse wallet_id: %w", pErr)
		}
		move := walletMovement{
			WalletID:         id,
			Name:             row.Name,
			Kind:             row.Kind,
			ExcludeFromTotal: row.Exclude != 0,
			In:               row.In,
			Out:              row.Out,
		}
		out = append(out, move.line(money.Currency(row.Currency)))
	}
	return out, nil
}

func optionalUUID(s *string) (*uuid.UUID, error) {
	if s == nil || *s == "" {
		return nil, nil //nolint:nilnil // a NULL column is legitimately no id and no error.
	}
	id, err := uuid.Parse(*s)
	if err != nil {
		return nil, fmt.Errorf("parse uuid: %w", err)
	}
	return &id, nil
}

func periodRef(id, name, start string, end *string) (PeriodRef, error) {
	pid, err := uuid.Parse(id)
	if err != nil {
		return PeriodRef{}, fmt.Errorf("parse period id: %w", err)
	}
	startDate, err := civil.ParseDate(start)
	if err != nil {
		return PeriodRef{}, fmt.Errorf("parse start_date: %w", err)
	}
	ref := PeriodRef{ID: pid, Name: name, Start: startDate}
	if end != nil && *end != "" {
		endDate, eErr := civil.ParseDate(*end)
		if eErr != nil {
			return PeriodRef{}, fmt.Errorf("parse end_date: %w", eErr)
		}
		ref.End = &endDate
	}
	return ref, nil
}
