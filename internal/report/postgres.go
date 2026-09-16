package report

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/go-jet/jet/v2/postgres"
	"github.com/go-jet/jet/v2/qrm"
	"github.com/google/uuid"

	"altalune.id/yasaku/civil"
	pdb "altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/money"
)

type postgresReader struct {
	pool   pdb.Pool
	pc     *tenant.PgConn
	tables tableNames
}

func newPostgresReader(pool pdb.Pool, pc *tenant.PgConn, schema, tablePrefix string) *postgresReader {
	if schema == "" {
		schema = "public"
	}
	return &postgresReader{pool: pool, pc: pc, tables: newTableNames(schema, tablePrefix)}
}

var _ Reader = (*postgresReader)(nil)

func (r *postgresReader) txAcquire(ctx context.Context) (*sql.Tx, bool, tenant.Context, error) {
	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, false, tenant.Context{}, err
	}
	if tx, ok := pdb.CurrentTx(ctx); ok {
		return tx, false, tc, nil
	}
	tx, err := r.pc.BeginTenanted(ctx, tc)
	if err != nil {
		return nil, false, tenant.Context{}, fmt.Errorf("report.postgres: begin: %w", err)
	}
	return tx, true, tc, nil
}

type pgPeriodRow struct {
	ID    uuid.UUID  `alias:"period.id"`
	Name  string     `alias:"period.name"`
	Start time.Time  `alias:"period.start"`
	End   *time.Time `alias:"period.end"`
}

func (r *postgresReader) Period(ctx context.Context, orgID, projectID, periodID uuid.UUID) (PeriodRef, error) {
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

	var row pgPeriodRow
	qErr := postgres.RawStatement(query, postgres.RawArgs{
		"#periodID":  periodID,
		"#orgID":     orgID,
		"#scopeOrg":  tc.OrgID,
		"#projectID": projectID,
	}).QueryContext(ctx, tx, &row)
	if qErr != nil {
		if errors.Is(qErr, qrm.ErrNoRows) || errors.Is(qErr, sql.ErrNoRows) {
			return PeriodRef{}, fmt.Errorf("report.postgres.Period: %s: %w", periodID, errPeriodAbsent)
		}
		return PeriodRef{}, fmt.Errorf("report.postgres.Period: %w", qErr)
	}
	return pgPeriodRef(row.ID, row.Name, row.Start, row.End), nil
}

type pgTotalsRow struct {
	Income  int64 `alias:"totals.income"`
	Expense int64 `alias:"totals.expense"`
	TxCount int   `alias:"totals.tx_count"`
}

type pgWalletRow struct {
	WalletID uuid.UUID `alias:"line.wallet_id"`
	Name     string    `alias:"line.name"`
	Kind     string    `alias:"line.kind"`
	Exclude  bool      `alias:"line.exclude"`
	Opening  int64     `alias:"line.opening"`
	In       int64     `alias:"line.in"`
	Out      int64     `alias:"line.out"`
}

func (r *postgresReader) Summary(ctx context.Context, orgID, projectID, periodID uuid.UUID, currency money.Currency, startUTC time.Time) (PeriodSummary, error) {
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
	args := postgres.RawArgs{
		"#orgID":     orgID,
		"#scopeOrg":  tc.OrgID,
		"#projectID": projectID,
		"#periodID":  periodID,
		"#currency":  string(currency),
	}

	var totals pgTotalsRow
	if qErr := postgres.RawStatement(summaryTotalsSQL(r.tables, true), args).QueryContext(ctx, tx, &totals); qErr != nil {
		if !errors.Is(qErr, qrm.ErrNoRows) && !errors.Is(qErr, sql.ErrNoRows) {
			return PeriodSummary{}, fmt.Errorf("report.postgres.Summary: totals: %w", qErr)
		}
	}

	lineArgs := postgres.RawArgs{"#startUTC": startUTC.UTC()}
	for k, v := range args {
		lineArgs[k] = v
	}
	var rows []pgWalletRow
	if qErr := postgres.RawStatement(walletLinesSQL(r.tables, true), lineArgs).QueryContext(ctx, tx, &rows); qErr != nil {
		return PeriodSummary{}, fmt.Errorf("report.postgres.Summary: wallets: %w", qErr)
	}
	moves := make([]walletMovement, 0, len(rows))
	for _, row := range rows {
		moves = append(moves, walletMovement{
			WalletID:         row.WalletID,
			Name:             row.Name,
			Kind:             row.Kind,
			ExcludeFromTotal: row.Exclude,
			Opening:          row.Opening,
			In:               row.In,
			Out:              row.Out,
		})
	}
	return assembleSummary(ref, currency, totals.Income, totals.Expense, totals.TxCount, moves), nil
}

