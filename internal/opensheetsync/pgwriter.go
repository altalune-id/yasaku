package opensheetsync

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/go-jet/jet/v2/postgres"
	"github.com/go-jet/jet/v2/qrm"
	"github.com/google/uuid"

	pgent "altalune.id/yasaku/internal/platform/db/entity/postgres"
)

type pgStateRow struct {
	ProjectID     uuid.UUID `alias:"opensheet_sync_state.project_id"`
	Entity        string    `alias:"opensheet_sync_state.entity"`
	EntityID      uuid.UUID `alias:"opensheet_sync_state.entity_id"`
	Version       int64     `alias:"opensheet_sync_state.version"`
	SyncedVersion int64     `alias:"opensheet_sync_state.synced_version"`
	Deleted       bool      `alias:"opensheet_sync_state.deleted"`
	Attempts      int32     `alias:"opensheet_sync_state.attempts"`
	LastError     string    `alias:"opensheet_sync_state.last_error"`
	UpdatedAt     time.Time `alias:"opensheet_sync_state.updated_at"`
}

func (r *pgStateRow) toState() State {
	return State{
		Ref:       Ref{Entity: Entity(r.Entity), ID: r.EntityID, Deleted: r.Deleted},
		ProjectID: r.ProjectID, Version: r.Version, SyncedVersion: r.SyncedVersion,
		Attempts: int(r.Attempts), LastError: r.LastError, UpdatedAt: r.UpdatedAt.UTC(),
	}
}

func (s *postgresStore) stateProjection() postgres.ProjectionList {
	t := s.state
	return postgres.ProjectionList{t.ProjectID, t.Entity, t.EntityID, t.Version, t.SyncedVersion, t.Deleted, t.Attempts, t.LastError, t.UpdatedAt}
}

func (s *postgresStore) stateKey(orgID, projectID uuid.UUID, ref Ref) postgres.BoolExpression {
	t := s.state
	return t.OrgID.EQ(postgres.UUID(orgID)).
		AND(t.ProjectID.EQ(postgres.UUID(projectID))).
		AND(t.Entity.EQ(postgres.String(string(ref.Entity)))).
		AND(t.EntityID.EQ(postgres.UUID(ref.ID)))
}

func (s *postgresStore) markColumns() postgres.ColumnList {
	t := s.state
	return postgres.ColumnList{t.OrgID, t.ProjectID, t.Entity, t.EntityID, t.Version, t.SyncedVersion, t.Deleted, t.Attempts, t.LastError, t.UpdatedAt}
}

// NOTE: a mark gives the row fresh attempts and drops its retry_after, so a new edit is tried again at once.
func (s *postgresStore) markSets(at time.Time, deleted bool) []postgres.ColumnAssigment {
	t := s.state
	sets := []postgres.ColumnAssigment{
		t.Version.SET(t.Version.ADD(postgres.Int64(1))),
		t.Attempts.SET(postgres.Int32(0)),
		t.RetryAfter.SET(pgent.NullTimestampz()),
		t.UpdatedAt.SET(postgres.TimestampzT(at)),
	}
	if deleted {
		sets = append(sets, t.Deleted.SET(postgres.Bool(true)))
	}
	return sets
}

func (s *postgresStore) Mark(ctx context.Context, orgID, projectID uuid.UUID, refs []Ref, at time.Time) error {
	tx, owned, err := s.txAcquire(ctx)
	if err != nil {
		return err
	}
	t, at := s.state, at.UTC()
	for _, r := range LockOrder(refs) {
		stmt := t.INSERT(s.markColumns()).
			VALUES(orgID, projectID, string(r.Entity), r.ID, int64(1), int64(0), r.Deleted, int32(0), "", at).
			ON_CONFLICT(t.OrgID, t.Entity, t.EntityID).
			// SECURITY: the conflict clause carries the tenant predicate, as every upsert here must.
			DO_UPDATE(postgres.SET(s.markSets(at, r.Deleted)...).WHERE(t.OrgID.EQ(postgres.UUID(orgID))))
		if _, err := stmt.ExecContext(ctx, tx); err != nil {
			return s.endTx(tx, owned, fmt.Errorf("opensheetsync.postgres.Mark: %w", err))
		}
	}
	return s.endTx(tx, owned, nil)
}

