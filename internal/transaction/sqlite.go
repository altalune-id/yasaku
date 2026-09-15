package transaction

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/go-jet/jet/v2/qrm"
	"github.com/go-jet/jet/v2/sqlite"
	"github.com/google/uuid"
	sqlitedrv "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	pdb "altalune.id/yasaku/internal/platform/db"
	sqliteent "altalune.id/yasaku/internal/platform/db/entity/sqlite"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/money"
)

type sqliteStore struct {
	db        *sql.DB
	table     *sqliteent.Transactions
	qualified string
}

func newSQLiteStore(sqlDB *sql.DB, tablePrefix string) *sqliteStore {
	return &sqliteStore{
		db:        sqlDB,
		table:     sqliteent.NewTransactions(tablePrefix),
		qualified: tablePrefix + "transactions",
	}
}

type sqliteTxnRow struct {
	ID          string  `alias:"transactions.id"`
	OrgID       string  `alias:"transactions.org_id"`
	ProjectID   string  `alias:"transactions.project_id"`
	WalletID    string  `alias:"transactions.wallet_id"`
	ToWalletID  *string `alias:"transactions.to_wallet_id"`
	Kind        string  `alias:"transactions.kind"`
	AmountMinor int64   `alias:"transactions.amount_minor"`
	Currency    string  `alias:"transactions.currency"`
	CategoryID  *string `alias:"transactions.category_id"`
	PeriodID    *string `alias:"transactions.period_id"`
	Note        string  `alias:"transactions.note"`
	NoteNorm    string  `alias:"transactions.note_norm"`
	OccurredAt  string  `alias:"transactions.occurred_at"`
	CreatedBy   string  `alias:"transactions.created_by"`
	CreatedAt   string  `alias:"transactions.created_at"`
	UpdatedAt   string  `alias:"transactions.updated_at"`
}

func (r *sqliteTxnRow) toTransaction() (*Transaction, error) {
	id, err := parseUUID(r.ID, "id")
	if err != nil {
		return nil, err
	}
	orgID, err := parseUUID(r.OrgID, "org_id")
	if err != nil {
		return nil, err
	}
	projectID, err := parseUUID(r.ProjectID, "project_id")
	if err != nil {
		return nil, err
	}
	walletID, err := parseUUID(r.WalletID, "wallet_id")
	if err != nil {
		return nil, err
	}
	toWalletID, err := parseOptionalUUID(r.ToWalletID, "to_wallet_id")
	if err != nil {
		return nil, err
	}
	categoryID, err := parseOptionalUUID(r.CategoryID, "category_id")
	if err != nil {
		return nil, err
	}
	periodID, err := parseOptionalUUID(r.PeriodID, "period_id")
	if err != nil {
		return nil, err
	}
	createdBy, err := parseUUID(r.CreatedBy, "created_by")
	if err != nil {
		return nil, err
	}
	occurredAt, err := parseSQLiteTime(r.OccurredAt, "occurred_at")
	if err != nil {
		return nil, err
	}
	createdAt, err := parseSQLiteTime(r.CreatedAt, "created_at")
	if err != nil {
		return nil, err
	}
	updatedAt, err := parseSQLiteTime(r.UpdatedAt, "updated_at")
	if err != nil {
		return nil, err
	}
	return &Transaction{
		ID:         id,
		OrgID:      orgID,
		ProjectID:  projectID,
		WalletID:   walletID,
		ToWalletID: toWalletID,
		Kind:       Kind(r.Kind),
		Amount:     money.New(r.AmountMinor, money.Currency(r.Currency)),
		CategoryID: categoryID,
		PeriodID:   periodID,
		Note:       r.Note,
		OccurredAt: occurredAt,
		CreatedBy:  createdBy,
		CreatedAt:  createdAt,
		UpdatedAt:  updatedAt,
	}, nil
}

func parseUUID(s, col string) (uuid.UUID, error) {
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil, fmt.Errorf("transaction.sqlite: parse %s: %w", col, err)
	}
	return id, nil
}

