// Package transaction is the money-movement bounded context.
package transaction

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"altalune.id/yasaku/money"
)

// Kind is what a transaction does to the wallets it names.
type Kind string

// The kinds a transaction may have; the last three are written by the system, not by hand.
const (
	KindIncome        Kind = "income"
	KindExpense       Kind = "expense"
	KindTransfer      Kind = "transfer"
	KindOpening       Kind = "opening"
	KindAdjustmentIn  Kind = "adjustment_in"
	KindAdjustmentOut Kind = "adjustment_out"
)

// MaxNoteRunes is the longest note a transaction may carry.
const MaxNoteRunes = 500

// DefaultListLimit is the page size List falls back to.
const DefaultListLimit = 50

// MaxListLimit caps a caller-supplied page size.
const MaxListLimit = 200

// ParseKind validates s against the supported kinds.
func ParseKind(s string) (Kind, error) {
	k := Kind(s)
	if !k.valid() {
		return "", &InvalidKindError{Kind: s, Reason: "unknown kind"}
	}
	return k, nil
}

func (k Kind) valid() bool {
	switch k {
	case KindIncome, KindExpense, KindTransfer, KindOpening, KindAdjustmentIn, KindAdjustmentOut:
		return true
	}
	return false
}

// IsInflow reports whether the kind adds to its wallet's balance.
func (k Kind) IsInflow() bool {
	switch k {
	case KindIncome, KindOpening, KindAdjustmentIn:
		return true
	case KindExpense, KindTransfer, KindAdjustmentOut:
		return false
	}
	return false
}

// AllowsCategory reports whether the kind may name a category.
func (k Kind) AllowsCategory() bool { return k == KindIncome || k == KindExpense }

