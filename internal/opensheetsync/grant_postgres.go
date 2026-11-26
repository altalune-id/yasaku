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

	pdb "altalune.id/yasaku/internal/platform/db"
	pgent "altalune.id/yasaku/internal/platform/db/entity/postgres"
	"altalune.id/yasaku/internal/platform/tenant"
)

const grantLockTimeout = "SET LOCAL lock_timeout = '10s'"

type pgGrantStore struct {
	pc     *tenant.PgConn
	grants *pgent.OpensheetGrants
}

type pgGrantRow struct {
	OrgID           uuid.UUID  `alias:"opensheet_grants.org_id"`
	ProjectID       uuid.UUID  `alias:"opensheet_grants.project_id"`
	ConnectedBy     uuid.UUID  `alias:"opensheet_grants.connected_by"`
	RefreshSealed   []byte     `alias:"opensheet_grants.refresh_sealed"`
	AccessSealed    []byte     `alias:"opensheet_grants.access_sealed"`
	AccessExpiresAt *time.Time `alias:"opensheet_grants.access_expires_at"`
	Scopes          string     `alias:"opensheet_grants.scopes"`
	Status          string     `alias:"opensheet_grants.status"`
	CreatedAt       time.Time  `alias:"opensheet_grants.created_at"`
	UpdatedAt       time.Time  `alias:"opensheet_grants.updated_at"`
}

func (r *pgGrantRow) toGrant() *Grant {
	return &Grant{
		OrgID: r.OrgID, ProjectID: r.ProjectID, ConnectedBy: r.ConnectedBy, RefreshSealed: r.RefreshSealed,
		AccessSealed: r.AccessSealed, AccessExpiresAt: utcPtr(r.AccessExpiresAt), Scopes: r.Scopes,
		Status: GrantStatus(r.Status), CreatedAt: r.CreatedAt.UTC(), UpdatedAt: r.UpdatedAt.UTC(),
	}
}

func (s *pgGrantStore) txAcquire(ctx context.Context) (*sql.Tx, bool, error) {
	if tx, ok := pdb.CurrentTx(ctx); ok {
		return tx, false, nil
	}
	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, false, err
	}
	tx, err := s.pc.BeginTenanted(ctx, tc)
	if err != nil {
		return nil, false, fmt.Errorf("opensheetsync.postgres: begin: %w", err)
	}
	return tx, true, nil
}

func (s *pgGrantStore) endTx(tx *sql.Tx, owned bool, err error) error {
	if !owned {
		return err
	}
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	if cerr := tx.Commit(); cerr != nil {
		return fmt.Errorf("opensheetsync.postgres: commit: %w", cerr)
	}
	return nil
}

func (s *pgGrantStore) where(orgID, projectID uuid.UUID) postgres.BoolExpression {
	return s.grants.OrgID.EQ(postgres.UUID(orgID)).AND(s.grants.ProjectID.EQ(postgres.UUID(projectID)))
}

func (s *pgGrantStore) SaveGrant(ctx context.Context, g *Grant) error {
	tx, owned, err := s.txAcquire(ctx)
	if err != nil {
		return err
	}
	t := s.grants
	stmt := t.INSERT(t.AllColumns).
		VALUES(g.OrgID, g.ProjectID, g.ConnectedBy, g.RefreshSealed, g.AccessSealed, pgTimeArg(g.AccessExpiresAt),
			g.Scopes, string(g.Status), g.CreatedAt.UTC(), g.UpdatedAt.UTC()).
		ON_CONFLICT(t.ProjectID).
		// SECURITY: the conflict clause carries the tenant predicate; without it a save naming another org's project takes over that org's grant.
		DO_UPDATE(postgres.SET(
			t.ConnectedBy.SET(postgres.UUID(g.ConnectedBy)),
			t.RefreshSealed.SET(postgres.Bytea(g.RefreshSealed)),
			t.AccessSealed.SET(postgres.Bytea(g.AccessSealed)),
			t.AccessExpiresAt.SET(pgTimeExpr(g.AccessExpiresAt)),
			t.Scopes.SET(postgres.String(g.Scopes)),
			t.Status.SET(postgres.String(string(g.Status))),
			t.UpdatedAt.SET(postgres.TimestampzT(g.UpdatedAt.UTC())),
		).WHERE(t.OrgID.EQ(postgres.UUID(g.OrgID))))
	res, err := stmt.ExecContext(ctx, tx)
	if err != nil {
		return s.endTx(tx, owned, fmt.Errorf("opensheetsync.postgres.SaveGrant: %w", err))
	}
	n, err := res.RowsAffected()
	if err != nil {
		return s.endTx(tx, owned, fmt.Errorf("opensheetsync.postgres.SaveGrant: rows affected: %w", err))
	}
	if n == 0 {
		return s.endTx(tx, owned, &GrantNotFoundError{ProjectID: g.ProjectID.String()})
	}
	return s.endTx(tx, owned, nil)
}

