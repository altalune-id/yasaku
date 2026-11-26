package opensheetsync

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/go-jet/jet/v2/qrm"
	"github.com/go-jet/jet/v2/sqlite"
	"github.com/google/uuid"

	pdb "altalune.id/yasaku/internal/platform/db"
	sqliteent "altalune.id/yasaku/internal/platform/db/entity/sqlite"
)

// NOTE: SQLite has no RLS, so the org predicate on every statement here is the only tenant guard.
type sqliteGrantStore struct {
	db     *sql.DB
	grants *sqliteent.OpensheetGrants
}

type sqliteGrantRow struct {
	OrgID           string  `alias:"opensheet_grants.org_id"`
	ProjectID       string  `alias:"opensheet_grants.project_id"`
	ConnectedBy     string  `alias:"opensheet_grants.connected_by"`
	RefreshSealed   []byte  `alias:"opensheet_grants.refresh_sealed"`
	AccessSealed    []byte  `alias:"opensheet_grants.access_sealed"`
	AccessExpiresAt *string `alias:"opensheet_grants.access_expires_at"`
	Scopes          string  `alias:"opensheet_grants.scopes"`
	Status          string  `alias:"opensheet_grants.status"`
	CreatedAt       string  `alias:"opensheet_grants.created_at"`
	UpdatedAt       string  `alias:"opensheet_grants.updated_at"`
}

func (r *sqliteGrantRow) toGrant() (*Grant, error) {
	g := &Grant{RefreshSealed: r.RefreshSealed, AccessSealed: r.AccessSealed, Scopes: r.Scopes, Status: GrantStatus(r.Status)}
	var err error
	for _, f := range []struct {
		dst *uuid.UUID
		src string
	}{{&g.OrgID, r.OrgID}, {&g.ProjectID, r.ProjectID}, {&g.ConnectedBy, r.ConnectedBy}} {
		if *f.dst, err = sqliteUUID(&f.src); err != nil {
			return nil, err
		}
	}
	if g.AccessExpiresAt, err = sqliteTimePtr(r.AccessExpiresAt); err != nil {
		return nil, err
	}
	if g.CreatedAt, err = sqliteTime(r.CreatedAt); err != nil {
		return nil, err
	}
	if g.UpdatedAt, err = sqliteTime(r.UpdatedAt); err != nil {
		return nil, err
	}
	return g, nil
}

func (s *sqliteGrantStore) txAcquire(ctx context.Context) (*sql.Tx, bool, error) {
	if tx, ok := pdb.CurrentTx(ctx); ok {
		return tx, false, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, fmt.Errorf("opensheetsync.sqlite: begin: %w", err)
	}
	return tx, true, nil
}

func (s *sqliteGrantStore) endTx(tx *sql.Tx, owned bool, err error) error {
	if !owned {
		return err
	}
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	if cerr := tx.Commit(); cerr != nil {
		return fmt.Errorf("opensheetsync.sqlite: commit: %w", cerr)
	}
	return nil
}

func (s *sqliteGrantStore) where(orgID, projectID uuid.UUID) sqlite.BoolExpression {
	return s.grants.OrgID.EQ(sqlite.String(orgID.String())).AND(s.grants.ProjectID.EQ(sqlite.String(projectID.String())))
}

func (s *sqliteGrantStore) SaveGrant(ctx context.Context, g *Grant) error {
	tx, owned, err := s.txAcquire(ctx)
	if err != nil {
		return err
	}
	t := s.grants
	stmt := t.INSERT(t.AllColumns).
		VALUES(g.OrgID.String(), g.ProjectID.String(), g.ConnectedBy.String(), g.RefreshSealed, g.AccessSealed,
			sqliteTimeArg(g.AccessExpiresAt), g.Scopes, string(g.Status),
			sqliteent.SQLiteTime(g.CreatedAt), sqliteent.SQLiteTime(g.UpdatedAt)).
		ON_CONFLICT(t.ProjectID).
		// SECURITY: SQLite has no RLS, so this tenant predicate is the only thing stopping a save from taking over another org's grant.
		DO_UPDATE(sqlite.SET(
			t.ConnectedBy.SET(sqlite.String(g.ConnectedBy.String())),
			t.RefreshSealed.SET(sqlite.Blob(g.RefreshSealed)),
			t.AccessSealed.SET(sqlite.Blob(g.AccessSealed)),
			t.AccessExpiresAt.SET(sqliteTimeExpr(g.AccessExpiresAt)),
			t.Scopes.SET(sqlite.String(g.Scopes)),
			t.Status.SET(sqlite.String(string(g.Status))),
			t.UpdatedAt.SET(sqlite.String(sqliteent.SQLiteTime(g.UpdatedAt))),
		).WHERE(t.OrgID.EQ(sqlite.String(g.OrgID.String()))))
	res, err := stmt.ExecContext(ctx, tx)
	if err != nil {
		return s.endTx(tx, owned, fmt.Errorf("opensheetsync.sqlite.SaveGrant: %w", err))
	}
	n, err := res.RowsAffected()
	if err != nil {
		return s.endTx(tx, owned, fmt.Errorf("opensheetsync.sqlite.SaveGrant: rows affected: %w", err))
	}
	if n == 0 {
		return s.endTx(tx, owned, &GrantNotFoundError{ProjectID: g.ProjectID.String()})
	}
	return s.endTx(tx, owned, nil)
}