type pgSliceRow struct {
	CategoryID *uuid.UUID `alias:"slice.category_id"`
	Name       string     `alias:"slice.name"`
	Icon       string     `alias:"slice.icon"`
	Color      string     `alias:"slice.color"`
	Amount     int64      `alias:"slice.amount"`
	Count      int        `alias:"slice.count"`
}

func (r *postgresReader) SpendByCategory(ctx context.Context, orgID, projectID, periodID uuid.UUID, currency money.Currency) ([]CategorySlice, error) {
	return r.categorySlices(ctx, orgID, projectID, periodID, currency, kindExpense)
}

func (r *postgresReader) IncomeByCategory(ctx context.Context, orgID, projectID, periodID uuid.UUID, currency money.Currency) ([]CategorySlice, error) {
	return r.categorySlices(ctx, orgID, projectID, periodID, currency, kindIncome)
}

func (r *postgresReader) categorySlices(ctx context.Context, orgID, projectID, periodID uuid.UUID, currency money.Currency, kind string) ([]CategorySlice, error) {
	tx, owned, tc, err := r.txAcquire(ctx)
	if err != nil {
		return nil, err
	}
	if owned {
		defer func() { _ = tx.Rollback() }()
	}
	var rows []pgSliceRow
	qErr := postgres.RawStatement(categorySlicesSQL(r.tables, true), postgres.RawArgs{
		"#orgID":     orgID,
		"#scopeOrg":  tc.OrgID,
		"#projectID": projectID,
		"#periodID":  periodID,
		"#currency":  string(currency),
		"#kind":      kind,
	}).QueryContext(ctx, tx, &rows)
	if qErr != nil {
		return nil, fmt.Errorf("report.postgres.categorySlices: %w", qErr)
	}
	out := make([]CategorySlice, 0, len(rows))
	for _, row := range rows {
		out = append(out, CategorySlice{
			CategoryID: row.CategoryID,
			Name:       row.Name,
			Icon:       row.Icon,
			Color:      row.Color,
			Amount:     money.New(row.Amount, currency),
			Count:      row.Count,
		})
	}
	return assignShares(out), nil
}

type pgFlowRow struct {
	WalletID     uuid.UUID  `alias:"flow.wallet_id"`
	WalletName   string     `alias:"flow.wallet_name"`
	CategoryID   *uuid.UUID `alias:"flow.category_id"`
	CategoryName string     `alias:"flow.category_name"`
	Amount       int64      `alias:"flow.amount"`
}

