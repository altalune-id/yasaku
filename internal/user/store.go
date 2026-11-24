package user

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Store is the driven port persisting User aggregates.
type Store interface {
	Save(ctx context.Context, u *User) error
	ByID(ctx context.Context, id uuid.UUID) (*User, error)
	ByEmail(ctx context.Context, email string) (*User, error)
	// ByIDP resolves the local user an IdP issuer/subject pair names.
	ByIDP(ctx context.Context, issuer, subject string) (*User, error)
	HasLocalUsers(ctx context.Context) (bool, error)
	UpdateLocale(ctx context.Context, id uuid.UUID, locale string) error
	// UpdateThemePrefs stores the user's theme family and color mode.
	UpdateThemePrefs(ctx context.Context, id uuid.UUID, theme, colorMode string) error
	// MarkTipsSeen adds ids to the user's seen tips, once each, and returns the stored list.
	MarkTipsSeen(ctx context.Context, id uuid.UUID, ids []string) ([]string, error)
	// ResetTipsSeen forgets every seen tip so the tips show again.
	ResetTipsSeen(ctx context.Context, id uuid.UUID) error
	// StampSignIn records that id signed in at at.
	StampSignIn(ctx context.Context, id uuid.UUID, at time.Time) error
	// SetSuspended suspends id from at, or lifts the suspension when at is nil.
	SetSuspended(ctx context.Context, id uuid.UUID, at *time.Time) error
	// AdminListOrgs lists every org with its counts for platform staff; no ledger data.
	AdminListOrgs(ctx context.Context) ([]AdminOrg, error)
	// AdminListUsers lists every user with sign-in and suspension state for platform staff.
	AdminListUsers(ctx context.Context) ([]AdminUser, error)
}

// AdminOrg is one org as platform staff see it: identity and counts, never ledger data.
type AdminOrg struct {
	ID           uuid.UUID
	Slug         string
	Name         string
	Plan         string
	CreatedAt    time.Time
	MemberCount  int64
	ProjectCount int64
	LastActive   *time.Time
}

// AdminUser is one user as platform staff see them.
type AdminUser struct {
	ID           uuid.UUID
	Email        string
	Name         string
	CreatedAt    time.Time
	LastSignInAt *time.Time
	SuspendedAt  *time.Time
	OrgCount     int64
}