func (s *postgresStore) markSelect(ctx context.Context, tx *sql.Tx, orgID uuid.UUID, at time.Time, sel postgres.SelectStatement) (int64, error) {
	t := s.state
	res, err := t.INSERT(s.markColumns()).
		QUERY(sel).
		ON_CONFLICT(t.OrgID, t.Entity, t.EntityID).
		// SECURITY: the conflict clause carries the tenant predicate, as every upsert here must.
		DO_UPDATE(postgres.SET(s.markSets(at, false)...).WHERE(t.OrgID.EQ(postgres.UUID(orgID)))).
		ExecContext(ctx, tx)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *postgresStore) markRow(e Entity, at time.Time, id, org, project postgres.Column) postgres.SelectStatement {
	return postgres.SELECT(org, project, postgres.String(string(e)), id,
		postgres.Int64(1), postgres.Int64(0), postgres.Bool(false), postgres.Int32(0), postgres.String(""), postgres.TimestampzT(at))
}

func (s *postgresStore) MarkReferencing(ctx context.Context, orgID, projectID uuid.UUID, ref Ref, at time.Time) (int64, error) {
	txs := s.transactions
	var cond postgres.BoolExpression
	switch ref.Entity {
	case EntityWallet:
		cond = txs.WalletID.EQ(postgres.UUID(ref.ID)).OR(txs.ToWalletID.EQ(postgres.UUID(ref.ID)))
	case EntityCategory:
		cond = txs.CategoryID.EQ(postgres.UUID(ref.ID))
	default:
		return 0, nil
	}
	tx, owned, err := s.txAcquire(ctx)
	if err != nil {
		return 0, err
	}
	at = at.UTC()
	sel := s.markRow(EntityTransaction, at, txs.ID, txs.OrgID, txs.ProjectID).FROM(txs).
		WHERE(txs.OrgID.EQ(postgres.UUID(orgID)).AND(txs.ProjectID.EQ(postgres.UUID(projectID))).AND(cond)).
		ORDER_BY(txs.ID.ASC())
	n, err := s.markSelect(ctx, tx, orgID, at, sel)
	if err != nil {
		return 0, s.endTx(tx, owned, fmt.Errorf("opensheetsync.postgres.MarkReferencing: %w", err))
	}
	return n, s.endTx(tx, owned, nil)
}

func (s *postgresStore) MarkAll(ctx context.Context, orgID, projectID uuid.UUID, at time.Time) (int64, error) {
	tx, owned, err := s.txAcquire(ctx)
	if err != nil {
		return 0, err
	}
	at = at.UTC()
	org, project := postgres.UUID(orgID), postgres.UUID(projectID)
	w, c, txs := s.wallets, s.categories, s.transactions
	sources := []struct {
		entity  Entity
		table   postgres.ReadableTable
		id, org postgres.ColumnString
		project postgres.ColumnString
	}{
		{EntityTransaction, txs, txs.ID, txs.OrgID, txs.ProjectID},
		{EntityCategory, c, c.ID, c.OrgID, c.ProjectID},
		{EntityWallet, w, w.ID, w.OrgID, w.ProjectID},
	}
	var total int64
	for _, src := range sources {
		sel := s.markRow(src.entity, at, src.id, src.org, src.project).FROM(src.table).
			WHERE(src.org.EQ(org).AND(src.project.EQ(project))).ORDER_BY(src.id.ASC())
		n, err := s.markSelect(ctx, tx, orgID, at, sel)
		if err != nil {
			return 0, s.endTx(tx, owned, fmt.Errorf("opensheetsync.postgres.MarkAll: %w", err))
		}
		gone, err := s.markGone(ctx, tx, orgID, projectID, src.entity, at, src.table, src.id, src.org)
		if err != nil {
			return 0, s.endTx(tx, owned, fmt.Errorf("opensheetsync.postgres.MarkAll: gone: %w", err))
		}
		total += n + gone
	}
	return total, s.endTx(tx, owned, nil)
}

