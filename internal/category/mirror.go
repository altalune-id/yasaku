package category

import (
	"context"

	"github.com/google/uuid"
)

// MirrorCategory is a category's entity name for a Mirror.
const MirrorCategory = "category"

// MirrorRef names one row an outside mirror must re-read; Deleted marks it gone and Cascade also re-reads the rows that show its name.
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

// UnitOfWork runs fn so that every write inside it commits or rolls back together.
type UnitOfWork func(ctx context.Context, fn func(ctx context.Context) error) error

type nopMirror struct{}

func (nopMirror) Mark(context.Context, ...MirrorRef) error { return nil }

func (nopMirror) Kick(context.Context, ...MirrorRef) {}

// Option configures a Service beyond its required dependencies.
type Option func(*Service)

// WithMirror makes every write run in uow, mark m inside it and kick m after the commit; a nil m keeps the no-op mirror, and a mirror without a unit of work panics at wiring time.
func WithMirror(m Mirror, uow UnitOfWork) Option {
	if m != nil && uow == nil {
		panic("category: WithMirror needs a unit of work, or a mark would not commit with its write")
	}
	return func(s *Service) {
		if m != nil {
			s.mirror = m
		}
		if uow != nil {
			s.uow = uow
		}
	}
}

func passthrough(ctx context.Context, fn func(ctx context.Context) error) error { return fn(ctx) }

// NOTE: commit runs write and marks the refs it returns in one unit of work; the caller kicks them after it returns.
func (s *Service) commit(ctx context.Context, op string, write func(ctx context.Context) ([]MirrorRef, error)) ([]MirrorRef, error) {
	var refs []MirrorRef
	var inner error
	uowErr := s.uow(ctx, func(ctx context.Context) error {
		refs, inner = write(ctx)
		if inner == nil {
			if err := s.mirror.Mark(ctx, refs...); err != nil {
				inner = s.unexpected(ctx, op+": mark", err)
			}
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
