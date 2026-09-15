// Package wallet is the wallets bounded context: a named holder of money in one currency.
package wallet

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"altalune.id/yasaku/money"
)

// MaxNameRunes bounds a wallet name.
const MaxNameRunes = 80

// Kind classifies where a wallet's money physically sits.
type Kind string

// The supported wallet kinds; the set is mirrored by a CHECK constraint on the wallets table.
const (
	KindCash       Kind = "cash"
	KindBank       Kind = "bank"
	KindEwallet    Kind = "ewallet"
	KindSavings    Kind = "savings"
	KindInvestment Kind = "investment"
	KindOther      Kind = "other"
)

// ParseKind validates s case-insensitively.
func ParseKind(s string) (Kind, error) {
	k := Kind(strings.ToLower(strings.TrimSpace(s)))
	if !k.Valid() {
		return "", &InvalidKindError{Value: s}
	}
	return k, nil
}

// Valid reports whether k is a supported kind.
func (k Kind) Valid() bool {
	switch k {
	case KindCash, KindBank, KindEwallet, KindSavings, KindInvestment, KindOther:
		return true
	}
	return false
}

// DefaultExcludeFromTotal reports the suggested exclude-from-total flag for a new wallet of this kind.
func (k Kind) DefaultExcludeFromTotal() bool {
	return k == KindSavings || k == KindInvestment
}

// Params carries the caller-supplied fields of a new wallet.
type Params struct {
	Name             string
	Kind             Kind
	Provider         string
	Currency         money.Currency
	ExcludeFromTotal bool
}

// Wallet is the aggregate root; its balance is derived from transactions and never stored here.
type Wallet struct {
	ID               uuid.UUID
	OrgID            uuid.UUID
	ProjectID        uuid.UUID
	Name             string
	Kind             Kind
	Provider         string
	Currency         money.Currency
	ExcludeFromTotal bool
	ArchivedAt       *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// ListOpts filters a wallet listing; the zero value returns every active wallet in scope.
type ListOpts struct {
	IncludeArchived bool
}

// New enforces creation invariants: name trimmed and bounded, kind known, currency supported.
func New(orgID, projectID uuid.UUID, p Params) (*Wallet, error) {
	name, err := cleanName(p.Name)
	if err != nil {
		return nil, err
	}
	if !p.Kind.Valid() {
		return nil, &InvalidKindError{Value: string(p.Kind)}
	}
	if !p.Currency.Valid() {
		return nil, &money.UnknownCurrencyError{Code: string(p.Currency)}
	}
	now := time.Now().UTC()
	return &Wallet{
		ID:               uuid.Must(uuid.NewV7()),
		OrgID:            orgID,
		ProjectID:        projectID,
		Name:             name,
		Kind:             p.Kind,
		Provider:         strings.TrimSpace(p.Provider),
		Currency:         p.Currency,
		ExcludeFromTotal: p.ExcludeFromTotal,
		CreatedAt:        now,
		UpdatedAt:        now,
	}, nil
}

// Rename replaces the display name; it stays available while archived so a wallet whose name was
// taken after archiving can be renamed out of the way and then unarchived.
func (w *Wallet) Rename(name string) error {
	clean, err := cleanName(name)
	if err != nil {
		return err
	}
	w.Name = clean
	w.UpdatedAt = time.Now().UTC()
	return nil
}

// Update replaces the kind, provider and exclude-from-total flag; an archived wallet must be unarchived first.
func (w *Wallet) Update(kind Kind, provider string, exclude bool) error {
	if w.IsArchived() {
		return &ArchivedError{ID: w.ID.String()}
	}
	if !kind.Valid() {
		return &InvalidKindError{Value: string(kind)}
	}
	w.Kind = kind
	w.Provider = strings.TrimSpace(provider)
	w.ExcludeFromTotal = exclude
	w.UpdatedAt = time.Now().UTC()
	return nil
}

// Archive retires the wallet, freeing its name for reuse; calling it again is a no-op.
func (w *Wallet) Archive() {
	if w.IsArchived() {
		return
	}
	now := time.Now().UTC()
	w.ArchivedAt = &now
	w.UpdatedAt = now
}

// Unarchive returns the wallet to active use; calling it on an active wallet is a no-op.
func (w *Wallet) Unarchive() {
	if !w.IsArchived() {
		return
	}
	w.ArchivedAt = nil
	w.UpdatedAt = time.Now().UTC()
}

// IsArchived reports whether the wallet has been retired.
func (w *Wallet) IsArchived() bool { return w.ArchivedAt != nil }

func cleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", &InvalidNameError{Reason: "empty"}
	}
	if utf8.RuneCountInString(name) > MaxNameRunes {
		return "", &InvalidNameError{Reason: "over 80 characters"}
	}
	return name, nil
}
