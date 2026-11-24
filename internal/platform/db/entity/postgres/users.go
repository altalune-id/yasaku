// Package postgres holds jet table bindings for the Postgres dialect.
package postgres

import "github.com/go-jet/jet/v2/postgres"

// Users is the jet binding for the users table.
type Users struct {
	postgres.Table

	ID              postgres.ColumnString
	IDPIssuer       postgres.ColumnString
	IDPSubject      postgres.ColumnString
	Email           postgres.ColumnString
	Name            postgres.ColumnString
	AvatarURL       postgres.ColumnString
	PasswordHash    postgres.ColumnString
	IsAdmin         postgres.ColumnBool
	Locale          postgres.ColumnString
	Theme           postgres.ColumnString
	ColorMode       postgres.ColumnString
	TipsSeen        postgres.ColumnString
	TermsAcceptedAt postgres.ColumnTimestampz
	SuspendedAt     postgres.ColumnTimestampz
	LastSignInAt    postgres.ColumnTimestampz
	CreatedAt       postgres.ColumnTimestampz
	UpdatedAt       postgres.ColumnTimestampz

	AllColumns postgres.ColumnList
}

// NewUsers builds the users binding.
func NewUsers(schema, tablePrefix string) *Users {
	if schema == "" {
		schema = "public"
	}
	var (
		id              = postgres.StringColumn("id")
		idpIssuer       = postgres.StringColumn("idp_issuer")
		idpSubject      = postgres.StringColumn("idp_subject")
		email           = postgres.StringColumn("email")
		name            = postgres.StringColumn("name")
		avatarURL       = postgres.StringColumn("avatar_url")
		passwordHash    = postgres.StringColumn("password_hash")
		isAdmin         = postgres.BoolColumn("is_admin")
		locale          = postgres.StringColumn("locale")
		theme           = postgres.StringColumn("theme")
		colorMode       = postgres.StringColumn("color_mode")
		tipsSeen        = postgres.StringColumn("tips_seen")
		termsAcceptedAt = postgres.TimestampzColumn("terms_accepted_at")
		suspendedAt     = postgres.TimestampzColumn("suspended_at")
		lastSignInAt    = postgres.TimestampzColumn("last_sign_in_at")
		createdAt       = postgres.TimestampzColumn("created_at")
		updatedAt       = postgres.TimestampzColumn("updated_at")
		all             = postgres.ColumnList{id, idpIssuer, idpSubject, email, name, avatarURL, passwordHash, isAdmin, locale, theme, colorMode, tipsSeen, termsAcceptedAt, suspendedAt, lastSignInAt, createdAt, updatedAt}
	)
	return &Users{
		Table:           postgres.NewTable(schema, tablePrefix+"users", "users", all...),
		ID:              id,
		IDPIssuer:       idpIssuer,
		IDPSubject:      idpSubject,
		Email:           email,
		Name:            name,
		AvatarURL:       avatarURL,
		PasswordHash:    passwordHash,
		IsAdmin:         isAdmin,
		Locale:          locale,
		Theme:           theme,
		ColorMode:       colorMode,
		TipsSeen:        tipsSeen,
		TermsAcceptedAt: termsAcceptedAt,
		SuspendedAt:     suspendedAt,
		LastSignInAt:    lastSignInAt,
		CreatedAt:       createdAt,
		UpdatedAt:       updatedAt,
		AllColumns:      all,
	}
}
