package limits

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
)

type postgresStore struct {
	db        *sql.DB
	schema    string
	prefix    string
	plans     *pgent.PlanLimits
	overrides *pgent.LimitOverrides
}

func newPostgresStore(db *sql.DB, schema, prefix string) *postgresStore {
	if schema == "" {
		schema = "public"
	}
	return &postgresStore{
		db: db, schema: schema, prefix: prefix,
		plans:     pgent.NewPlanLimits(schema, prefix),
		overrides: pgent.NewLimitOverrides(schema, prefix),
	}
}

type dbtx interface {
	qrm.DB
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func conn(ctx context.Context, db *sql.DB) dbtx {
	if tx, ok := pdb.CurrentTx(ctx); ok {
		return tx
	}
	return db
}

func (s *postgresStore) conn(ctx context.Context) dbtx { return conn(ctx, s.db) }

func (s *postgresStore) table(name string) string { return s.schema + "." + s.prefix + name }

func (s *postgresStore) planExpr(kind SubjectKind) string {
	switch kind {
	case SubjectOrg:
		return "(SELECT o.plan FROM " + s.table("orgs") + " o WHERE o.id = $2)"
	case SubjectProject:
		return "(SELECT o.plan FROM " + s.table("projects") + " p JOIN " + s.table("orgs") + " o ON o.id = p.org_id WHERE p.id = $2)"
	default:
		return "NULL"
	}
}

// NOTE: an org row hidden by RLS resolves to the default plan, so a check outside the org's scope never reads another plan.
func (s *postgresStore) Resolve(ctx context.Context, key Key, sub Subject) (value int64, ok bool, err error) {
	q := "SELECT value FROM (" +
		"SELECT value, 0 AS pri FROM " + s.table("limit_overrides") + " WHERE subject_kind = $1 AND subject_id = $2 AND key = $3 " +
		"UNION ALL SELECT value, 1 FROM " + s.table("plan_limits") + " WHERE key = $3 AND plan = COALESCE(" + s.planExpr(sub.Kind) + ", $4)" +
		") r ORDER BY pri LIMIT 1"
	var v sql.NullInt64
	err = s.conn(ctx).QueryRowContext(ctx, q, string(sub.Kind), sub.ID, string(key), DefaultPlan).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("limits.postgres: resolve: %w", err)
	}
	if !v.Valid {
		return Unlimited, true, nil
	}
	return v.Int64, true, nil
}

func (s *postgresStore) Lock(ctx context.Context, subs []Subject) error {
	tx, ok := pdb.CurrentTx(ctx)
	if !ok {
		return &NoUnitOfWorkError{}
	}
	for _, sub := range subs {
		var table string
		switch sub.Kind {
		case SubjectUser:
			table = s.table("users")
		case SubjectOrg:
			table = s.table("orgs")
		default:
			table = s.table("projects")
		}
		rows, err := tx.QueryContext(ctx, "SELECT 1 FROM "+table+" WHERE id = $1 FOR NO KEY UPDATE", sub.ID) //nolint:gosec // G202: table is schema plus prefix from config and a fixed suffix, never input

		if err != nil {
			return fmt.Errorf("limits.postgres: lock %s: %w", sub.Kind, err)
		}
		locked := rows.Next()
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return fmt.Errorf("limits.postgres: lock %s: %w", sub.Kind, err)
		}
		if !locked {
			return &SubjectMissingError{Subject: sub}
		}
	}
	return nil
}

func pgValue(v int64) postgres.IntegerExpression {
	if v == Unlimited {
		return pgent.NullBigint()
	}
	return postgres.Int64(v)
}

func pgBy(by uuid.UUID) postgres.StringExpression {
	if by == uuid.Nil {
		return pgent.NullUUID()
	}
	return postgres.UUID(by)
}

func (s *postgresStore) SetPlan(ctx context.Context, plan string, key Key, value int64, by uuid.UUID) error {
	now := time.Now().UTC()
	stmt := s.plans.INSERT(s.plans.AllColumns).
		VALUES(postgres.String(plan), postgres.String(string(key)), pgValue(value), postgres.TimestampzT(now), pgBy(by)).
		ON_CONFLICT(s.plans.Plan, s.plans.Key).
		DO_UPDATE(postgres.SET(
			s.plans.Value.SET(pgValue(value)),
			s.plans.UpdatedAt.SET(postgres.TimestampzT(now)),
			s.plans.UpdatedBy.SET(pgBy(by)),
		))
	if _, err := stmt.ExecContext(ctx, s.conn(ctx)); err != nil {
		return fmt.Errorf("limits.postgres: set plan: %w", err)
	}
	return nil
}

