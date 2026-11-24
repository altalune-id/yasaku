package limits

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/go-jet/jet/v2/qrm"
	"github.com/go-jet/jet/v2/sqlite"
	"github.com/google/uuid"

	sqliteent "altalune.id/yasaku/internal/platform/db/entity/sqlite"
)

type sqliteStore struct {
	db        *sql.DB
	prefix    string
	plans     *sqliteent.PlanLimits
	overrides *sqliteent.LimitOverrides
}

func newSQLiteStore(db *sql.DB, prefix string) *sqliteStore {
	return &sqliteStore{db: db, prefix: prefix, plans: sqliteent.NewPlanLimits(prefix), overrides: sqliteent.NewLimitOverrides(prefix)}
}

func (s *sqliteStore) conn(ctx context.Context) dbtx { return conn(ctx, s.db) }

func (s *sqliteStore) planExpr(kind SubjectKind) string {
	switch kind {
	case SubjectOrg:
		return "(SELECT o.plan FROM " + s.prefix + "orgs o WHERE o.id = ?2)"
	case SubjectProject:
		return "(SELECT o.plan FROM " + s.prefix + "projects p JOIN " + s.prefix + "orgs o ON o.id = p.org_id WHERE p.id = ?2)"
	default:
		return "NULL"
	}
}

func (s *sqliteStore) Resolve(ctx context.Context, key Key, sub Subject) (value int64, ok bool, err error) {
	q := "SELECT value FROM (" +
		"SELECT value, 0 AS pri FROM " + s.prefix + "limit_overrides WHERE subject_kind = ?1 AND subject_id = ?2 AND key = ?3 " +
		"UNION ALL SELECT value, 1 FROM " + s.prefix + "plan_limits WHERE key = ?3 AND plan = COALESCE(" + s.planExpr(sub.Kind) + ", ?4)" +
		") ORDER BY pri LIMIT 1"
	var v sql.NullInt64
	err = s.conn(ctx).QueryRowContext(ctx, q, string(sub.Kind), sub.ID.String(), string(key), DefaultPlan).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("limits.sqlite: resolve: %w", err)
	}
	return nullToLimit(v), true, nil
}

// NOTE: a SQLite unit of work opens with BEGIN IMMEDIATE and already holds the database write lock.
func (s *sqliteStore) Lock(context.Context, []Subject) error { return nil }

func sqliteValue(v int64) sqlite.IntegerExpression {
	if v == Unlimited {
		return sqliteent.NullInteger()
	}
	return sqlite.Int64(v)
}

func sqliteBy(by uuid.UUID) sqlite.StringExpression {
	if by == uuid.Nil {
		return sqliteent.NullText()
	}
	return sqlite.String(by.String())
}

func (s *sqliteStore) SetPlan(ctx context.Context, plan string, key Key, value int64, by uuid.UUID) error {
	now := sqliteent.SQLiteTime(time.Now())
	stmt := s.plans.INSERT(s.plans.AllColumns).
		VALUES(sqlite.String(plan), sqlite.String(string(key)), sqliteValue(value), sqlite.String(now), sqliteBy(by)).
		ON_CONFLICT(s.plans.Plan, s.plans.Key).
		DO_UPDATE(sqlite.SET(
			s.plans.Value.SET(sqliteValue(value)),
			s.plans.UpdatedAt.SET(sqlite.String(now)),
			s.plans.UpdatedBy.SET(sqliteBy(by)),
		))
	if _, err := stmt.ExecContext(ctx, s.conn(ctx)); err != nil {
		return fmt.Errorf("limits.sqlite: set plan: %w", err)
	}
	return nil
}

func (s *sqliteStore) ClearPlan(ctx context.Context, plan string, key Key) error {
	stmt := s.plans.DELETE().WHERE(s.plans.Plan.EQ(sqlite.String(plan)).AND(s.plans.Key.EQ(sqlite.String(string(key)))))
	if _, err := stmt.ExecContext(ctx, s.conn(ctx)); err != nil {
		return fmt.Errorf("limits.sqlite: clear plan: %w", err)
	}
	return nil
}