func parseOptionalUUID(s *string, col string) (*uuid.UUID, error) {
	if s == nil || *s == "" {
		return nil, nil //nolint:nilnil // a NULL column is legitimately no id and no error.
	}
	id, err := parseUUID(*s, col)
	if err != nil {
		return nil, err
	}
	return &id, nil
}

func parseSQLiteTime(s, col string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("transaction.sqlite: parse %s: %w", col, err)
	}
	return t.UTC(), nil
}

func sqliteUUIDArg(id *uuid.UUID) any {
	if id == nil {
		return nil
	}
	return id.String()
}

func sqliteUUIDExpr(id *uuid.UUID) sqlite.StringExpression {
	if id == nil {
		return sqlite.StringExp(sqlite.NULL)
	}
	return sqlite.String(id.String())
}

func (s *sqliteStore) txAcquire(ctx context.Context) (*sql.Tx, bool, tenant.Context, error) {
	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, false, tenant.Context{}, err
	}
	if tx, ok := pdb.CurrentTx(ctx); ok {
		return tx, false, tc, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, tenant.Context{}, fmt.Errorf("transaction.sqlite: begin: %w", err)
	}
	return tx, true, tc, nil
}

func (s *sqliteStore) endTx(tx *sql.Tx, owned bool, err error) error {
	if !owned {
		return err
	}
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	if cerr := tx.Commit(); cerr != nil {
		return fmt.Errorf("transaction.sqlite: commit: %w", cerr)
	}
	return nil
}

// NOTE: SQLite names no constraint, so the offending id cannot be derived and is left empty.
func translateSQLiteConstraint(err error) error {
	var sqliteErr *sqlitedrv.Error
	if !errors.As(err, &sqliteErr) {
		return nil
	}
	if sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_FOREIGNKEY {
		return &NotFoundError{}
	}
	return nil
}

func (s *sqliteStore) Save(ctx context.Context, t *Transaction) error {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return err
	}
	noteNorm := NormalizeNote(t.Note)
	occurredAt := sqliteent.SQLiteTime(t.OccurredAt)
	updatedAt := sqliteent.SQLiteTime(t.UpdatedAt)

	stmt := s.table.INSERT(s.table.AllColumns).
		VALUES(
			t.ID.String(), t.OrgID.String(), t.ProjectID.String(), t.WalletID.String(),
			sqliteUUIDArg(t.ToWalletID), string(t.Kind), t.Amount.Minor, string(t.Amount.Currency),
			sqliteUUIDArg(t.CategoryID), sqliteUUIDArg(t.PeriodID),
			t.Note, noteNorm, occurredAt, t.CreatedBy.String(),
			sqliteent.SQLiteTime(t.CreatedAt), updatedAt,
		).
		ON_CONFLICT(s.table.ID).
		// SECURITY: SQLite has no RLS, so this tenant predicate is the only thing stopping an attacker-supplied row id from rewriting another org's row.
		DO_UPDATE(
			sqlite.SET(
				s.table.WalletID.SET(sqlite.String(t.WalletID.String())),
				s.table.ToWalletID.SET(sqliteUUIDExpr(t.ToWalletID)),
				s.table.Kind.SET(sqlite.String(string(t.Kind))),
				s.table.AmountMinor.SET(sqlite.Int(t.Amount.Minor)),
				s.table.Currency.SET(sqlite.String(string(t.Amount.Currency))),
				s.table.CategoryID.SET(sqliteUUIDExpr(t.CategoryID)),
				s.table.PeriodID.SET(sqliteUUIDExpr(t.PeriodID)),
				s.table.Note.SET(sqlite.String(t.Note)),
				s.table.NoteNorm.SET(sqlite.String(noteNorm)),
				s.table.OccurredAt.SET(sqlite.String(occurredAt)),
				s.table.UpdatedAt.SET(sqlite.String(updatedAt)),
			).WHERE(s.table.OrgID.EQ(sqlite.String(tc.OrgID.String()))),
		)

	res, execErr := stmt.ExecContext(ctx, tx)
	if execErr != nil {
		if typed := translateSQLiteConstraint(execErr); typed != nil {
			return s.endTx(tx, owned, typed)
		}
		return s.endTx(tx, owned, fmt.Errorf("transaction.sqlite.Save: %w", execErr))
	}
	n, raErr := res.RowsAffected()
	if raErr != nil {
		return s.endTx(tx, owned, fmt.Errorf("transaction.sqlite.Save: rows affected: %w", raErr))
	}
	if n == 0 {
		return s.endTx(tx, owned, &NotFoundError{ID: t.ID.String()})
	}
	return s.endTx(tx, owned, nil)
}

