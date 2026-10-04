package transaction

import (
	"context"

	"github.com/google/uuid"
)

const (
	// MirrorTransaction is a transaction's entity name for a Mirror.
	MirrorTransaction = "transaction"
	// MirrorWallet is a wallet's entity name for a Mirror; a transaction moves its wallets' balances.
	MirrorWallet = "wallet"
)

// MirrorRef names one row an outside mirror must re-read; Deleted marks it gone.
type MirrorRef struct {
	Entity  string
	ID      uuid.UUID
	Deleted bool
	Cascade bool
}

// Mirror follows the rows this module changes: Mark runs inside the write's unit of work, Kick after its commit.
type Mirror interface {
	Mark(ctx context.Context, refs ...MirrorRef) error
	Kick(ctx context.Context, refs ...MirrorRef)
}

type nopMirror struct{}

func (nopMirror) Mark(context.Context, ...MirrorRef) error { return nil }

func (nopMirror) Kick(context.Context, ...MirrorRef) {}

// Option configures a Service beyond its required dependencies.
type Option func(*Service)

// WithMirror makes every write mark m inside its unit of work and kick it after the commit; a nil m keeps the no-op mirror.
func WithMirror(m Mirror) Option {
	return func(s *Service) {
		if m != nil {
			s.mirror = m
		}
	}
}

func touched(t *Transaction, deleted bool) []MirrorRef {
	refs := []MirrorRef{{Entity: MirrorTransaction, ID: t.ID, Deleted: deleted}, {Entity: MirrorWallet, ID: t.WalletID}}
	if t.ToWalletID != nil {
		refs = append(refs, MirrorRef{Entity: MirrorWallet, ID: *t.ToWalletID})
	}
	return refs
}

// NOTE: commit runs write and marks the refs it returns in one unit of work; the caller kicks them after it returns.
func (s *Service) commit(ctx context.Context, op string, write func(ctx context.Context) ([]MirrorRef, error)) ([]MirrorRef, error) {
	var refs []MirrorRef
	var inner error
	uowErr := s.uow(ctx, func(ctx context.Context) error {
		refs, inner = write(ctx)
		if inner == nil {
			inner = s.mark(ctx, op, refs)
		}
		return inner
	})
	if inner != nil {
		return nil, inner
	}
	if uowErr != nil {
		return nil, s.unexpected(ctx, op+": unit of work", uowErr)
	}
	return refs, nil
}

func (s *Service) mark(ctx context.Context, op string, refs []MirrorRef) error {
	if err := s.mirror.Mark(ctx, refs...); err != nil {
		return s.unexpected(ctx, op+": mark", err)
	}
	return nil
}
