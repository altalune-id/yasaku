package wallet

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
	db    *sql.DB
	table *sqliteent.Wallets
}

func newSQLiteStore(db *sql.DB, tablePrefix string) *sqliteStore {
	return &sqliteStore{db: db, table: sqliteent.NewWallets(tablePrefix)}
}

// txAcquire enrolls in the caller's unit of work when one is active, so a write inside
// db.RunInTx rolls back with it and a multi-statement write never opens a second SQLite
// writer transaction against the same file.
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
		return nil, false, tenant.Context{}, fmt.Errorf("wallet.sqlite: begin: %w", err)
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
		return fmt.Errorf("wallet.sqlite: commit: %w", cerr)
	}
	return nil
}

type sqliteWalletRow struct {
	ID               string  `alias:"wallets.id"`
	OrgID            string  `alias:"wallets.org_id"`
	ProjectID        string  `alias:"wallets.project_id"`
	Name             string  `alias:"wallets.name"`
	Kind             string  `alias:"wallets.kind"`
	Provider         string  `alias:"wallets.provider"`
	Currency         string  `alias:"wallets.currency"`
	ExcludeFromTotal int64   `alias:"wallets.exclude_from_total"`
	ArchivedAt       *string `alias:"wallets.archived_at"`
	CreatedAt        string  `alias:"wallets.created_at"`
	UpdatedAt        string  `alias:"wallets.updated_at"`
}

func (r *sqliteWalletRow) toWallet() (*Wallet, error) {
	id, err := uuid.Parse(r.ID)
	if err != nil {
		return nil, fmt.Errorf("wallet.sqlite: parse id: %w", err)
	}
	oid, err := uuid.Parse(r.OrgID)
	if err != nil {
		return nil, fmt.Errorf("wallet.sqlite: parse org_id: %w", err)
	}
	pid, err := uuid.Parse(r.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("wallet.sqlite: parse project_id: %w", err)
	}
	ca, err := time.Parse(time.RFC3339Nano, r.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("wallet.sqlite: parse created_at: %w", err)
	}
	ua, err := time.Parse(time.RFC3339Nano, r.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("wallet.sqlite: parse updated_at: %w", err)
	}
	w := &Wallet{
		ID:               id,
		OrgID:            oid,
		ProjectID:        pid,
		Name:             r.Name,
		Kind:             Kind(r.Kind),
		Provider:         r.Provider,
		Currency:         money.Currency(r.Currency),
		ExcludeFromTotal: r.ExcludeFromTotal == 1,
		CreatedAt:        ca.UTC(),
		UpdatedAt:        ua.UTC(),
	}
	if r.ArchivedAt != nil && *r.ArchivedAt != "" {
		aa, aErr := time.Parse(time.RFC3339Nano, *r.ArchivedAt)
		if aErr != nil {
			return nil, fmt.Errorf("wallet.sqlite: parse archived_at: %w", aErr)
		}
		aa = aa.UTC()
		w.ArchivedAt = &aa
	}
	return w, nil
}

func (s *sqliteStore) Save(ctx context.Context, w *Wallet) error {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return err
	}
	exclude := int64(0)
	if w.ExcludeFromTotal {
		exclude = 1
	}
	updatedAt := sqliteent.SQLiteTime(w.UpdatedAt)
	// SECURITY: the conflict clause carries the tenant predicate; SQLite has no RLS behind it,
	// so without this a Save holding another org's row id would rewrite that row.
	stmt := s.table.INSERT(s.table.AllColumns).
		VALUES(
			w.ID.String(),
			w.OrgID.String(),
			w.ProjectID.String(),
			w.Name,
			string(w.Kind),
			w.Provider,
			string(w.Currency),
			exclude,
			sqliteNullableTime(w.ArchivedAt),
			sqliteent.SQLiteTime(w.CreatedAt),
			updatedAt,
		).
		ON_CONFLICT(s.table.ID).
		DO_UPDATE(
			sqlite.SET(
				s.table.Name.SET(sqlite.String(w.Name)),
				s.table.Kind.SET(sqlite.String(string(w.Kind))),
				s.table.Provider.SET(sqlite.String(w.Provider)),
				s.table.Currency.SET(sqlite.String(string(w.Currency))),
				s.table.ExcludeFromTotal.SET(sqlite.Int(exclude)),
				s.table.ArchivedAt.SET(sqliteNullableTimeExpr(w.ArchivedAt)),
				s.table.UpdatedAt.SET(sqlite.String(updatedAt)),
			).WHERE(s.table.OrgID.EQ(sqlite.String(tc.OrgID.String()))),
		)
	res, execErr := stmt.ExecContext(ctx, tx)
	if execErr != nil {
		if typed := translateSQLiteSaveError(execErr, w.Name); typed != nil {
			return s.endTx(tx, owned, typed)
		}
		return s.endTx(tx, owned, fmt.Errorf("wallet.sqlite.Save: %w", execErr))
	}
	n, raErr := res.RowsAffected()
	if raErr != nil {
		return s.endTx(tx, owned, fmt.Errorf("wallet.sqlite.Save: rows affected: %w", raErr))
	}
	// NOTE: an insert affects one row and so does a conflicting update the caller owns; zero
	// means the conflict-clause guard refused an upsert onto a row outside the caller's org.
	if n == 0 {
		return s.endTx(tx, owned, &NotFoundError{ID: w.ID.String()})
	}
	return s.endTx(tx, owned, nil)
}