// Transaction is the aggregate root: one movement of money.
// NOTE: Amount is always positive; the direction comes from Kind, never from the sign.
type Transaction struct {
	ID         uuid.UUID
	OrgID      uuid.UUID
	ProjectID  uuid.UUID
	WalletID   uuid.UUID
	ToWalletID *uuid.UUID
	Kind       Kind
	Amount     money.Amount
	CategoryID *uuid.UUID
	PeriodID   *uuid.UUID
	Note       string
	OccurredAt time.Time
	CreatedBy  uuid.UUID
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// NewParams carries the fields New needs beyond the tenant scope.
type NewParams struct {
	WalletID   uuid.UUID
	ToWalletID *uuid.UUID
	Kind       Kind
	Amount     money.Amount
	CategoryID *uuid.UUID
	PeriodID   *uuid.UUID
	Note       string
	OccurredAt time.Time
	CreatedBy  uuid.UUID
}

// New enforces the creation invariants and stamps id and timestamps.
func New(orgID, projectID uuid.UUID, p NewParams) (*Transaction, error) {
	now := time.Now().UTC()
	t := &Transaction{
		ID:         uuid.Must(uuid.NewV7()),
		OrgID:      orgID,
		ProjectID:  projectID,
		WalletID:   p.WalletID,
		ToWalletID: p.ToWalletID,
		Kind:       p.Kind,
		Amount:     p.Amount,
		CategoryID: p.CategoryID,
		PeriodID:   p.PeriodID,
		Note:       strings.TrimSpace(p.Note),
		OccurredAt: p.OccurredAt.UTC(),
		CreatedBy:  p.CreatedBy,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := t.validate(); err != nil {
		return nil, err
	}
	return t, nil
}

func (t *Transaction) validate() error {
	if !t.Kind.valid() {
		return &InvalidKindError{Kind: string(t.Kind), Reason: "unknown kind"}
	}
	if err := validAmount(t.Amount); err != nil {
		return err
	}
	if err := validNote(t.Note); err != nil {
		return err
	}
	if t.Kind == KindTransfer {
		if t.ToWalletID == nil {
			return &InvalidKindError{Kind: string(t.Kind), Reason: "transfer needs a destination wallet"}
		}
		if *t.ToWalletID == t.WalletID {
			return &SameWalletError{WalletID: t.WalletID.String()}
		}
	} else if t.ToWalletID != nil {
		return &InvalidKindError{Kind: string(t.Kind), Reason: "only a transfer may name a destination wallet"}
	}
	if t.CategoryID != nil && !t.Kind.AllowsCategory() {
		return &InvalidKindError{Kind: string(t.Kind), Reason: "only income and expense may name a category"}
	}
	return nil
}

func validAmount(a money.Amount) error {
	if !a.Currency.Valid() {
		return &InvalidAmountError{Reason: "unknown currency " + string(a.Currency)}
	}
	if !a.IsPositive() {
		return &InvalidAmountError{Reason: "must be greater than zero"}
	}
	return nil
}

func validNote(note string) error {
	if utf8.RuneCountInString(note) > MaxNoteRunes {
		return &InvalidNoteError{Reason: "over 500 characters"}
	}
	return nil
}

// Effect is what this transaction does to walletID's balance; zero when the wallet is unrelated.
func (t *Transaction) Effect(walletID uuid.UUID) money.Amount {
	if t.Kind == KindTransfer {
		switch {
		case walletID == t.WalletID:
			return t.Amount.Neg()
		case t.ToWalletID != nil && walletID == *t.ToWalletID:
			return t.Amount
		}
		return money.Zero(t.Amount.Currency)
	}
	if walletID != t.WalletID {
		return money.Zero(t.Amount.Currency)
	}
	if t.Kind.IsInflow() {
		return t.Amount
	}
	return t.Amount.Neg()
}

// SetAmount replaces the amount, keeping it positive.
func (t *Transaction) SetAmount(a money.Amount) error {
	if err := validAmount(a); err != nil {
		return err
	}
	t.Amount = a
	t.touch()
	return nil
}

// SetNote replaces the trimmed note.
func (t *Transaction) SetNote(note string) error {
	note = strings.TrimSpace(note)
	if err := validNote(note); err != nil {
		return err
	}
	t.Note = note
	t.touch()
	return nil
}

// SetOccurredAt moves the transaction in time.
func (t *Transaction) SetOccurredAt(at time.Time) {
	t.OccurredAt = at.UTC()
	t.touch()
}

// SetCategory assigns or clears the category, refusing one the kind may not carry.
func (t *Transaction) SetCategory(id *uuid.UUID) error {
	if id != nil && !t.Kind.AllowsCategory() {
		return &InvalidKindError{Kind: string(t.Kind), Reason: "only income and expense may name a category"}
	}
	t.CategoryID = id
	t.touch()
	return nil
}

// SetToWallet assigns or clears a transfer's destination wallet.
func (t *Transaction) SetToWallet(id *uuid.UUID) {
	t.ToWalletID = id
	t.touch()
}

// SetWallet moves the transaction to another wallet.
func (t *Transaction) SetWallet(id uuid.UUID) {
	t.WalletID = id
	t.touch()
}

// SetPeriod assigns or clears the budget period the transaction counts toward.
func (t *Transaction) SetPeriod(id *uuid.UUID) {
	t.PeriodID = id
	t.touch()
}

func (t *Transaction) touch() { t.UpdatedAt = time.Now().UTC() }

// NormalizeNote lower-cases, trims and collapses internal whitespace; it is the key note suggestions match on.
func NormalizeNote(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// ListOpts filters Store.List. The zero value returns the newest page in scope.
type ListOpts struct {
	WalletID   *uuid.UUID
	CategoryID *uuid.UUID
	PeriodID   *uuid.UUID
	Kinds      []Kind
	From       *time.Time
	To         *time.Time
	Search     string
	Limit      int
	After      *Cursor
}

// NormalizedLimit clamps Limit into the supported page-size range.
func (o ListOpts) NormalizedLimit() int {
	if o.Limit <= 0 {
		return DefaultListLimit
	}
	if o.Limit > MaxListLimit {
		return MaxListLimit
	}
	return o.Limit
}
