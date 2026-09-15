package category

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/go-jet/jet/v2/postgres"
	"github.com/go-jet/jet/v2/qrm"
	"github.com/google/uuid"

	"altalune.id/yasaku/internal/platform/tenant"
)

func (s *postgresStore) ByID(ctx context.Context, id uuid.UUID) (*Category, error) {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return nil, err
	}
	if owned {
		defer func() { _ = tx.Rollback() }()
	}
	// SECURITY: explicit org predicate, not RLS alone — a BYPASSRLS role would otherwise read another org's row.
	stmt := postgres.SELECT(s.table.AllColumns).
		FROM(s.table).
		WHERE(s.table.ID.EQ(postgres.UUID(id)).
			AND(s.table.OrgID.EQ(postgres.UUID(tc.OrgID)))).
		LIMIT(1)
	var row pgCategoryRow
	if qErr := stmt.QueryContext(ctx, tx, &row); qErr != nil {
		if errors.Is(qErr, qrm.ErrNoRows) || errors.Is(qErr, sql.ErrNoRows) {
			return nil, &NotFoundError{ID: id.String()}
		}
		return nil, fmt.Errorf("category.postgres.ByID: %w", qErr)
	}
	return row.toCategory(), nil
}

func (s *postgresStore) ByIDs(ctx context.Context, orgID, projectID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]*Category, error) {
	out := map[uuid.UUID]*Category{}
	if len(ids) == 0 {
		if _, err := tenant.From(ctx); err != nil {
			return nil, err
		}
		return out, nil
	}
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return nil, err
	}
	if owned {
		defer func() { _ = tx.Rollback() }()
	}
	wanted := make([]postgres.Expression, 0, len(ids))
	for _, id := range ids {
		wanted = append(wanted, postgres.UUID(id))
	}
	stmt := postgres.SELECT(s.table.AllColumns).
		FROM(s.table).
		WHERE(s.table.OrgID.EQ(postgres.UUID(orgID)).
			AND(s.table.OrgID.EQ(postgres.UUID(tc.OrgID))).
			AND(s.table.ProjectID.EQ(postgres.UUID(projectID))).
			AND(s.table.ID.IN(wanted...)))
	var rows []pgCategoryRow
	if qErr := stmt.QueryContext(ctx, tx, &rows); qErr != nil {
		return nil, fmt.Errorf("category.postgres.ByIDs: %w", qErr)
	}
	for i := range rows {
		c := rows[i].toCategory()
		out[c.ID] = c
	}
	return out, nil
}

func (s *postgresStore) List(ctx context.Context, orgID, projectID uuid.UUID, opts ListOpts) ([]*Category, error) {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return nil, err
	}
	if owned {
		defer func() { _ = tx.Rollback() }()
	}
	where := s.table.OrgID.EQ(postgres.UUID(orgID)).
		AND(s.table.OrgID.EQ(postgres.UUID(tc.OrgID))).
		AND(s.table.ProjectID.EQ(postgres.UUID(projectID)))
	if opts.Kind != "" {
		where = where.AND(s.table.Kind.EQ(postgres.String(string(opts.Kind))))
	}
	if !opts.IncludeArchived {
		where = where.AND(s.table.ArchivedAt.IS_NULL())
	}
	stmt := postgres.SELECT(s.table.AllColumns).
		FROM(s.table).
		WHERE(where).
		ORDER_BY(s.table.SortOrder.ASC(), s.table.Name.ASC(), s.table.ID.ASC())
	var rows []pgCategoryRow
	if qErr := stmt.QueryContext(ctx, tx, &rows); qErr != nil {
		return nil, fmt.Errorf("category.postgres.List: %w", qErr)
	}
	out := make([]*Category, 0, len(rows))
	for i := range rows {
		out = append(out, rows[i].toCategory())
	}
	return out, nil
}