func (s *sqliteGrantStore) UpdateGrantTokens(ctx context.Context, g *Grant) error {
	tx, owned, err := s.txAcquire(ctx)
	if err != nil {
		return err
	}
	t := s.grants
	res, err := t.UPDATE(t.RefreshSealed, t.AccessSealed, t.AccessExpiresAt, t.Scopes, t.Status, t.UpdatedAt).
		SET(g.RefreshSealed, g.AccessSealed, sqliteTimeArg(g.AccessExpiresAt), g.Scopes, string(g.Status), sqliteent.SQLiteTime(g.UpdatedAt)).
		WHERE(s.where(g.OrgID, g.ProjectID).AND(t.CreatedAt.EQ(sqlite.String(sqliteent.SQLiteTime(g.CreatedAt))))).
		ExecContext(ctx, tx)
	if err != nil {
		return s.endTx(tx, owned, fmt.Errorf("opensheetsync.sqlite.UpdateGrantTokens: %w", err))
	}
	n, err := res.RowsAffected()
	if err != nil {
		return s.endTx(tx, owned, fmt.Errorf("opensheetsync.sqlite.UpdateGrantTokens: rows affected: %w", err))
	}
	if n == 0 {
		return s.endTx(tx, owned, &GrantNotFoundError{ProjectID: g.ProjectID.String()})
	}
	return s.endTx(tx, owned, nil)
}

func (s *sqliteGrantStore) GrantByProject(ctx context.Context, orgID, projectID uuid.UUID) (*Grant, error) {
	var q qrm.Queryable = s.db
	if tx, ok := pdb.CurrentTx(ctx); ok {
		q = tx
	}
	var row sqliteGrantRow
	err := s.grants.SELECT(s.grants.AllColumns).FROM(s.grants).WHERE(s.where(orgID, projectID)).QueryContext(ctx, q, &row)
	if errors.Is(err, qrm.ErrNoRows) {
		return nil, &GrantNotFoundError{ProjectID: projectID.String()}
	}
	if err != nil {
		return nil, fmt.Errorf("opensheetsync.sqlite.GrantByProject: %w", err)
	}
	return row.toGrant()
}

func (s *sqliteGrantStore) GrantsConnectedBy(ctx context.Context, orgID, userID uuid.UUID) ([]*Grant, error) {
	var q qrm.Queryable = s.db
	if tx, ok := pdb.CurrentTx(ctx); ok {
		q = tx
	}
	t := s.grants
	var rows []sqliteGrantRow
	err := t.SELECT(t.AllColumns).FROM(t).
		WHERE(t.OrgID.EQ(sqlite.String(orgID.String())).AND(t.ConnectedBy.EQ(sqlite.String(userID.String())))).
		ORDER_BY(t.ProjectID).QueryContext(ctx, q, &rows)
	if err != nil && !errors.Is(err, qrm.ErrNoRows) {
		return nil, fmt.Errorf("opensheetsync.sqlite.GrantsConnectedBy: %w", err)
	}
	out := make([]*Grant, 0, len(rows))
	for i := range rows {
		g, err := rows[i].toGrant()
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, nil
}

func (s *sqliteGrantStore) DeleteGrant(ctx context.Context, orgID, projectID uuid.UUID) error {
	tx, owned, err := s.txAcquire(ctx)
	if err != nil {
		return err
	}
	res, err := s.grants.DELETE().WHERE(s.where(orgID, projectID)).ExecContext(ctx, tx)
	if err != nil {
		return s.endTx(tx, owned, fmt.Errorf("opensheetsync.sqlite.DeleteGrant: %w", err))
	}
	n, err := res.RowsAffected()
	if err != nil {
		return s.endTx(tx, owned, fmt.Errorf("opensheetsync.sqlite.DeleteGrant: rows affected: %w", err))
	}
	if n == 0 {
		return s.endTx(tx, owned, &GrantNotFoundError{ProjectID: projectID.String()})
	}
	return s.endTx(tx, owned, nil)
}

// NOTE: no write transaction is held across fn's refresh call; the caller's per-project mutex serialises refreshers in the one SQLite process.
func (s *sqliteGrantStore) LockGrant(ctx context.Context, orgID, projectID uuid.UUID, fn func(ctx context.Context, g *Grant) error) error {
	g, err := s.GrantByProject(ctx, orgID, projectID)
	if err != nil {
		return err
	}
	return fn(ctx, g)
}
