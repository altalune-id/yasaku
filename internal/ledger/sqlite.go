package ledger

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/go-jet/jet/v2/qrm"
	"github.com/go-jet/jet/v2/sqlite"
	"github.com/google/uuid"

	pdb "altalune.id/yasaku/internal/platform/db"
	sqliteent "altalune.id/yasaku/internal/platform/db/entity/sqlite"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/money"
)

type sqliteStore struct {
	db    *sql.DB
	table *sqliteent.LedgerSettings
}

func newSQLiteStore(db *sql.DB, tablePrefix string) *sqliteStore {
	return &sqliteStore{db: db, table: sqliteent.NewLedgerSettings(tablePrefix)}
}

// txAcquire enrolls in the caller's unit of work when one is active, so a Save never opens a
// second writer transaction against the same SQLite file.
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
		return nil, false, tenant.Context{}, fmt.Errorf("ledger.sqlite: begin: %w", err)
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
		return fmt.Errorf("ledger.sqlite: commit: %w", cerr)
	}
	return nil
}

type sqliteSettingsRow struct {
	ProjectID      string `alias:"ledger_settings.project_id"`
	OrgID          string `alias:"ledger_settings.org_id"`
	Timezone       string `alias:"ledger_settings.timezone"`
	Currency       string `alias:"ledger_settings.currency"`
	PeriodStartDay int64  `alias:"ledger_settings.period_start_day"`
	UpdatedAt      string `alias:"ledger_settings.updated_at"`
}

func (r *sqliteSettingsRow) toSettings() (*Settings, error) {
	oid, err := uuid.Parse(r.OrgID)
	if err != nil {
		return nil, fmt.Errorf("ledger.sqlite: parse org_id: %w", err)
	}
	pid, err := uuid.Parse(r.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("ledger.sqlite: parse project_id: %w", err)
	}
	ua, err := time.Parse(time.RFC3339Nano, r.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("ledger.sqlite: parse updated_at: %w", err)
	}
	return &Settings{
		OrgID:          oid,
		ProjectID:      pid,
		Timezone:       r.Timezone,
		Currency:       money.Currency(r.Currency),
		PeriodStartDay: int(r.PeriodStartDay),
		UpdatedAt:      ua.UTC(),
	}, nil
}

func (s *sqliteStore) ByProject(ctx context.Context, orgID, projectID uuid.UUID) (*Settings, error) {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return nil, err
	}
	if owned {
		defer func() { _ = tx.Rollback() }()
	}
	// SECURITY: SQLite has no RLS, so both the caller's argument and the request's tenant scope are checked here.
	stmt := sqlite.SELECT(s.table.AllColumns).
		FROM(s.table).
		WHERE(s.table.ProjectID.EQ(sqlite.String(projectID.String())).
			AND(s.table.OrgID.EQ(sqlite.String(orgID.String()))).
			AND(s.table.OrgID.EQ(sqlite.String(tc.OrgID.String())))).
		LIMIT(1)
	var row sqliteSettingsRow
	if qErr := stmt.QueryContext(ctx, tx, &row); qErr != nil {
		if errors.Is(qErr, qrm.ErrNoRows) || errors.Is(qErr, sql.ErrNoRows) {
			return nil, &NotFoundError{ProjectID: projectID.String()}
		}
		return nil, fmt.Errorf("ledger.sqlite.ByProject: %w", qErr)
	}
	return row.toSettings()
}

func (s *sqliteStore) Save(ctx context.Context, st *Settings) error {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return err
	}
	updatedAt := sqliteent.SQLiteTime(st.UpdatedAt)
	stmt := s.table.INSERT(s.table.AllColumns).
		VALUES(
			st.ProjectID.String(),
			st.OrgID.String(),
			st.Timezone,
			string(st.Currency),
			int64(st.PeriodStartDay),
			updatedAt,
		).
		ON_CONFLICT(s.table.ProjectID).
		// SECURITY: SQLite has no RLS, so this tenant predicate is the only thing stopping another org's project id from rewriting this row.
		DO_UPDATE(sqlite.SET(
			s.table.Timezone.SET(sqlite.String(st.Timezone)),
			s.table.Currency.SET(sqlite.String(string(st.Currency))),
			s.table.PeriodStartDay.SET(sqlite.Int(int64(st.PeriodStartDay))),
			s.table.UpdatedAt.SET(sqlite.String(updatedAt)),
		).WHERE(s.table.OrgID.EQ(sqlite.String(tc.OrgID.String()))))

	res, execErr := stmt.ExecContext(ctx, tx)
	if execErr != nil {
		return s.endTx(tx, owned, fmt.Errorf("ledger.sqlite.Save: %w", execErr))
	}
	n, raErr := res.RowsAffected()
	if raErr != nil {
		return s.endTx(tx, owned, fmt.Errorf("ledger.sqlite.Save: rows affected: %w", raErr))
	}
	// NOTE: an insert affects one row and so does a conflicting update the caller owns; zero
	// means the conflict-clause guard refused an upsert onto a row outside the caller's org.
	if n == 0 {
		return s.endTx(tx, owned, &NotFoundError{ProjectID: st.ProjectID.String()})
	}
	return s.endTx(tx, owned, nil)
}
