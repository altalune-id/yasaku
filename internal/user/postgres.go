package user

import (
	"context"
	"errors"
	"time"

	"github.com/go-jet/jet/v2/qrm"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	pdb "altalune.id/yasaku/internal/platform/db"
	pgent "altalune.id/yasaku/internal/platform/db/entity/postgres"
)

type postgresStore struct {
	pool           pdb.Pool
	table          *pgent.Users
	adminOrgsStmt  string
	adminUsersStmt string
}

func newPostgresStore(pool pdb.Pool, schema, tablePrefix string) *postgresStore {
	if schema == "" {
		schema = "public"
	}
	fnPrefix := schema + "." + tablePrefix
	return &postgresStore{
		pool:           pool,
		table:          pgent.NewUsers(schema, tablePrefix),
		adminOrgsStmt:  `SELECT a.id, a.slug, a.name, a.plan, a.created_at, a.member_count, a.project_count, a.last_active FROM ` + fnPrefix + `admin_list_orgs() AS a`,
		adminUsersStmt: `SELECT a.id, a.email, a.name, a.created_at, a.last_sign_in_at, a.suspended_at, a.org_count FROM ` + fnPrefix + `admin_list_users() AS a`,
	}
}

type pgUserRow struct {
	ID              uuid.UUID  `alias:"users.id"`
	Email           string     `alias:"users.email"`
	Name            string     `alias:"users.name"`
	IsAdmin         bool       `alias:"users.is_admin"`
	IDPIssuer       *string    `alias:"users.idp_issuer"`
	IDPSubject      *string    `alias:"users.idp_subject"`
	PasswordHash    string     `alias:"users.password_hash"`
	Locale          string     `alias:"users.locale"`
	Theme           string     `alias:"users.theme"`
	ColorMode       string     `alias:"users.color_mode"`
	TipsSeen        string     `alias:"users.tips_seen"`
	TermsAcceptedAt *time.Time `alias:"users.terms_accepted_at"`
	LastSignInAt    *time.Time `alias:"users.last_sign_in_at"`
	SuspendedAt     *time.Time `alias:"users.suspended_at"`
	CreatedAt       time.Time  `alias:"users.created_at"`
}

func (r *pgUserRow) toUser() *User {
	u := &User{
		ID:           r.ID,
		Email:        r.Email,
		Name:         r.Name,
		IsAdmin:      r.IsAdmin,
		PasswordHash: r.PasswordHash,
		Locale:       r.Locale,
		Theme:        r.Theme,
		ColorMode:    r.ColorMode,
		TipsSeen:     parseTipsSeen(r.TipsSeen),
		CreatedAt:    r.CreatedAt.UTC(),
	}
	if r.IDPIssuer != nil {
		u.IDPIssuer = *r.IDPIssuer
	}
	if r.IDPSubject != nil {
		u.IDPSubject = *r.IDPSubject
	}
	u.Source = sourceFrom(r.IsAdmin, u.IDPIssuer != "", r.PasswordHash != "")
	if r.TermsAcceptedAt != nil {
		t := r.TermsAcceptedAt.UTC()
		u.TermsAcceptedAt = &t
	}
	if r.LastSignInAt != nil {
		t := r.LastSignInAt.UTC()
		u.LastSignInAt = &t
	}
	if r.SuspendedAt != nil {
		t := r.SuspendedAt.UTC()
		u.SuspendedAt = &t
	}
	return u
}

func (s *postgresStore) writer(ctx context.Context) qrm.DB {
	if tx, ok := pdb.CurrentTx(ctx); ok {
		return tx
	}
	return s.pool.W
}

func (s *postgresStore) reader(ctx context.Context) qrm.DB {
	if tx, ok := pdb.CurrentTx(ctx); ok {
		return tx
	}
	return s.pool.R
}

func sourceFrom(isAdmin, hasIDPIssuer, hasPassword bool) string {
	if hasIDPIssuer {
		return SourceOIDC
	}
	if hasPassword {
		return SourceLocal
	}
	if isAdmin {
		return SourceGenesis
	}
	return SourceGenesis
}

func isPGUniqueViolation(err error) (constraint string, ok bool) {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return pgErr.ConstraintName, true
	}
	return "", false
}