func (s *sqliteStore) SetOverride(ctx context.Context, sub Subject, key Key, value int64, by uuid.UUID) error {
	now := sqliteent.SQLiteTime(time.Now())
	stmt := s.overrides.INSERT(s.overrides.AllColumns).
		VALUES(sqlite.String(string(sub.Kind)), sqlite.String(sub.ID.String()), sqlite.String(string(key)), sqliteValue(value), sqlite.String(now), sqliteBy(by)).
		ON_CONFLICT(s.overrides.SubjectKind, s.overrides.SubjectID, s.overrides.Key).
		DO_UPDATE(sqlite.SET(
			s.overrides.Value.SET(sqliteValue(value)),
			s.overrides.UpdatedAt.SET(sqlite.String(now)),
			s.overrides.UpdatedBy.SET(sqliteBy(by)),
		))
	if _, err := stmt.ExecContext(ctx, s.conn(ctx)); err != nil {
		return fmt.Errorf("limits.sqlite: set override: %w", err)
	}
	return nil
}

func (s *sqliteStore) DeleteOverride(ctx context.Context, sub Subject, key Key) error {
	stmt := s.overrides.DELETE().WHERE(
		s.overrides.SubjectKind.EQ(sqlite.String(string(sub.Kind))).
			AND(s.overrides.SubjectID.EQ(sqlite.String(sub.ID.String()))).
			AND(s.overrides.Key.EQ(sqlite.String(string(key)))))
	if _, err := stmt.ExecContext(ctx, s.conn(ctx)); err != nil {
		return fmt.Errorf("limits.sqlite: delete override: %w", err)
	}
	return nil
}

type sqlitePlanRow struct {
	Plan  string        `alias:"plan_limits.plan"`
	Key   string        `alias:"plan_limits.key"`
	Value sql.NullInt64 `alias:"plan_limits.value"`
}

func (s *sqliteStore) ListPlan(ctx context.Context, plan string) ([]PlanEntry, error) {
	stmt := sqlite.SELECT(s.plans.Plan, s.plans.Key, s.plans.Value).FROM(s.plans).
		WHERE(s.plans.Plan.EQ(sqlite.String(plan))).ORDER_BY(s.plans.Key.ASC())
	var rows []sqlitePlanRow
	if err := stmt.QueryContext(ctx, s.conn(ctx), &rows); err != nil && !errors.Is(err, qrm.ErrNoRows) {
		return nil, fmt.Errorf("limits.sqlite: list plan: %w", err)
	}
	out := make([]PlanEntry, 0, len(rows))
	for _, r := range rows {
		out = append(out, PlanEntry{Plan: r.Plan, Key: Key(r.Key), Value: nullToLimit(r.Value)})
	}
	return out, nil
}

type sqliteOverrideRow struct {
	SubjectKind string        `alias:"limit_overrides.subject_kind"`
	SubjectID   string        `alias:"limit_overrides.subject_id"`
	Key         string        `alias:"limit_overrides.key"`
	Value       sql.NullInt64 `alias:"limit_overrides.value"`
}

func (s *sqliteStore) ListOverrides(ctx context.Context) ([]Override, error) {
	stmt := sqlite.SELECT(s.overrides.SubjectKind, s.overrides.SubjectID, s.overrides.Key, s.overrides.Value).FROM(s.overrides).
		ORDER_BY(s.overrides.SubjectKind.ASC(), s.overrides.SubjectID.ASC(), s.overrides.Key.ASC())
	var rows []sqliteOverrideRow
	if err := stmt.QueryContext(ctx, s.conn(ctx), &rows); err != nil && !errors.Is(err, qrm.ErrNoRows) {
		return nil, fmt.Errorf("limits.sqlite: list overrides: %w", err)
	}
	out := make([]Override, 0, len(rows))
	for _, r := range rows {
		id, err := uuid.Parse(r.SubjectID)
		if err != nil {
			return nil, fmt.Errorf("limits.sqlite: list overrides: subject id: %w", err)
		}
		out = append(out, Override{Subject: Subject{Kind: SubjectKind(r.SubjectKind), ID: id}, Key: Key(r.Key), Value: nullToLimit(r.Value)})
	}
	return out, nil
}

func (s *sqliteStore) OrgName(ctx context.Context, orgID uuid.UUID) (string, error) {
	var name string
	err := s.conn(ctx).QueryRowContext(ctx, "SELECT name FROM "+s.prefix+"orgs WHERE id = ?", orgID.String()).Scan(&name)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("limits.sqlite: org name: %w", err)
	}
	return name, nil
}
