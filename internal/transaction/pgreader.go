package transaction

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/go-jet/jet/v2/postgres"
	"github.com/go-jet/jet/v2/qrm"
	"github.com/google/uuid"

	"altalune.id/yasaku/money"
)

func (s *postgresStore) ByID(ctx context.Context, id uuid.UUID) (*Transaction, error) {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return nil, err
	}
	if owned {
		defer func() { _ = tx.Rollback() }()
	}
	stmt := postgres.SELECT(s.table.AllColumns).
		FROM(s.table).
		WHERE(s.table.ID.EQ(postgres.UUID(id)).
			AND(s.table.OrgID.EQ(postgres.UUID(tc.OrgID)))).
		LIMIT(1)
	var row pgTxnRow
	if qErr := stmt.QueryContext(ctx, tx, &row); qErr != nil {
		if errors.Is(qErr, qrm.ErrNoRows) || errors.Is(qErr, sql.ErrNoRows) {
			return nil, &NotFoundError{ID: id.String()}
		}
		return nil, fmt.Errorf("transaction.postgres.ByID: %w", qErr)
	}
	return row.toTransaction(), nil
}

func (s *postgresStore) List(ctx context.Context, orgID, projectID uuid.UUID, opts ListOpts) ([]*Transaction, error) {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return nil, err
	}
	if owned {
		defer func() { _ = tx.Rollback() }()
	}
	stmt := postgres.SELECT(s.table.AllColumns).
		FROM(s.table).
		WHERE(s.pgListWhere(orgID, projectID, tc.OrgID, opts)).
		ORDER_BY(s.table.OccurredAt.DESC(), s.table.CreatedAt.DESC(), s.table.ID.DESC()).
		LIMIT(int64(opts.NormalizedLimit()))

	var rows []pgTxnRow
	if qErr := stmt.QueryContext(ctx, tx, &rows); qErr != nil {
		return nil, fmt.Errorf("transaction.postgres.List: %w", qErr)
	}
	out := make([]*Transaction, 0, len(rows))
	for i := range rows {
		out = append(out, rows[i].toTransaction())
	}
	return out, nil
}

