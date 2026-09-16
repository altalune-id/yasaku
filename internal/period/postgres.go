package period

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/go-jet/jet/v2/postgres"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"altalune.id/yasaku/civil"
	pdb "altalune.id/yasaku/internal/platform/db"
	pgent "altalune.id/yasaku/internal/platform/db/entity/postgres"
	"altalune.id/yasaku/internal/platform/tenant"
)

type postgresStore struct {
	pool     pdb.Pool
	pc       *tenant.PgConn
	table    *pgent.Periods
	closings *pgent.PeriodClosings
}

func newPostgresStore(pool pdb.Pool, pc *tenant.PgConn, schema, tablePrefix string) *postgresStore {
	return &postgresStore{
		pool:     pool,
		pc:       pc,
		table:    pgent.NewPeriods(schema, tablePrefix),
		closings: pgent.NewPeriodClosings(schema, tablePrefix),
	}
}

type pgPeriodRow struct {
	ID        uuid.UUID   `alias:"periods.id"`
	OrgID     uuid.UUID   `alias:"periods.org_id"`
	ProjectID uuid.UUID   `alias:"periods.project_id"`
	Name      string      `alias:"periods.name"`
	StartDate civil.Date  `alias:"periods.start_date"`
	EndDate   *civil.Date `alias:"periods.end_date"`
	Status    string      `alias:"periods.status"`
	ClosedAt  *time.Time  `alias:"periods.closed_at"`
	Snapshot  jsonDoc     `alias:"periods.snapshot"`
	CreatedAt time.Time   `alias:"periods.created_at"`
	UpdatedAt time.Time   `alias:"periods.updated_at"`
}

func (r *pgPeriodRow) toPeriod() (*Period, error) {
	snap, err := unmarshalSnapshot(r.Snapshot.raw)
	if err != nil {
		return nil, err
	}
	return &Period{
		ID:        r.ID,
		OrgID:     r.OrgID,
		ProjectID: r.ProjectID,
		Name:      r.Name,
		StartDate: r.StartDate,
		EndDate:   r.EndDate,
		Status:    Status(r.Status),
		ClosedAt:  utcPtr(r.ClosedAt),
		Snapshot:  snap,
		CreatedAt: r.CreatedAt.UTC(),
		UpdatedAt: r.UpdatedAt.UTC(),
	}, nil
}

type pgClosingRow struct {
	ID        uuid.UUID `alias:"period_closings.id"`
	OrgID     uuid.UUID `alias:"period_closings.org_id"`
	ProjectID uuid.UUID `alias:"period_closings.project_id"`
	PeriodID  uuid.UUID `alias:"period_closings.period_id"`
	ClosedAt  time.Time `alias:"period_closings.closed_at"`
	ClosedBy  uuid.UUID `alias:"period_closings.closed_by"`
	Snapshot  jsonDoc   `alias:"period_closings.snapshot"`
}

func (r *pgClosingRow) toClosing() (*Closing, error) {
	snap, err := unmarshalSnapshot(r.Snapshot.raw)
	if err != nil {
		return nil, err
	}
	c := &Closing{
		ID:        r.ID,
		OrgID:     r.OrgID,
		ProjectID: r.ProjectID,
		PeriodID:  r.PeriodID,
		ClosedAt:  r.ClosedAt.UTC(),
		ClosedBy:  r.ClosedBy,
	}
	if snap != nil {
		c.Snapshot = *snap
	}
	return c, nil
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

func pgDate(d civil.Date) postgres.DateExpression { return postgres.Date(d.Year, d.Month, d.Day) }

func pgDatePtr(d *civil.Date) postgres.DateExpression {
	if d == nil {
		return pgent.NullDate()
	}
	return pgDate(*d)
}

func pgTimePtr(t *time.Time) postgres.TimestampzExpression {
	if t == nil {
		return pgent.NullTimestampz()
	}
	return postgres.TimestampzT(t.UTC())
}

// NOTE: jet binds a string literal as text, which Postgres will not implicitly coerce into the JSONB column; the explicit cast is required.
func pgJSON(js *string) postgres.StringExpression {
	if js == nil {
		return pgent.NullJSONB()
	}
	return postgres.StringExp(postgres.CAST(postgres.String(*js)).AS("jsonb"))
}

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
		return nil, false, tenant.Context{}, fmt.Errorf("period.postgres: begin: %w", err)
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
		return fmt.Errorf("period.postgres: commit: %w", cerr)
	}
	return nil
}

// SECURITY: the only unique index a conflicting write can reach is the partial current-period one, so 23505 always means a second current period.
func translatePgError(err error, p *Period) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return nil
	}
	if pgErr.Code == "23505" {
		return newOverlapError(p)
	}
	return nil
}

func newOverlapError(p *Period) *OverlapError {
	e := &OverlapError{}
	if p != nil {
		e.Start = p.StartDate.String()
		if p.EndDate != nil {
			e.End = p.EndDate.String()
		}
	}
	return e
}
