package user

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/go-jet/jet/v2/postgres"
	"github.com/google/uuid"
)

func (s *postgresStore) StampSignIn(ctx context.Context, id uuid.UUID, at time.Time) error {
	res, err := s.table.UPDATE(s.table.LastSignInAt).
		SET(postgres.TimestampzT(at.UTC())).
		WHERE(s.table.ID.EQ(postgres.UUID(id))).
		ExecContext(ctx, s.writer(ctx))
	if err != nil {
		return fmt.Errorf("user.postgres.StampSignIn: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return &NotFoundError{ID: id.String()}
	}
	return nil
}

// SECURITY: reads through a SECURITY DEFINER wrapper that returns counts only; the caller gates it to staff.
func (s *postgresStore) AdminListOrgs(ctx context.Context) ([]AdminOrg, error) {
	rows, err := s.reader(ctx).QueryContext(ctx, s.adminOrgsStmt)
	if err != nil {
		return nil, fmt.Errorf("user.postgres.AdminListOrgs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []AdminOrg
	for rows.Next() {
		var o AdminOrg
		var last sql.NullTime
		if err := rows.Scan(&o.ID, &o.Slug, &o.Name, &o.Plan, &o.CreatedAt, &o.MemberCount, &o.ProjectCount, &last); err != nil {
			return nil, fmt.Errorf("user.postgres.AdminListOrgs: scan: %w", err)
		}
		o.CreatedAt = o.CreatedAt.UTC()
		o.LastActive = utcPtr(last)
		out = append(out, o)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("user.postgres.AdminListOrgs: %w", err)
	}
	return out, nil
}

// SECURITY: reads through a SECURITY DEFINER wrapper; the caller gates it to staff.
func (s *postgresStore) AdminListUsers(ctx context.Context) ([]AdminUser, error) {
	rows, err := s.reader(ctx).QueryContext(ctx, s.adminUsersStmt)
	if err != nil {
		return nil, fmt.Errorf("user.postgres.AdminListUsers: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []AdminUser
	for rows.Next() {
		var u AdminUser
		var signedIn, suspended sql.NullTime
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.CreatedAt, &signedIn, &suspended, &u.OrgCount); err != nil {
			return nil, fmt.Errorf("user.postgres.AdminListUsers: scan: %w", err)
		}
		u.CreatedAt = u.CreatedAt.UTC()
		u.LastSignInAt, u.SuspendedAt = utcPtr(signedIn), utcPtr(suspended)
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("user.postgres.AdminListUsers: %w", err)
	}
	return out, nil
}

func utcPtr(t sql.NullTime) *time.Time {
	if !t.Valid {
		return nil
	}
	u := t.Time.UTC()
	return &u
}

func (s *postgresStore) SetSuspended(ctx context.Context, id uuid.UUID, at *time.Time) error {
	res, err := s.table.UPDATE(s.table.SuspendedAt, s.table.UpdatedAt).
		SET(nullableTimeExpr(at), postgres.TimestampzT(time.Now().UTC())).
		WHERE(s.table.ID.EQ(postgres.UUID(id))).
		ExecContext(ctx, s.writer(ctx))
	if err != nil {
		return fmt.Errorf("user.postgres.SetSuspended: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return &NotFoundError{ID: id.String()}
	}
	return nil
}