func (s *sqliteStore) ByID(ctx context.Context, id uuid.UUID) (*Transaction, error) {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return nil, err
	}
	if owned {
		defer func() { _ = tx.Rollback() }()
	}
	stmt := sqlite.SELECT(s.table.AllColumns).
		FROM(s.table).
		WHERE(s.table.ID.EQ(sqlite.String(id.String())).
			AND(s.table.OrgID.EQ(sqlite.String(tc.OrgID.String())))).
		LIMIT(1)
	var row sqliteTxnRow
	if qErr := stmt.QueryContext(ctx, tx, &row); qErr != nil {
		if errors.Is(qErr, qrm.ErrNoRows) || errors.Is(qErr, sql.ErrNoRows) {
			return nil, &NotFoundError{ID: id.String()}
		}
		return nil, fmt.Errorf("transaction.sqlite.ByID: %w", qErr)
	}
	return row.toTransaction()
}

func (s *sqliteStore) List(ctx context.Context, orgID, projectID uuid.UUID, opts ListOpts) ([]*Transaction, error) {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return nil, err
	}
	if owned {
		defer func() { _ = tx.Rollback() }()
	}
	stmt := sqlite.SELECT(s.table.AllColumns).
		FROM(s.table).
		WHERE(s.sqliteListWhere(orgID, projectID, tc.OrgID, opts)).
		ORDER_BY(s.table.OccurredAt.DESC(), s.table.CreatedAt.DESC(), s.table.ID.DESC()).
		LIMIT(int64(opts.NormalizedLimit()))

	var rows []sqliteTxnRow
	if qErr := stmt.QueryContext(ctx, tx, &rows); qErr != nil {
		return nil, fmt.Errorf("transaction.sqlite.List: %w", qErr)
	}
	out := make([]*Transaction, 0, len(rows))
	for i := range rows {
		t, cErr := rows[i].toTransaction()
		if cErr != nil {
			return nil, cErr
		}
		out = append(out, t)
	}
	return out, nil
}

func (s *sqliteStore) sqliteListWhere(orgID, projectID, scopeOrgID uuid.UUID, opts ListOpts) sqlite.BoolExpression {
	// SECURITY: SQLite has no RLS; both predicates are the only tenant guard on this read.
	where := s.table.OrgID.EQ(sqlite.String(orgID.String())).
		AND(s.table.OrgID.EQ(sqlite.String(scopeOrgID.String()))).
		AND(s.table.ProjectID.EQ(sqlite.String(projectID.String())))

	if opts.WalletID != nil {
		w := sqlite.String(opts.WalletID.String())
		where = where.AND(s.table.WalletID.EQ(w).OR(s.table.ToWalletID.EQ(w)))
	}
	if opts.CategoryID != nil {
		where = where.AND(s.table.CategoryID.EQ(sqlite.String(opts.CategoryID.String())))
	}
	if opts.PeriodID != nil {
		where = where.AND(s.table.PeriodID.EQ(sqlite.String(opts.PeriodID.String())))
	}
	if len(opts.Kinds) > 0 {
		vals := make([]sqlite.Expression, 0, len(opts.Kinds))
		for _, k := range opts.Kinds {
			vals = append(vals, sqlite.String(string(k)))
		}
		where = where.AND(s.table.Kind.IN(vals...))
	}
	if opts.From != nil {
		where = where.AND(s.table.OccurredAt.GT_EQ(sqlite.String(sqliteent.SQLiteTime(*opts.From))))
	}
	if opts.To != nil {
		where = where.AND(s.table.OccurredAt.LT_EQ(sqlite.String(sqliteent.SQLiteTime(*opts.To))))
	}
	if opts.Search != "" {
		where = where.AND(sqlite.RawBool(
			`LOWER(transactions.note) LIKE LOWER(#pattern) ESCAPE '\'`,
			sqlite.RawArgs{"#pattern": escapeLikePattern(opts.Search)},
		))
	}
	if c := opts.After; c != nil {
		occurred := sqlite.String(sqliteent.SQLiteTime(c.OccurredAt))
		created := sqlite.String(sqliteent.SQLiteTime(c.CreatedAt))
		id := sqlite.String(c.ID.String())
		where = where.AND(
			s.table.OccurredAt.LT(occurred).OR(
				s.table.OccurredAt.EQ(occurred).AND(
					s.table.CreatedAt.LT(created).OR(
						s.table.CreatedAt.EQ(created).AND(s.table.ID.LT(id)),
					),
				),
			),
		)
	}
	return where
}