// NOTE: every domain row is hard-deleted, so a state row whose entity no longer exists is a delete made while the link was off; marking it deleted lets the backfill tombstone it in the sheet.
func (s *postgresStore) markGone(ctx context.Context, tx *sql.Tx, orgID, projectID uuid.UUID, e Entity, at time.Time, table postgres.ReadableTable, id, org postgres.ColumnString) (int64, error) {
	t, orgArg := s.state, postgres.UUID(orgID)
	res, err := t.UPDATE().
		SET(
			t.Version.SET(t.Version.ADD(postgres.Int64(1))),
			t.Deleted.SET(postgres.Bool(true)),
			t.Attempts.SET(postgres.Int32(0)),
			t.RetryAfter.SET(pgent.NullTimestampz()),
			t.UpdatedAt.SET(postgres.TimestampzT(at)),
		).
		WHERE(t.OrgID.EQ(orgArg).
			AND(t.ProjectID.EQ(postgres.UUID(projectID))).
			AND(t.Entity.EQ(postgres.String(string(e)))).
			AND(postgres.NOT(postgres.EXISTS(postgres.SELECT(postgres.Int64(1)).FROM(table).WHERE(id.EQ(t.EntityID).AND(org.EQ(orgArg))))))).
		ExecContext(ctx, tx)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// NOTE: one predicate for Claim and ListDirty, so the reconciler never lists a row a job could not claim.
func (s *postgresStore) claimable(at time.Time) postgres.BoolExpression {
	t, now := s.state, postgres.TimestampzT(at.UTC())
	return t.SyncedVersion.LT(t.Version).
		AND(t.Attempts.LT(postgres.Int32(MaxRowAttempts))).
		AND(t.LeasedUntil.IS_NULL().OR(t.LeasedUntil.LT(now))).
		AND(t.RetryAfter.IS_NULL().OR(t.RetryAfter.LT(now)))
}

func (s *postgresStore) Claim(ctx context.Context, orgID, projectID uuid.UUID, ref Ref, token uuid.UUID, at time.Time, ttl time.Duration) (State, bool, error) {
	tx, owned, err := s.txAcquire(ctx)
	if err != nil {
		return State{}, false, err
	}
	t, at := s.state, at.UTC()
	var row pgStateRow
	err = t.UPDATE().
		SET(t.LeaseToken.SET(postgres.UUID(token)), t.LeasedUntil.SET(postgres.TimestampzT(at.Add(ttl)))).
		WHERE(s.stateKey(orgID, projectID, ref).AND(s.claimable(at))).
		RETURNING(s.stateProjection()).
		QueryContext(ctx, tx, &row)
	if errors.Is(err, qrm.ErrNoRows) {
		return State{}, false, s.endTx(tx, owned, nil)
	}
	if err != nil {
		return State{}, false, s.endTx(tx, owned, fmt.Errorf("opensheetsync.postgres.Claim: %w", err))
	}
	return row.toState(), true, s.endTx(tx, owned, nil)
}

func (s *postgresStore) Settle(ctx context.Context, orgID, projectID uuid.UUID, ref Ref, version int64, token uuid.UUID) (settled, dirty bool, err error) {
	tx, owned, err := s.txAcquire(ctx)
	if err != nil {
		return false, false, err
	}
	t := s.state
	var row struct {
		Version int64 `alias:"opensheet_sync_state.version"`
	}
	err = t.UPDATE().
		SET(
			t.SyncedVersion.SET(postgres.Int64(version)),
			t.Attempts.SET(postgres.Int32(0)),
			t.LastError.SET(postgres.String("")),
			t.LeaseToken.SET(pgent.NullUUID()),
			t.LeasedUntil.SET(pgent.NullTimestampz()),
			t.RetryAfter.SET(pgent.NullTimestampz()),
		).
		WHERE(s.stateKey(orgID, projectID, ref).AND(t.LeaseToken.EQ(postgres.UUID(token)))).
		RETURNING(t.Version).
		QueryContext(ctx, tx, &row)
	if errors.Is(err, qrm.ErrNoRows) {
		return false, false, s.endTx(tx, owned, nil)
	}
	if err != nil {
		return false, false, s.endTx(tx, owned, fmt.Errorf("opensheetsync.postgres.Settle: %w", err))
	}
	return true, row.Version > version, s.endTx(tx, owned, nil)
}

func (s *postgresStore) Release(ctx context.Context, orgID, projectID uuid.UUID, ref Ref, token uuid.UUID, f Failure) error {
	tx, owned, err := s.txAcquire(ctx)
	if err != nil {
		return err
	}
	t := s.state
	attempts := postgres.IntegerExpression(t.Attempts)
	if f.Count {
		attempts = t.Attempts.ADD(postgres.Int32(1))
	}
	retryAfter := pgent.NullTimestampz()
	if !f.RetryAfter.IsZero() {
		retryAfter = postgres.TimestampzT(f.RetryAfter.UTC())
	}
	// NOTE: a mark since the claim moved the version on; that new edit keeps its fresh attempts and no retry_after.
	if f.Version != 0 {
		same := t.Version.EQ(postgres.Int64(f.Version))
		attempts = postgres.IntExp(postgres.CASE().WHEN(same).THEN(attempts).ELSE(t.Attempts))
		retryAfter = postgres.TimestampzExp(postgres.CASE().WHEN(same).THEN(retryAfter).ELSE(t.RetryAfter))
	}
	_, err = t.UPDATE().
		SET(
			t.Attempts.SET(attempts),
			t.LastError.SET(postgres.String(f.Reason)),
			t.LeaseToken.SET(pgent.NullUUID()),
			t.LeasedUntil.SET(pgent.NullTimestampz()),
			t.RetryAfter.SET(retryAfter),
			t.UpdatedAt.SET(postgres.TimestampzT(f.At.UTC())),
		).
		WHERE(s.stateKey(orgID, projectID, ref).AND(t.LeaseToken.EQ(postgres.UUID(token)))).
		ExecContext(ctx, tx)
	if err != nil {
		return s.endTx(tx, owned, fmt.Errorf("opensheetsync.postgres.Release: %w", err))
	}
	return s.endTx(tx, owned, nil)
}

func (s *postgresStore) ListDirty(ctx context.Context, orgID, projectID uuid.UUID, cutoff, at time.Time, limit int) ([]Ref, error) {
	tx, owned, err := s.txAcquire(ctx)
	if err != nil {
		return nil, err
	}
	t := s.state
	var rows []pgStateRow
	err = t.SELECT(s.stateProjection()).
		FROM(t).
		WHERE(t.OrgID.EQ(postgres.UUID(orgID)).
			AND(t.ProjectID.EQ(postgres.UUID(projectID))).
			AND(t.UpdatedAt.LT(postgres.TimestampzT(cutoff.UTC()))).
			AND(s.claimable(at))).
		ORDER_BY(t.UpdatedAt.ASC(), t.Entity.ASC(), t.EntityID.ASC()).
		LIMIT(int64(limit)).
		QueryContext(ctx, tx, &rows)
	if err != nil {
		return nil, s.endTx(tx, owned, fmt.Errorf("opensheetsync.postgres.ListDirty: %w", err))
	}
	out := make([]Ref, 0, len(rows))
	for i := range rows {
		out = append(out, rows[i].toState().Ref)
	}
	return out, s.endTx(tx, owned, nil)
}

func (s *postgresStore) Backlog(ctx context.Context, orgID, projectID uuid.UUID) (Backlog, error) {
	tx, owned, err := s.txAcquire(ctx)
	if err != nil {
		return Backlog{}, err
	}
	t := s.state
	tried, left, spent := t.Attempts.GT(postgres.Int32(0)), t.Attempts.LT(postgres.Int32(MaxRowAttempts)), t.Attempts.GT_EQ(postgres.Int32(MaxRowAttempts))
	count := func(cond postgres.BoolExpression, alias string) postgres.Projection {
		return postgres.COUNT(postgres.CASE().WHEN(cond).THEN(postgres.Int64(1))).AS(alias)
	}
	var row struct {
		Pending int64 `alias:"backlog.pending"`
		Failing int64 `alias:"backlog.failing"`
		GivenUp int64 `alias:"backlog.given_up"`
	}
	err = postgres.SELECT(
		count(left, "backlog.pending"),
		count(tried.AND(left), "backlog.failing"),
		count(spent, "backlog.given_up"),
	).FROM(t).
		WHERE(t.OrgID.EQ(postgres.UUID(orgID)).AND(t.ProjectID.EQ(postgres.UUID(projectID))).AND(t.SyncedVersion.LT(t.Version))).
		QueryContext(ctx, tx, &row)
	if err != nil {
		return Backlog{}, s.endTx(tx, owned, fmt.Errorf("opensheetsync.postgres.Backlog: %w", err))
	}
	return Backlog{Pending: row.Pending, Failing: row.Failing, GivenUp: row.GivenUp}, s.endTx(tx, owned, nil)
}
