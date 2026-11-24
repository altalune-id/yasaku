package user

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/go-jet/jet/v2/qrm"
	"github.com/go-jet/jet/v2/sqlite"
	"github.com/google/uuid"

	pdb "altalune.id/yasaku/internal/platform/db"
	sqliteent "altalune.id/yasaku/internal/platform/db/entity/sqlite"
)

func (s *sqliteStore) StampSignIn(ctx context.Context, id uuid.UUID, at time.Time) error {
	res, err := s.table.UPDATE(s.table.LastSignInAt).
		SET(sqlite.String(sqliteent.SQLiteTime(at))).
		WHERE(s.table.ID.EQ(sqlite.String(id.String()))).
		ExecContext(ctx, s.db)
	if err != nil {
		return fmt.Errorf("user.sqlite.StampSignIn: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return &NotFoundError{ID: id.String()}
	}
	return nil
}

func (s *sqliteStore) AdminListOrgs(ctx context.Context) ([]AdminOrg, error) {
	p := s.prefix
	q := "SELECT o.id, o.slug, o.name, o.plan, o.created_at, " + //nolint:gosec // G202: q joins the configured table prefix with fixed SQL, never input
		"(SELECT count(*) FROM " + p + "memberships m WHERE m.org_id = o.id), " +
		"(SELECT count(*) FROM " + p + "projects pr WHERE pr.org_id = o.id), " +
		"(SELECT max(u.last_sign_in_at) FROM " + p + "memberships m JOIN " + p + "users u ON u.id = m.user_id WHERE m.org_id = o.id) " +
		"FROM " + p + "orgs o ORDER BY o.created_at DESC, o.id ASC"
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("user.sqlite.AdminListOrgs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []AdminOrg
	for rows.Next() {
		var o AdminOrg
		var id, created string
		var last sql.NullString
		if err := rows.Scan(&id, &o.Slug, &o.Name, &o.Plan, &created, &o.MemberCount, &o.ProjectCount, &last); err != nil {
			return nil, fmt.Errorf("user.sqlite.AdminListOrgs: scan: %w", err)
		}
		if o.ID, err = uuid.Parse(id); err != nil {
			return nil, fmt.Errorf("user.sqlite.AdminListOrgs: parse id: %w", err)
		}
		if o.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
			return nil, fmt.Errorf("user.sqlite.AdminListOrgs: parse created_at: %w", err)
		}
		if o.LastActive, err = parseNullTime(last); err != nil {
			return nil, fmt.Errorf("user.sqlite.AdminListOrgs: parse last_active: %w", err)
		}
		out = append(out, o)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("user.sqlite.AdminListOrgs: %w", err)
	}
	return out, nil
}

func (s *sqliteStore) AdminListUsers(ctx context.Context) ([]AdminUser, error) {
	p := s.prefix
	q := "SELECT u.id, u.email, u.name, u.created_at, u.last_sign_in_at, u.suspended_at, " + //nolint:gosec // G202: q joins the configured table prefix with fixed SQL, never input
		"(SELECT count(*) FROM " + p + "memberships m WHERE m.user_id = u.id) " +
		"FROM " + p + "users u ORDER BY u.created_at DESC, u.id ASC"
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("user.sqlite.AdminListUsers: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []AdminUser
	for rows.Next() {
		var u AdminUser
		var id, created string
		var signedIn, suspended sql.NullString
		if err := rows.Scan(&id, &u.Email, &u.Name, &created, &signedIn, &suspended, &u.OrgCount); err != nil {
			return nil, fmt.Errorf("user.sqlite.AdminListUsers: scan: %w", err)
		}
		if u.ID, err = uuid.Parse(id); err != nil {
			return nil, fmt.Errorf("user.sqlite.AdminListUsers: parse id: %w", err)
		}
		if u.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
			return nil, fmt.Errorf("user.sqlite.AdminListUsers: parse created_at: %w", err)
		}
		if u.LastSignInAt, err = parseNullTime(signedIn); err != nil {
			return nil, fmt.Errorf("user.sqlite.AdminListUsers: parse last_sign_in_at: %w", err)
		}
		if u.SuspendedAt, err = parseNullTime(suspended); err != nil {
			return nil, fmt.Errorf("user.sqlite.AdminListUsers: parse suspended_at: %w", err)
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("user.sqlite.AdminListUsers: %w", err)
	}
	return out, nil
}

func (s *sqliteStore) execer(ctx context.Context) qrm.Executable {
	if tx, ok := pdb.CurrentTx(ctx); ok {
		return tx
	}
	return s.db
}

func parseNullTime(v sql.NullString) (*time.Time, error) {
	if !v.Valid || v.String == "" {
		return nil, nil //nolint:nilnil // an absent timestamp is not an error
	}
	t, err := time.Parse(time.RFC3339Nano, v.String)
	if err != nil {
		return nil, err
	}
	t = t.UTC()
	return &t, nil
}

func (s *sqliteStore) SetSuspended(ctx context.Context, id uuid.UUID, at *time.Time) error {
	val := sqliteent.NullText()
	if at != nil {
		val = sqlite.String(sqliteent.SQLiteTime(*at))
	}
	res, err := s.table.UPDATE(s.table.SuspendedAt, s.table.UpdatedAt).
		SET(val, sqlite.String(sqliteent.SQLiteTime(time.Now()))).
		WHERE(s.table.ID.EQ(sqlite.String(id.String()))).
		ExecContext(ctx, s.execer(ctx))
	if err != nil {
		return fmt.Errorf("user.sqlite.SetSuspended: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return &NotFoundError{ID: id.String()}
	}
	return nil
}