type sqliteBalanceRow struct {
	WalletID string `alias:"balances.wallet_id"`
	Currency string `alias:"balances.currency"`
	Total    int64  `alias:"balances.total"`
}

func (s *sqliteStore) Balances(ctx context.Context, orgID, projectID uuid.UUID) (map[uuid.UUID]money.Amount, error) {
	tx, owned, tc, err := s.txAcquire(ctx)
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
	}

	own := fmt.Sprintf(`
SELECT wallet_id AS "balances.wallet_id",
       currency  AS "balances.currency",
       SUM(CASE WHEN kind IN ('income','opening','adjustment_in')
                THEN amount_minor ELSE -amount_minor END) AS "balances.total"
  FROM %s
 WHERE org_id = #orgID AND org_id = #scopeOrg AND project_id = #projectID
 GROUP BY wallet_id, currency`, s.qualified)

	incoming := fmt.Sprintf(`
SELECT to_wallet_id AS "balances.wallet_id",
       currency     AS "balances.currency",
       SUM(amount_minor) AS "balances.total"
  FROM %s
 WHERE org_id = #orgID AND org_id = #scopeOrg AND project_id = #projectID
   AND kind = 'transfer' AND to_wallet_id IS NOT NULL
 GROUP BY to_wallet_id, currency`, s.qualified)

	out := map[uuid.UUID]money.Amount{}
	for _, q := range []string{own, incoming} {
		var rows []sqliteBalanceRow
		if qErr := sqlite.RawStatement(q, args).QueryContext(ctx, tx, &rows); qErr != nil {
			return nil, fmt.Errorf("transaction.sqlite.Balances: %w", qErr)
		}
		for _, r := range rows {
			id, pErr := parseUUID(r.WalletID, "wallet_id")
			if pErr != nil {
				return nil, pErr
			}
			accumulate(out, id, money.New(r.Total, money.Currency(r.Currency)))
		}
	}
	return out, nil
}

func (s *sqliteStore) Balance(ctx context.Context, orgID, projectID, walletID uuid.UUID) (money.Amount, error) {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return money.Amount{}, err
	}
	if owned {
		defer func() { _ = tx.Rollback() }()
	}
	query := fmt.Sprintf(`
SELECT #walletID AS "balances.wallet_id",
       currency  AS "balances.currency",
       SUM(delta) AS "balances.total"
  FROM (
    SELECT currency,
           CASE WHEN kind IN ('income','opening','adjustment_in')
                THEN amount_minor ELSE -amount_minor END AS delta
      FROM %[1]s
     WHERE org_id = #orgID AND org_id = #scopeOrg AND project_id = #projectID
       AND wallet_id = #walletID
    UNION ALL
    SELECT currency, amount_minor AS delta
      FROM %[1]s
     WHERE org_id = #orgID AND org_id = #scopeOrg AND project_id = #projectID
       AND kind = 'transfer' AND to_wallet_id = #walletID
  )
 GROUP BY currency
 ORDER BY currency
 LIMIT 1`, s.qualified)

	var rows []sqliteBalanceRow
	qErr := sqlite.RawStatement(query, sqlite.RawArgs{
		"#orgID":     orgID.String(),
		"#scopeOrg":  tc.OrgID.String(),
		"#projectID": projectID.String(),
		"#walletID":  walletID.String(),
	}).QueryContext(ctx, tx, &rows)
	if qErr != nil {
		return money.Amount{}, fmt.Errorf("transaction.sqlite.Balance: %w", qErr)
	}
	if len(rows) == 0 {
		return money.Amount{}, nil
	}
	return money.New(rows[0].Total, money.Currency(rows[0].Currency)), nil
}