func (s *postgresStore) ClearPlan(ctx context.Context, plan string, key Key) error {
	stmt := s.plans.DELETE().WHERE(s.plans.Plan.EQ(postgres.String(plan)).AND(s.plans.Key.EQ(postgres.String(string(key)))))
	if _, err := stmt.ExecContext(ctx, s.conn(ctx)); err != nil {
		return fmt.Errorf("limits.postgres: clear plan: %w", err)
	}
	return nil
}

func (s *postgresStore) SetOverride(ctx context.Context, sub Subject, key Key, value int64, by uuid.UUID) error {
	now := time.Now().UTC()
	stmt := s.overrides.INSERT(s.overrides.AllColumns).
		VALUES(postgres.String(string(sub.Kind)), postgres.UUID(sub.ID), postgres.String(string(key)), pgValue(value), postgres.TimestampzT(now), pgBy(by)).
		ON_CONFLICT(s.overrides.SubjectKind, s.overrides.SubjectID, s.overrides.Key).
		DO_UPDATE(postgres.SET(
			s.overrides.Value.SET(pgValue(value)),
			s.overrides.UpdatedAt.SET(postgres.TimestampzT(now)),
			s.overrides.UpdatedBy.SET(pgBy(by)),
		))
	if _, err := stmt.ExecContext(ctx, s.conn(ctx)); err != nil {
		return fmt.Errorf("limits.postgres: set override: %w", err)
	}
	return nil
}

func (s *postgresStore) DeleteOverride(ctx context.Context, sub Subject, key Key) error {
	stmt := s.overrides.DELETE().WHERE(
		s.overrides.SubjectKind.EQ(postgres.String(string(sub.Kind))).
			AND(s.overrides.SubjectID.EQ(postgres.UUID(sub.ID))).
			AND(s.overrides.Key.EQ(postgres.String(string(key)))))
	if _, err := stmt.ExecContext(ctx, s.conn(ctx)); err != nil {
		return fmt.Errorf("limits.postgres: delete override: %w", err)
	}
	return nil
}

type pgPlanRow struct {
	Plan  string        `alias:"plan_limits.plan"`
	Key   string        `alias:"plan_limits.key"`
	Value sql.NullInt64 `alias:"plan_limits.value"`
}

func (s *postgresStore) ListPlan(ctx context.Context, plan string) ([]PlanEntry, error) {
	stmt := postgres.SELECT(s.plans.Plan, s.plans.Key, s.plans.Value).FROM(s.plans).
		WHERE(s.plans.Plan.EQ(postgres.String(plan))).ORDER_BY(s.plans.Key.ASC())
	var rows []pgPlanRow
	if err := stmt.QueryContext(ctx, s.conn(ctx), &rows); err != nil && !errors.Is(err, qrm.ErrNoRows) {
		return nil, fmt.Errorf("limits.postgres: list plan: %w", err)
	}
	out := make([]PlanEntry, 0, len(rows))
	for _, r := range rows {
		out = append(out, PlanEntry{Plan: r.Plan, Key: Key(r.Key), Value: nullToLimit(r.Value)})
	}
	return out, nil
}

type pgOverrideRow struct {
	SubjectKind string        `alias:"limit_overrides.subject_kind"`
	SubjectID   uuid.UUID     `alias:"limit_overrides.subject_id"`
	Key         string        `alias:"limit_overrides.key"`
	Value       sql.NullInt64 `alias:"limit_overrides.value"`
}

func (s *postgresStore) ListOverrides(ctx context.Context) ([]Override, error) {
	stmt := postgres.SELECT(s.overrides.SubjectKind, s.overrides.SubjectID, s.overrides.Key, s.overrides.Value).FROM(s.overrides).
		ORDER_BY(s.overrides.SubjectKind.ASC(), s.overrides.SubjectID.ASC(), s.overrides.Key.ASC())
	var rows []pgOverrideRow
	if err := stmt.QueryContext(ctx, s.conn(ctx), &rows); err != nil && !errors.Is(err, qrm.ErrNoRows) {
		return nil, fmt.Errorf("limits.postgres: list overrides: %w", err)
	}
	out := make([]Override, 0, len(rows))
	for _, r := range rows {
		out = append(out, Override{Subject: Subject{Kind: SubjectKind(r.SubjectKind), ID: r.SubjectID}, Key: Key(r.Key), Value: nullToLimit(r.Value)})
	}
	return out, nil
}

func nullToLimit(v sql.NullInt64) int64 {
	if !v.Valid {
		return Unlimited
	}
	return v.Int64
}

func (s *postgresStore) OrgName(ctx context.Context, orgID uuid.UUID) (string, error) {
	var name string
	err := s.conn(ctx).QueryRowContext(ctx, "SELECT name FROM "+s.table("orgs")+" WHERE id = $1", orgID).Scan(&name)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("limits.postgres: org name: %w", err)
	}
	return name, nil
}
