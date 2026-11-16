package user

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-jet/jet/v2/postgres"
	"github.com/go-jet/jet/v2/qrm"
	"github.com/google/uuid"

	pgent "altalune.id/yasaku/internal/platform/db/entity/postgres"
)

func (s *postgresStore) Save(ctx context.Context, u *User) error {
	isAdmin := u.IsAdmin || u.Source == SourceGenesis || u.Source == SourceLocal
	now := time.Now().UTC()
	stmt := s.table.INSERT(
		s.table.ID,
		s.table.Email,
		s.table.Name,
		s.table.IDPIssuer,
		s.table.IDPSubject,
		s.table.AvatarURL,
		s.table.PasswordHash,
		s.table.IsAdmin,
		s.table.Locale,
		s.table.TermsAcceptedAt,
		s.table.CreatedAt,
		s.table.UpdatedAt,
	).
		VALUES(
			u.ID,
			u.Email,
			u.Name,
			nullableStringExpr(u.IDPIssuer),
			nullableStringExpr(u.IDPSubject),
			"",
			u.PasswordHash,
			isAdmin,
			u.Locale,
			nullableTimeExpr(u.TermsAcceptedAt),
			u.CreatedAt.UTC(),
			now,
		).
		ON_CONFLICT(s.table.ID).
		DO_UPDATE(postgres.SET(
			s.table.Email.SET(postgres.String(u.Email)),
			s.table.Name.SET(postgres.String(u.Name)),
			s.table.IDPIssuer.SET(nullableStringExpr(u.IDPIssuer)),
			s.table.IDPSubject.SET(nullableStringExpr(u.IDPSubject)),
			s.table.IsAdmin.SET(postgres.Bool(isAdmin)),
			s.table.PasswordHash.SET(postgres.String(u.PasswordHash)),
			s.table.Locale.SET(postgres.String(u.Locale)),
			s.table.TermsAcceptedAt.SET(nullableTimeExpr(u.TermsAcceptedAt)),
			s.table.UpdatedAt.SET(postgres.TimestampzT(now)),
		))
	if _, err := stmt.ExecContext(ctx, s.writer(ctx)); err != nil {
		if constraint, ok := isPGUniqueViolation(err); ok {
			if strings.HasSuffix(constraint, "users_idp_idx") {
				return &AlreadyExistsError{Field: "idp_subject", Value: u.IDPSubject}
			}
			return &AlreadyExistsError{Field: "email", Value: u.Email}
		}
		return fmt.Errorf("user.postgres.Save: %w", err)
	}
	return nil
}

func (s *postgresStore) UpdateThemePrefs(ctx context.Context, id uuid.UUID, theme, colorMode string) error {
	stmt := s.table.UPDATE(s.table.Theme, s.table.ColorMode, s.table.UpdatedAt).
		SET(postgres.String(theme), postgres.String(colorMode), postgres.TimestampzT(time.Now().UTC())).
		WHERE(s.table.ID.EQ(postgres.UUID(id)))
	res, err := stmt.ExecContext(ctx, s.writer(ctx))
	if err != nil {
		return fmt.Errorf("user.postgres.UpdateThemePrefs: %w", err)
	}
	n, raErr := res.RowsAffected()
	if raErr == nil && n == 0 {
		return &NotFoundError{ID: id.String()}
	}
	return nil
}

func (s *postgresStore) UpdateLocale(ctx context.Context, id uuid.UUID, locale string) error {
	stmt := s.table.UPDATE(s.table.Locale, s.table.UpdatedAt).
		SET(postgres.String(locale), postgres.TimestampzT(time.Now().UTC())).
		WHERE(s.table.ID.EQ(postgres.UUID(id)))
	res, err := stmt.ExecContext(ctx, s.writer(ctx))
	if err != nil {
		return fmt.Errorf("user.postgres.UpdateLocale: %w", err)
	}
	n, raErr := res.RowsAffected()
	if raErr == nil && n == 0 {
		return &NotFoundError{ID: id.String()}
	}
	return nil
}

// NOTE: users_idp_idx is partial on idp_issuer IS NOT NULL, so "" here would collide every password user.
func nullableStringExpr(v string) postgres.StringExpression {
	if v == "" {
		return pgent.NullText()
	}
	return postgres.String(v)
}

func nullableTimeExpr(t *time.Time) postgres.TimestampzExpression {
	if t == nil {
		return pgent.NullTimestampz()
	}
	return postgres.TimestampzT(t.UTC())
}

func (s *postgresStore) MarkTipsSeen(ctx context.Context, id uuid.UUID, ids []string) ([]string, error) {
	where := s.table.ID.EQ(postgres.UUID(id))
	for range tipsSeenAttempts {
		var row struct {
			TipsSeen string `alias:"users.tips_seen"`
		}
		if err := postgres.SELECT(s.table.TipsSeen).FROM(s.table).WHERE(where).LIMIT(1).QueryContext(ctx, s.writer(ctx), &row); err != nil {
			if errors.Is(err, qrm.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
				return nil, &NotFoundError{ID: id.String()}
			}
			return nil, fmt.Errorf("user.postgres.MarkTipsSeen: %w", err)
		}
		merged, raw := mergeTipsSeen(row.TipsSeen, ids)
		if raw == row.TipsSeen {
			return merged, nil
		}
		res, err := s.table.UPDATE(s.table.TipsSeen, s.table.UpdatedAt).
			SET(postgres.String(raw), postgres.TimestampzT(time.Now().UTC())).
			WHERE(where.AND(s.table.TipsSeen.EQ(postgres.String(row.TipsSeen)))).
			ExecContext(ctx, s.writer(ctx))
		if err != nil {
			return nil, fmt.Errorf("user.postgres.MarkTipsSeen: %w", err)
		}
		if n, err := res.RowsAffected(); err == nil && n == 1 {
			return merged, nil
		}
	}
	return nil, fmt.Errorf("user.postgres.MarkTipsSeen: %w", &TipsSeenContendedError{ID: id.String(), Attempts: tipsSeenAttempts})
}
