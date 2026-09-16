package wallet

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/go-jet/jet/v2/postgres"
	"github.com/go-jet/jet/v2/qrm"
	"github.com/google/uuid"
)

func (s *postgresStore) ByID(ctx context.Context, id uuid.UUID) (*Wallet, error) {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return nil, err
	}
	if owned {
		defer func() { _ = tx.Rollback() }()
	}
	// SECURITY: explicit org predicate, not RLS alone — a BYPASSRLS role slips past policies.
	stmt := postgres.SELECT(s.table.AllColumns).
		FROM(s.table).
		WHERE(s.table.ID.EQ(postgres.UUID(id)).
			AND(s.table.OrgID.EQ(postgres.UUID(tc.OrgID)))).
		LIMIT(1)
	var row pgWalletRow
	if qErr := stmt.QueryContext(ctx, tx, &row); qErr != nil {
		if errors.Is(qErr, qrm.ErrNoRows) || errors.Is(qErr, sql.ErrNoRows) {
			return nil, &NotFoundError{ID: id.String()}
		}
		return nil, fmt.Errorf("wallet.postgres.ByID: %w", qErr)
	}
	return row.toWallet(), nil
}

func (s *postgresStore) List(ctx context.Context, orgID, projectID uuid.UUID, opts ListOpts) ([]*Wallet, error) {
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
	if !opts.IncludeArchived {
		where = where.AND(s.table.ArchivedAt.IS_NULL())
	}
	stmt := postgres.SELECT(s.table.AllColumns).
		FROM(s.table).
		WHERE(where).
		ORDER_BY(s.table.Name.ASC(), s.table.ID.ASC())
	var rows []pgWalletRow
	if qErr := stmt.QueryContext(ctx, tx, &rows); qErr != nil {
		return nil, fmt.Errorf("wallet.postgres.List: %w", qErr)
	}
	out := make([]*Wallet, 0, len(rows))
	for i := range rows {
		out = append(out, rows[i].toWallet())
	}
	return out, nil
}