func (s *postgresStore) pgListWhere(orgID, projectID, scopeOrgID uuid.UUID, opts ListOpts) postgres.BoolExpression {
	// SECURITY: both the caller's org and the request's tenant scope, so a mismatched pair matches nothing.
	where := s.table.OrgID.EQ(postgres.UUID(orgID)).
		AND(s.table.OrgID.EQ(postgres.UUID(scopeOrgID))).
		AND(s.table.ProjectID.EQ(postgres.UUID(projectID)))

	if opts.WalletID != nil {
		w := postgres.UUID(*opts.WalletID)
		where = where.AND(s.table.WalletID.EQ(w).OR(s.table.ToWalletID.EQ(w)))
	}
	if opts.CategoryID != nil {
		where = where.AND(s.table.CategoryID.EQ(postgres.UUID(*opts.CategoryID)))
	}
	if opts.PeriodID != nil {
		where = where.AND(s.table.PeriodID.EQ(postgres.UUID(*opts.PeriodID)))
	}
	if len(opts.Kinds) > 0 {
		vals := make([]postgres.Expression, 0, len(opts.Kinds))
		for _, k := range opts.Kinds {
			vals = append(vals, postgres.String(string(k)))
		}
		where = where.AND(s.table.Kind.IN(vals...))
	}
	if opts.From != nil {
		where = where.AND(s.table.OccurredAt.GT_EQ(postgres.TimestampzT(opts.From.UTC())))
	}
	if opts.To != nil {
		where = where.AND(s.table.OccurredAt.LT_EQ(postgres.TimestampzT(opts.To.UTC())))
	}
	if opts.Search != "" {
		where = where.AND(postgres.RawBool(
			`transactions.note ILIKE #pattern ESCAPE '\'`,
			postgres.RawArgs{"#pattern": escapeLikePattern(opts.Search)},
		))
	}
	if c := opts.After; c != nil {
		occurred := postgres.TimestampzT(c.OccurredAt.UTC())
		created := postgres.TimestampzT(c.CreatedAt.UTC())
		id := postgres.UUID(c.ID)
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

func (s *postgresStore) Balances(ctx context.Context, orgID, projectID uuid.UUID) (map[uuid.UUID]money.Amount, error) {
	tx, owned, tc, err := s.txAcquire(ctx)
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
	}

	own := fmt.Sprintf(`
SELECT wallet_id AS "balances.wallet_id",
       currency  AS "balances.currency",
       SUM(CASE WHEN kind IN ('income','opening','adjustment_in')
                THEN amount_minor ELSE -amount_minor END)::bigint AS "balances.total"
  FROM %s
 WHERE org_id = #orgID AND org_id = #scopeOrg AND project_id = #projectID
 GROUP BY wallet_id, currency`, s.qualified)

	incoming := fmt.Sprintf(`
SELECT to_wallet_id AS "balances.wallet_id",
       currency     AS "balances.currency",
       SUM(amount_minor)::bigint AS "balances.total"
  FROM %s
 WHERE org_id = #orgID AND org_id = #scopeOrg AND project_id = #projectID
   AND kind = 'transfer' AND to_wallet_id IS NOT NULL
 GROUP BY to_wallet_id, currency`, s.qualified)

	out := map[uuid.UUID]money.Amount{}
	for _, q := range []string{own, incoming} {
		var rows []pgBalanceRow
		if qErr := postgres.RawStatement(q, args).QueryContext(ctx, tx, &rows); qErr != nil {
			return nil, fmt.Errorf("transaction.postgres.Balances: %w", qErr)
		}
		for _, r := range rows {
			accumulate(out, r.WalletID, money.New(r.Total, money.Currency(r.Currency)))
		}
	}
	return out, nil
}

func (s *postgresStore) Balance(ctx context.Context, orgID, projectID, walletID uuid.UUID) (money.Amount, error) {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return money.Amount{}, err
	}
	if owned {
		defer func() { _ = tx.Rollback() }()
	}
	query := fmt.Sprintf(`
SELECT #walletID::uuid AS "balances.wallet_id",
       currency        AS "balances.currency",
       SUM(delta)::bigint AS "balances.total"
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
  ) moves
 GROUP BY currency
 ORDER BY currency
 LIMIT 1`, s.qualified)

	var rows []pgBalanceRow
	qErr := postgres.RawStatement(query, postgres.RawArgs{
		"#orgID":     orgID,
		"#scopeOrg":  tc.OrgID,
		"#projectID": projectID,
		"#walletID":  walletID,
	}).QueryContext(ctx, tx, &rows)
	if qErr != nil {
		return money.Amount{}, fmt.Errorf("transaction.postgres.Balance: %w", qErr)
	}
	if len(rows) == 0 {
		return money.Amount{}, nil
	}
	return money.New(rows[0].Total, money.Currency(rows[0].Currency)), nil
}

func (s *postgresStore) LastCategoryForNote(ctx context.Context, orgID, projectID uuid.UUID, noteNorm string) (uuid.UUID, bool, error) {
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
	stmt := postgres.SELECT(s.table.CategoryID.AS("suggestion.category_id")).
		FROM(s.table).
		WHERE(s.table.OrgID.EQ(postgres.UUID(orgID)).
			AND(s.table.OrgID.EQ(postgres.UUID(tc.OrgID))).
			AND(s.table.ProjectID.EQ(postgres.UUID(projectID))).
			AND(s.table.NoteNorm.EQ(postgres.String(noteNorm))).
			AND(s.table.CategoryID.IS_NOT_NULL())).
		ORDER_BY(s.table.OccurredAt.DESC(), s.table.CreatedAt.DESC(), s.table.ID.DESC()).
		LIMIT(1)

	var row struct {
		CategoryID uuid.UUID `alias:"suggestion.category_id"`
	}
	if qErr := stmt.QueryContext(ctx, tx, &row); qErr != nil {
		if errors.Is(qErr, qrm.ErrNoRows) || errors.Is(qErr, sql.ErrNoRows) {
			return uuid.Nil, false, nil
		}
		return uuid.Nil, false, fmt.Errorf("transaction.postgres.LastCategoryForNote: %w", qErr)
	}
	return row.CategoryID, true, nil
}

func accumulate(out map[uuid.UUID]money.Amount, id uuid.UUID, delta money.Amount) {
	cur, ok := out[id]
	if !ok || !cur.SameCurrency(delta) {
		out[id] = delta
		return
	}
	out[id] = cur.Add(delta)
}
