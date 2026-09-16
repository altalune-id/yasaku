package fakes

import (
	"context"
	"strings"
	"sync"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/user"
)

// User is an in-memory user.Store for tests.
type User struct {
	mu    sync.Mutex
	byID  map[uuid.UUID]*user.User
	byMap map[string]uuid.UUID
	// NOTE: deliberately permissive — byIDP enforces no uniqueness, unlike the real users_idp_idx.
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
	// Clean up old email and IDP indices if user exists and they've changed
	if oldUser, ok := f.byID[u.ID]; ok {
		oldEmail := strings.ToLower(strings.TrimSpace(oldUser.Email))
		if oldEmail != email {
			delete(f.byMap, oldEmail)
		}
		if oldUser.IDPIssuer != "" && oldUser.IDPSubject != "" {
			oldKey := idpKey(oldUser.IDPIssuer, oldUser.IDPSubject)
			newKey := idpKey(u.IDPIssuer, u.IDPSubject)
			if oldKey != newKey {
				delete(f.byIDP, oldKey)
			}
		}
	}
	cp := *u
	f.byID[u.ID] = &cp
	f.byMap[email] = u.ID
	if u.IDPIssuer != "" && u.IDPSubject != "" {
		f.byIDP[idpKey(u.IDPIssuer, u.IDPSubject)] = u.ID
	}
	return nil
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

// ByIDP returns a copy of the user linked to the given issuer and subject.
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

func idpKey(issuer, subject string) string { return issuer + "\x00" + subject }

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
