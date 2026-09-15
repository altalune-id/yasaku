package ledger

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/go-jet/jet/v2/postgres"
	"github.com/go-jet/jet/v2/qrm"
	"github.com/google/uuid"

	pdb "altalune.id/yasaku/internal/platform/db"
	pgent "altalune.id/yasaku/internal/platform/db/entity/postgres"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/money"
)

type postgresStore struct {
	pool  pdb.Pool
	pc    *tenant.PgConn
	table *pgent.LedgerSettings
}

func newPostgresStore(pool pdb.Pool, pc *tenant.PgConn, schema, tablePrefix string) *postgresStore {
	return &postgresStore{pool: pool, pc: pc, table: pgent.NewLedgerSettings(schema, tablePrefix)}
}

type pgSettingsRow struct {
	ProjectID      uuid.UUID `alias:"ledger_settings.project_id"`
	OrgID          uuid.UUID `alias:"ledger_settings.org_id"`
	Timezone       string    `alias:"ledger_settings.timezone"`
	Currency       string    `alias:"ledger_settings.currency"`
	PeriodStartDay int       `alias:"ledger_settings.period_start_day"`
	UpdatedAt      time.Time `alias:"ledger_settings.updated_at"`
}

func (r *pgSettingsRow) toSettings() *Settings {
	return &Settings{
		OrgID:          r.OrgID,
		ProjectID:      r.ProjectID,
		Timezone:       r.Timezone,
		Currency:       money.Currency(r.Currency),
		PeriodStartDay: r.PeriodStartDay,
		UpdatedAt:      r.UpdatedAt.UTC(),
	}
}

// txAcquire enrolls in the caller's unit of work when one is active, so a Save never opens a
// second transaction. SECURITY: the tenant is resolved first, so a missing one is a MissingError
// rather than a zero OrgID silently becoming the upsert guard's predicate.
func (s *postgresStore) txAcquire(ctx context.Context) (*sql.Tx, bool, tenant.Context, error) {
	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, false, tenant.Context{}, err
	}
	if tx, ok := pdb.CurrentTx(ctx); ok {
		return tx, false, tc, nil
	}
	tx, err := s.pc.BeginTenanted(ctx, tc)
	if err != nil {
		return nil, false, tenant.Context{}, fmt.Errorf("ledger.postgres: begin: %w", err)
	}
	return tx, true, tc, nil
}

func (s *postgresStore) endTx(tx *sql.Tx, owned bool, err error) error {
	if !owned {
		return err
	}
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	if cerr := tx.Commit(); cerr != nil {
		return fmt.Errorf("ledger.postgres: commit: %w", cerr)
	}
	return nil
}

func (s *postgresStore) ByProject(ctx context.Context, orgID, projectID uuid.UUID) (*Settings, error) {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return nil, err
	}
	if owned {
		defer func() { _ = tx.Rollback() }()
	}
	// SECURITY: both the caller's argument and the request's tenant scope must agree, so a mismatched orgID matches nothing.
	stmt := postgres.SELECT(s.table.AllColumns).
		FROM(s.table).
		WHERE(s.table.ProjectID.EQ(postgres.UUID(projectID)).
			AND(s.table.OrgID.EQ(postgres.UUID(orgID))).
			AND(s.table.OrgID.EQ(postgres.UUID(tc.OrgID)))).
		LIMIT(1)
	var row pgSettingsRow
	if qErr := stmt.QueryContext(ctx, tx, &row); qErr != nil {
		if errors.Is(qErr, qrm.ErrNoRows) || errors.Is(qErr, sql.ErrNoRows) {
			return nil, &NotFoundError{ProjectID: projectID.String()}
		}
		return nil, fmt.Errorf("ledger.postgres.ByProject: %w", qErr)
	}
	return row.toSettings(), nil
}

func (s *postgresStore) Save(ctx context.Context, st *Settings) error {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return err
	}
	stmt := s.table.INSERT(s.table.AllColumns).
		VALUES(st.ProjectID, st.OrgID, st.Timezone, string(st.Currency), st.PeriodStartDay, st.UpdatedAt.UTC()).
		ON_CONFLICT(s.table.ProjectID).
		// SECURITY: the conflict clause carries the tenant predicate so another org's project id cannot rewrite this row.
		DO_UPDATE(postgres.SET(
			s.table.Timezone.SET(postgres.String(st.Timezone)),
			s.table.Currency.SET(postgres.String(string(st.Currency))),
			s.table.PeriodStartDay.SET(postgres.Int(int64(st.PeriodStartDay))),
			s.table.UpdatedAt.SET(postgres.TimestampzT(st.UpdatedAt.UTC())),
		).WHERE(s.table.OrgID.EQ(postgres.UUID(tc.OrgID))))

	res, execErr := stmt.ExecContext(ctx, tx)
	if execErr != nil {
		return s.endTx(tx, owned, fmt.Errorf("ledger.postgres.Save: %w", execErr))
	}
	n, raErr := res.RowsAffected()
	if raErr != nil {
		return s.endTx(tx, owned, fmt.Errorf("ledger.postgres.Save: rows affected: %w", raErr))
	}
	// NOTE: an insert affects one row and so does a conflicting update the caller owns; zero
	// means the conflict-clause guard refused an upsert onto a row outside the caller's org.
	if n == 0 {
		return s.endTx(tx, owned, &NotFoundError{ProjectID: st.ProjectID.String()})
	}
	return s.endTx(tx, owned, nil)
}