func (s *sqliteStore) LastCategoryForNote(ctx context.Context, orgID, projectID uuid.UUID, noteNorm string) (uuid.UUID, bool, error) {
	if noteNorm == "" {
		return uuid.Nil, false, nil
	}
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return uuid.Nil, false, err
	}
	if owned {
		defer func() { _ = tx.Rollback() }()
	}
	stmt := sqlite.SELECT(s.table.CategoryID.AS("suggestion.category_id")).
		FROM(s.table).
		WHERE(s.table.OrgID.EQ(sqlite.String(orgID.String())).
			AND(s.table.OrgID.EQ(sqlite.String(tc.OrgID.String()))).
			AND(s.table.ProjectID.EQ(sqlite.String(projectID.String()))).
			AND(s.table.NoteNorm.EQ(sqlite.String(noteNorm))).
			AND(s.table.CategoryID.IS_NOT_NULL())).
		ORDER_BY(s.table.OccurredAt.DESC(), s.table.CreatedAt.DESC(), s.table.ID.DESC()).
		LIMIT(1)

	var row struct {
		CategoryID string `alias:"suggestion.category_id"`
	}
	if qErr := stmt.QueryContext(ctx, tx, &row); qErr != nil {
		if errors.Is(qErr, qrm.ErrNoRows) || errors.Is(qErr, sql.ErrNoRows) {
			return uuid.Nil, false, nil
		}
		return uuid.Nil, false, fmt.Errorf("transaction.sqlite.LastCategoryForNote: %w", qErr)
	}
	id, pErr := parseUUID(row.CategoryID, "category_id")
	if pErr != nil {
		return uuid.Nil, false, pErr
	}
	return id, true, nil
}

func (s *sqliteStore) Delete(ctx context.Context, id uuid.UUID) error {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return err
	}
	stmt := s.table.DELETE().
		WHERE(s.table.ID.EQ(sqlite.String(id.String())).
			AND(s.table.OrgID.EQ(sqlite.String(tc.OrgID.String()))))
	res, execErr := stmt.ExecContext(ctx, tx)
	if execErr != nil {
		return s.endTx(tx, owned, fmt.Errorf("transaction.sqlite.Delete: %w", execErr))
	}
	n, raErr := res.RowsAffected()
	if raErr != nil {
		return s.endTx(tx, owned, fmt.Errorf("transaction.sqlite.Delete: rows affected: %w", raErr))
	}
	if n == 0 {
		return s.endTx(tx, owned, &NotFoundError{ID: id.String()})
	}
	return s.endTx(tx, owned, nil)
}

// SECURITY: SQLite has no advisory locks, so a caller that forgot its unit of work must fail loudly rather than race silently.
// NOTE: an ambient transaction is necessary but not sufficient here — SQLite begins deferred, so a concurrent adjustment surfaces as SQLITE_BUSY on the write rather than being serialized at the read.
func (s *sqliteStore) LockWallet(ctx context.Context, _, _, _ uuid.UUID) error {
	if _, ok := pdb.CurrentTx(ctx); !ok {
		return fmt.Errorf("transaction.sqlite.LockWallet: %w", ErrNoUnitOfWork)
	}
	_, err := tenant.From(ctx)
	return err
}