func (s *sqliteStore) ByID(ctx context.Context, id uuid.UUID) (*Wallet, error) {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return nil, err
	}
	if owned {
		defer func() { _ = tx.Rollback() }()
	}
	// SECURITY: explicit org predicate; SQLite has no row level security at all.
	stmt := sqlite.SELECT(s.table.AllColumns).
		FROM(s.table).
		WHERE(s.table.ID.EQ(sqlite.String(id.String())).
			AND(s.table.OrgID.EQ(sqlite.String(tc.OrgID.String())))).
		LIMIT(1)
	var row sqliteWalletRow
	if qErr := stmt.QueryContext(ctx, tx, &row); qErr != nil {
		if errors.Is(qErr, qrm.ErrNoRows) || errors.Is(qErr, sql.ErrNoRows) {
			return nil, &NotFoundError{ID: id.String()}
		}
		return nil, fmt.Errorf("wallet.sqlite.ByID: %w", qErr)
	}
	return row.toWallet()
}

func (s *sqliteStore) List(ctx context.Context, orgID, projectID uuid.UUID, opts ListOpts) ([]*Wallet, error) {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return nil, err
	}
	if owned {
		defer func() { _ = tx.Rollback() }()
	}
	where := s.table.OrgID.EQ(sqlite.String(orgID.String())).
		AND(s.table.OrgID.EQ(sqlite.String(tc.OrgID.String()))).
		AND(s.table.ProjectID.EQ(sqlite.String(projectID.String())))
	if !opts.IncludeArchived {
		where = where.AND(s.table.ArchivedAt.IS_NULL())
	}
	stmt := sqlite.SELECT(s.table.AllColumns).
		FROM(s.table).
		WHERE(where).
		ORDER_BY(s.table.Name.ASC(), s.table.ID.ASC())
	var rows []sqliteWalletRow
	if qErr := stmt.QueryContext(ctx, tx, &rows); qErr != nil {
		return nil, fmt.Errorf("wallet.sqlite.List: %w", qErr)
	}
	out := make([]*Wallet, 0, len(rows))
	for i := range rows {
		w, cErr := rows[i].toWallet()
		if cErr != nil {
			return nil, cErr
		}
		out = append(out, w)
	}
	return out, nil
}

func (s *sqliteStore) Delete(ctx context.Context, id uuid.UUID) error {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return err
	}
	// SECURITY: org predicate; SQLite has no row level security at all.
	stmt := s.table.DELETE().
		WHERE(s.table.ID.EQ(sqlite.String(id.String())).
			AND(s.table.OrgID.EQ(sqlite.String(tc.OrgID.String()))))
	res, execErr := stmt.ExecContext(ctx, tx)
	if execErr != nil {
		if typed := translateSQLiteDeleteError(execErr, id.String()); typed != nil {
			return s.endTx(tx, owned, typed)
		}
		return s.endTx(tx, owned, fmt.Errorf("wallet.sqlite.Delete: %w", execErr))
	}
	n, raErr := res.RowsAffected()
	if raErr != nil {
		return s.endTx(tx, owned, fmt.Errorf("wallet.sqlite.Delete: rows affected: %w", raErr))
	}
	if n == 0 {
		return s.endTx(tx, owned, &NotFoundError{ID: id.String()})
	}
	return s.endTx(tx, owned, nil)
}

func sqliteNullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return sqliteent.SQLiteTime(*t)
}

func sqliteNullableTimeExpr(t *time.Time) sqlite.StringExpression {
	if t == nil {
		return sqlite.StringExp(sqlite.NULL)
	}
	return sqlite.String(sqliteent.SQLiteTime(*t))
}

func sqliteCode(err error) (int, bool) {
	var sqliteErr *sqlitedrv.Error
	if !errors.As(err, &sqliteErr) {
		return 0, false
	}
	return sqliteErr.Code(), true
}

func translateSQLiteSaveError(err error, name string) error {
	code, ok := sqliteCode(err)
	if !ok {
		return nil
	}
	if code == sqlite3.SQLITE_CONSTRAINT_UNIQUE || code == sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY {
		return &AlreadyExistsError{Name: name}
	}
	return nil
}

// NOTE: SQLite raises an ON DELETE RESTRICT violation as SQLITE_CONSTRAINT_TRIGGER
// (FK actions run as internal triggers); only insert-side violations use FOREIGNKEY.
func translateSQLiteDeleteError(err error, id string) error {
	code, ok := sqliteCode(err)
	if !ok {
		return nil
	}
	if code == sqlite3.SQLITE_CONSTRAINT_FOREIGNKEY || code == sqlite3.SQLITE_CONSTRAINT_TRIGGER {
		return &InUseError{ID: id}
	}
	return nil
}
