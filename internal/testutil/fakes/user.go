package fakes

import (
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/user"
)

// User is an in-memory user.Store for tests.
type User struct {
	mu    sync.Mutex
	byID  map[uuid.UUID]*user.User
	byMap map[string]uuid.UUID
	byIDP map[string]uuid.UUID
	// SaveErr, if non-nil, is returned from Save (once, unless StickyError is true).
	SaveErr     error
	ByIDErr     error
	ByEmailErr  error
	ByIDPErr    error
	StickyError bool
}

var _ user.Store = (*User)(nil)

// NewUser builds an empty fake user.Store.
func NewUser() *User {
	return &User{
		byID:  map[uuid.UUID]*user.User{},
		byMap: map[string]uuid.UUID{},
		byIDP: map[string]uuid.UUID{},
	}
}

// Save stores a copy under both id and email.
func (f *User) Save(_ context.Context, u *user.User) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.SaveErr != nil {
		err := f.SaveErr
		if !f.StickyError {
			f.SaveErr = nil
		}
		return err
	}
	if u == nil {
		return nil
	}
	email := strings.ToLower(strings.TrimSpace(u.Email))
	if existing, ok := f.byMap[email]; ok && existing != u.ID {
		return &user.AlreadyExistsError{Field: "email", Value: u.Email}
	}
	idp := idpKey(u.IDPIssuer, u.IDPSubject)
	if idp != "" {
		if existing, ok := f.byIDP[idp]; ok && existing != u.ID {
			return &user.AlreadyExistsError{Field: "idp_subject", Value: u.IDPSubject}
		}
	}
	if prev, ok := f.byID[u.ID]; ok {
		delete(f.byMap, strings.ToLower(strings.TrimSpace(prev.Email)))
		delete(f.byIDP, idpKey(prev.IDPIssuer, prev.IDPSubject))
	}
	cp := *u
	if prev, ok := f.byID[u.ID]; ok {
		cp.LastSignInAt, cp.SuspendedAt = prev.LastSignInAt, prev.SuspendedAt
	}
	f.byID[u.ID] = &cp
	f.byMap[email] = u.ID
	if idp != "" {
		f.byIDP[idp] = u.ID
	}
	return nil
}

// ByIDP returns a copy of the user linked to the issuer/subject pair, if present.
func (f *User) ByIDP(_ context.Context, issuer, subject string) (*user.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ByIDPErr != nil {
		err := f.ByIDPErr
		if !f.StickyError {
			f.ByIDPErr = nil
		}
		return nil, err
	}
	id, ok := f.byIDP[idpKey(issuer, subject)]
	if !ok {
		return nil, &user.NotFoundError{Subject: subject}
	}
	cp := *f.byID[id]
	return &cp, nil
}

func idpKey(issuer, subject string) string {
	if issuer == "" || subject == "" {
		return ""
	}
	return issuer + "\x00" + subject
}

// ByID returns a copy of the user if present.
func (f *User) ByID(_ context.Context, id uuid.UUID) (*user.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ByIDErr != nil {
		err := f.ByIDErr
		if !f.StickyError {
			f.ByIDErr = nil
		}
		return nil, err
	}
	u, ok := f.byID[id]
	if !ok {
		return nil, &user.NotFoundError{ID: id.String()}
	}
	cp := *u
	return &cp, nil
}

// ByEmail returns a copy of the user if present.
func (f *User) ByEmail(_ context.Context, email string) (*user.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ByEmailErr != nil {
		err := f.ByEmailErr
		if !f.StickyError {
			f.ByEmailErr = nil
		}
		return nil, err
	}
	id, ok := f.byMap[strings.ToLower(strings.TrimSpace(email))]
	if !ok {
		return nil, &user.NotFoundError{Email: email}
	}
	cp := *f.byID[id]
	return &cp, nil
}

// Len returns the number of stored users (test helper).
func (f *User) Len() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.byID)
}

// HasLocalUsers reports whether any stored user carries a non-empty password hash.
func (f *User) HasLocalUsers(_ context.Context) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, u := range f.byID {
		if u.PasswordHash != "" {
			return true, nil
		}
	}
	return false, nil
}

// UpdateThemePrefs sets the stored user's theme family and color mode.
func (f *User) UpdateThemePrefs(_ context.Context, id uuid.UUID, theme, colorMode string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.byID[id]
	if !ok {
		return &user.NotFoundError{ID: id.String()}
	}
	u.Theme, u.ColorMode = theme, colorMode
	return nil
}

// MarkTipsSeen appends ids to the stored user's seen tips, once each.
func (f *User) MarkTipsSeen(_ context.Context, id uuid.UUID, ids []string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.byID[id]
	if !ok {
		return nil, &user.NotFoundError{ID: id.String()}
	}
	for _, t := range ids {
		if !slices.Contains(u.TipsSeen, t) {
			u.TipsSeen = append(u.TipsSeen, t)
		}
	}
	return slices.Clone(u.TipsSeen), nil
}

// ResetTipsSeen clears the stored user's seen tips.
func (f *User) ResetTipsSeen(_ context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.byID[id]
	if !ok {
		return &user.NotFoundError{ID: id.String()}
	}
	u.TipsSeen = nil
	return nil
}

// UpdateLocale sets the stored user's locale.
func (f *User) UpdateLocale(_ context.Context, id uuid.UUID, locale string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.byID[id]
	if !ok {
		return &user.NotFoundError{ID: id.String()}
	}
	u.Locale = locale
	return nil
}

// StampSignIn records the sign-in time; Save leaves it alone, as the stores do.
func (f *User) StampSignIn(_ context.Context, id uuid.UUID, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.byID[id]
	if !ok {
		return &user.NotFoundError{ID: id.String()}
	}
	t := at.UTC()
	u.LastSignInAt = &t
	return nil
}

// SetSuspended sets or clears the suspension; Save leaves it alone, as the stores do.
func (f *User) SetSuspended(_ context.Context, id uuid.UUID, at *time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.byID[id]
	if !ok {
		return &user.NotFoundError{ID: id.String()}
	}
	u.SuspendedAt = nil
	if at != nil {
		t := at.UTC()
		u.SuspendedAt = &t
	}
	return nil
}

// AdminListOrgs returns no orgs: the fake user store knows none.
func (f *User) AdminListOrgs(context.Context) ([]user.AdminOrg, error) {
	return nil, nil
}

// AdminListUsers lists every stored user with no org count.
func (f *User) AdminListUsers(context.Context) ([]user.AdminUser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]user.AdminUser, 0, len(f.byID))
	for _, u := range f.byID {
		out = append(out, user.AdminUser{ID: u.ID, Email: u.Email, Name: u.Name, CreatedAt: u.CreatedAt, LastSignInAt: u.LastSignInAt, SuspendedAt: u.SuspendedAt})
	}
	return out, nil
}
