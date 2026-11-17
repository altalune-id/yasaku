package user

import (
	"context"

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
}