func (s *pgGrantStore) UpdateGrantTokens(ctx context.Context, g *Grant) error {
	tx, owned, err := s.txAcquire(ctx)
	if err != nil {
		return err
	}
	t := s.grants
	res, err := t.UPDATE(t.RefreshSealed, t.AccessSealed, t.AccessExpiresAt, t.Scopes, t.Status, t.UpdatedAt).
		SET(g.RefreshSealed, g.AccessSealed, pgTimeArg(g.AccessExpiresAt), g.Scopes, string(g.Status), g.UpdatedAt.UTC()).
		WHERE(s.where(g.OrgID, g.ProjectID).AND(t.CreatedAt.EQ(postgres.TimestampzT(g.CreatedAt.UTC())))).
		ExecContext(ctx, tx)
	if err != nil {
		return s.endTx(tx, owned, fmt.Errorf("opensheetsync.postgres.UpdateGrantTokens: %w", err))
	}
	n, err := res.RowsAffected()
	if err != nil {
		return s.endTx(tx, owned, fmt.Errorf("opensheetsync.postgres.UpdateGrantTokens: rows affected: %w", err))
	}
	if n == 0 {
		return s.endTx(tx, owned, &GrantNotFoundError{ProjectID: g.ProjectID.String()})
	}
	return s.endTx(tx, owned, nil)
}

func (s *pgGrantStore) GrantByProject(ctx context.Context, orgID, projectID uuid.UUID) (*Grant, error) {
	tx, owned, err := s.txAcquire(ctx)
	if err != nil {
		return nil, err
	}
	g, err := s.selectOne(ctx, tx, orgID, projectID, false)
	return g, s.endTx(tx, owned, err)
}

func (s *pgGrantStore) selectOne(ctx context.Context, tx *sql.Tx, orgID, projectID uuid.UUID, lock bool) (*Grant, error) {
	stmt := s.grants.SELECT(s.grants.AllColumns).FROM(s.grants).WHERE(s.where(orgID, projectID))
	if lock {
		stmt = stmt.FOR(postgres.UPDATE())
	}
	var row pgGrantRow
	err := stmt.QueryContext(ctx, tx, &row)
	if errors.Is(err, qrm.ErrNoRows) {
		return nil, &GrantNotFoundError{ProjectID: projectID.String()}
	}
	if err != nil {
		return nil, fmt.Errorf("opensheetsync.postgres.grant: %w", err)
	}
	return row.toGrant(), nil
}

func (s *pgGrantStore) GrantsConnectedBy(ctx context.Context, orgID, userID uuid.UUID) ([]*Grant, error) {
	tx, owned, err := s.txAcquire(ctx)
	if err != nil {
		return nil, err
	}
	t := s.grants
	var rows []pgGrantRow
	err = t.SELECT(t.AllColumns).FROM(t).
		WHERE(t.OrgID.EQ(postgres.UUID(orgID)).AND(t.ConnectedBy.EQ(postgres.UUID(userID)))).
		ORDER_BY(t.ProjectID).QueryContext(ctx, tx, &rows)
	if err != nil && !errors.Is(err, qrm.ErrNoRows) {
		return nil, s.endTx(tx, owned, fmt.Errorf("opensheetsync.postgres.GrantsConnectedBy: %w", err))
	}
	out := make([]*Grant, len(rows))
	for i := range rows {
		out[i] = rows[i].toGrant()
	}
	return out, s.endTx(tx, owned, nil)
}

func (s *pgGrantStore) DeleteGrant(ctx context.Context, orgID, projectID uuid.UUID) error {
	tx, owned, err := s.txAcquire(ctx)
	if err != nil {
		return err
	}
	res, err := s.grants.DELETE().WHERE(s.where(orgID, projectID)).ExecContext(ctx, tx)
	if err != nil {
		return s.endTx(tx, owned, fmt.Errorf("opensheetsync.postgres.DeleteGrant: %w", err))
	}
	n, err := res.RowsAffected()
	if err != nil {
		return s.endTx(tx, owned, fmt.Errorf("opensheetsync.postgres.DeleteGrant: rows affected: %w", err))
	}
	if n == 0 {
		return s.endTx(tx, owned, &GrantNotFoundError{ProjectID: projectID.String()})
	}
	return s.endTx(tx, owned, nil)
}

// NOTE: the row lock is held across fn's refresh call, which has its own shorter timeout, so a second process waits for the rotated token instead of spending the old one.
func (s *pgGrantStore) LockGrant(ctx context.Context, orgID, projectID uuid.UUID, fn func(ctx context.Context, g *Grant) error) error {
	if _, ok := pdb.CurrentTx(ctx); ok {
		return pdb.ErrNestedUnitOfWork
	}
	tx, _, err := s.txAcquire(ctx)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, grantLockTimeout); err != nil {
		return s.endTx(tx, true, fmt.Errorf("opensheetsync.postgres.LockGrant: lock timeout: %w", err))
	}
	g, err := s.selectOne(ctx, tx, orgID, projectID, true)
	if err != nil {
		return s.endTx(tx, true, err)
	}
	ferr := fn(pdb.ContextWithTx(ctx, tx), g)
	if err := s.endTx(tx, true, nil); err != nil {
		return errors.Join(ferr, err)
	}
	return ferr
}