func (r *postgresReader) Flows(ctx context.Context, orgID, projectID, periodID uuid.UUID, currency money.Currency) ([]Flow, error) {
	tx, owned, tc, err := r.txAcquire(ctx)
	if err != nil {
		return nil, err
	}
	if owned {
		defer func() { _ = tx.Rollback() }()
	}
	var rows []pgFlowRow
	qErr := postgres.RawStatement(flowsSQL(r.tables, true), postgres.RawArgs{
		"#orgID":     orgID,
		"#scopeOrg":  tc.OrgID,
		"#projectID": projectID,
		"#periodID":  periodID,
		"#currency":  string(currency),
	}).QueryContext(ctx, tx, &rows)
	if qErr != nil {
		return nil, fmt.Errorf("report.postgres.Flows: %w", qErr)
	}
	out := make([]Flow, 0, len(rows))
	for _, row := range rows {
		out = append(out, Flow{
			WalletID:     row.WalletID,
			WalletName:   row.WalletName,
			CategoryID:   row.CategoryID,
			CategoryName: row.CategoryName,
			Amount:       money.New(row.Amount, currency),
		})
	}
	return out, nil
}

type pgPointRow struct {
	PeriodID uuid.UUID  `alias:"point.period_id"`
	Name     string     `alias:"point.name"`
	Start    time.Time  `alias:"point.start"`
	End      *time.Time `alias:"point.end"`
	Income   int64      `alias:"point.income"`
	Expense  int64      `alias:"point.expense"`
}

func (r *postgresReader) Cashflow(ctx context.Context, orgID, projectID uuid.UUID, periodIDs []uuid.UUID, currency money.Currency) ([]CashflowPoint, error) {
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
	args := postgres.RawArgs{
		"#orgID":     orgID,
		"#scopeOrg":  tc.OrgID,
		"#projectID": projectID,
		"#currency":  string(currency),
	}
	for token, id := range tokens.args {
		args[token] = id
	}
	var rows []pgPointRow
	if qErr := postgres.RawStatement(cashflowSQL(r.tables, true, tokens.list), args).QueryContext(ctx, tx, &rows); qErr != nil {
		return nil, fmt.Errorf("report.postgres.Cashflow: %w", qErr)
	}
	found := make(map[uuid.UUID]CashflowPoint, len(rows))
	for _, row := range rows {
		ref := pgPeriodRef(row.PeriodID, row.Name, row.Start, row.End)
		found[ref.ID] = cashflowPoint(ref, row.Income, row.Expense, currency)
	}
	return orderCashflow(periodIDs, found), nil
}

type pgBalanceLineRow struct {
	WalletID uuid.UUID `alias:"line.wallet_id"`
	Name     string    `alias:"line.name"`
	Kind     string    `alias:"line.kind"`
	Exclude  bool      `alias:"line.exclude"`
	Currency string    `alias:"line.currency"`
	In       int64     `alias:"line.in"`
	Out      int64     `alias:"line.out"`
}

func (r *postgresReader) WalletBalances(ctx context.Context, orgID, projectID uuid.UUID) ([]WalletLine, error) {
	tx, owned, tc, err := r.txAcquire(ctx)
	if err != nil {
		return nil, err
	}
	if owned {
		defer func() { _ = tx.Rollback() }()
	}
	var rows []pgBalanceLineRow
	qErr := postgres.RawStatement(walletBalancesSQL(r.tables, true), postgres.RawArgs{
		"#orgID":     orgID,
		"#scopeOrg":  tc.OrgID,
		"#projectID": projectID,
	}).QueryContext(ctx, tx, &rows)
	if qErr != nil {
		return nil, fmt.Errorf("report.postgres.WalletBalances: %w", qErr)
	}
	out := make([]WalletLine, 0, len(rows))
	for _, row := range rows {
		move := walletMovement{
			WalletID:         row.WalletID,
			Name:             row.Name,
			Kind:             row.Kind,
			ExcludeFromTotal: row.Exclude,
			In:               row.In,
			Out:              row.Out,
		}
		out = append(out, move.line(money.Currency(row.Currency)))
	}
	return out, nil
}

func pgPeriodRef(id uuid.UUID, name string, start time.Time, end *time.Time) PeriodRef {
	ref := PeriodRef{ID: id, Name: name, Start: civil.DateOf(start, time.UTC)}
	if end != nil {
		d := civil.DateOf(*end, time.UTC)
		ref.End = &d
	}
	return ref
}
