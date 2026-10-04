package period

import (
	"context"
	"fmt"

	"github.com/go-jet/jet/v2/postgres"
)

func (s *postgresStore) Save(ctx context.Context, p *Period) error {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return err
	}
	js, err := marshalSnapshot(p.Snapshot)
	if err != nil {
		return s.endTx(tx, owned, err)
	}
	stmt := s.insertValues(p, js).
		ON_CONFLICT(s.table.ID).
		// SECURITY: the conflict clause carries the tenant predicate; without it an attacker-supplied row id updates another org's row.
		DO_UPDATE(
			postgres.SET(
				s.table.Name.SET(postgres.String(p.Name)),
				s.table.StartDate.SET(pgDate(p.StartDate)),
				s.table.EndDate.SET(pgDatePtr(p.EndDate)),
				s.table.Status.SET(postgres.String(string(p.Status))),
				s.table.ClosedAt.SET(pgTimePtr(p.ClosedAt)),
				s.table.Snapshot.SET(pgJSON(js)),
				s.table.UpdatedAt.SET(postgres.TimestampzT(p.UpdatedAt.UTC())),
			).WHERE(s.table.OrgID.EQ(postgres.UUID(tc.OrgID)).AND(s.table.ProjectID.EQ(postgres.UUID(tc.ProjectID)))),
		)
	res, execErr := stmt.ExecContext(ctx, tx)
	if execErr != nil {
		if typed := translatePgError(execErr, p); typed != nil {
			return s.endTx(tx, owned, typed)
		}
		return s.endTx(tx, owned, fmt.Errorf("period.postgres.Save: %w", execErr))
	}
	n, raErr := res.RowsAffected()
	if raErr != nil {
		return s.endTx(tx, owned, fmt.Errorf("period.postgres.Save: rows affected: %w", raErr))
	}
	if n == 0 {
		return s.endTx(tx, owned, &NotFoundError{ID: p.ID.String()})
	}
	return s.endTx(tx, owned, nil)
}

func (s *postgresStore) insertValues(p *Period, js *string) postgres.InsertStatement {
	return s.table.INSERT(s.table.AllColumns).
		VALUES(
			postgres.UUID(p.ID),
			postgres.UUID(p.OrgID),
			postgres.UUID(p.ProjectID),
			postgres.String(p.Name),
			pgDate(p.StartDate),
			pgDatePtr(p.EndDate),
			postgres.String(string(p.Status)),
			pgTimePtr(p.ClosedAt),
			pgJSON(js),
			postgres.TimestampzT(p.CreatedAt.UTC()),
			postgres.TimestampzT(p.UpdatedAt.UTC()),
		)
}

// NOTE: ON CONFLICT DO NOTHING, not a caught unique violation: a violation aborts the caller's unit of work, and the re-read after a lost race must run in it.
func (s *postgresStore) CreateCurrent(ctx context.Context, p *Period) (bool, error) {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return false, err
	}
	// SECURITY: a period is only ever created inside the caller's own scope.
	if p.OrgID != tc.OrgID || p.ProjectID != tc.ProjectID {
		return false, s.endTx(tx, owned, &NotFoundError{ID: p.ID.String()})
	}
	js, err := marshalSnapshot(p.Snapshot)
	if err != nil {
		return false, s.endTx(tx, owned, err)
	}
	stmt := s.insertValues(p, js).
		ON_CONFLICT(s.table.ProjectID).WHERE(s.table.EndDate.IS_NULL()).DO_NOTHING()
	res, execErr := stmt.ExecContext(ctx, tx)
	if execErr != nil {
		return false, s.endTx(tx, owned, fmt.Errorf("period.postgres.CreateCurrent: %w", execErr))
	}
	n, raErr := res.RowsAffected()
	if raErr != nil {
		return false, s.endTx(tx, owned, fmt.Errorf("period.postgres.CreateCurrent: rows affected: %w", raErr))
	}
	return n == 1, s.endTx(tx, owned, nil)
}

func (s *postgresStore) SaveClosing(ctx context.Context, c *Closing) error {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return err
	}
	// SECURITY: a closing may only be appended inside the caller's own scope.
	if c.OrgID != tc.OrgID || c.ProjectID != tc.ProjectID {
		return s.endTx(tx, owned, &NotFoundError{ID: c.PeriodID.String()})
	}
	js, err := marshalSnapshot(&c.Snapshot)
	if err != nil {
		return s.endTx(tx, owned, err)
	}
	closedBy, closedByKey := pgAuthorExprs(c.ClosedBy, c.ClosedByKeyID)
	stmt := s.closings.INSERT(s.closings.AllColumns).
		VALUES(
			postgres.UUID(c.ID),
			postgres.UUID(c.OrgID),
			postgres.UUID(c.ProjectID),
			postgres.UUID(c.PeriodID),
			postgres.TimestampzT(c.ClosedAt.UTC()),
			closedBy,
			closedByKey,
			pgJSON(js),
		)
	if _, execErr := stmt.ExecContext(ctx, tx); execErr != nil {
		if typed := translatePgError(execErr, nil); IsAuthorMissingError(typed) {
			return s.endTx(tx, owned, typed)
		}
		return s.endTx(tx, owned, fmt.Errorf("period.postgres.SaveClosing: %w", execErr))
	}
	return s.endTx(tx, owned, nil)
}
