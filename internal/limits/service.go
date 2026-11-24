package limits

import (
	"context"
	"fmt"
	"slices"

	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/db"
)

// Service resolves and enforces limits.
type Service struct {
	store   Store
	mode    config.Mode
	contact string
}

// Option configures a Service.
type Option func(*Service)

// WithContact names the address a cloud refusal tells the user to write to.
func WithContact(email string) Option {
	return func(s *Service) { s.contact = email }
}

// NewService binds the service to its store and deployment mode.
func NewService(store Store, mode config.Mode, opts ...Option) *Service {
	s := &Service{store: store, mode: mode}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Lock takes a row lock on each subject, user then org then project, so two concurrent creates serialize. It needs a unit of work on ctx.
func (s *Service) Lock(ctx context.Context, subs ...Subject) error {
	if _, ok := db.CurrentTx(ctx); !ok {
		return &NoUnitOfWorkError{}
	}
	sorted := slices.Clone(subs)
	slices.SortStableFunc(sorted, func(a, b Subject) int { return subjectRank(a.Kind) - subjectRank(b.Kind) })
	if err := s.store.Lock(ctx, sorted); err != nil {
		return fmt.Errorf("limits: lock: %w", err)
	}
	return nil
}

func subjectRank(k SubjectKind) int {
	switch k {
	case SubjectUser:
		return 0
	case SubjectOrg:
		return 1
	default:
		return 2
	}
}

// Check returns a *ReachedError when used is at or past the effective limit for (key, sub).
func (s *Service) Check(ctx context.Context, key Key, sub Subject, used int64) error {
	def, ok := Lookup(key)
	if !ok {
		return &UnknownKeyError{Key: key}
	}
	limit, err := s.effective(ctx, def, sub)
	if err != nil {
		return err
	}
	if limit == Unlimited || used < limit {
		return nil
	}
	re := &ReachedError{Key: key, Max: limit, Reason: ReasonLimit, Hidden: def.Hidden, mode: s.mode, contact: s.contact}
	if sub.Kind == SubjectOrg {
		if re.Name, err = s.store.OrgName(ctx, sub.ID); err != nil {
			return fmt.Errorf("limits: org name: %w", err)
		}
	}
	return re
}

// Effective returns the limit for (key, sub): the override, else the plan row, else the mode's default.
func (s *Service) Effective(ctx context.Context, key Key, sub Subject) (int64, error) {
	def, ok := Lookup(key)
	if !ok {
		return 0, &UnknownKeyError{Key: key}
	}
	return s.effective(ctx, def, sub)
}

func (s *Service) effective(ctx context.Context, def Def, sub Subject) (int64, error) {
	v, ok, err := s.store.Resolve(ctx, def.Key, sub)
	if err != nil {
		return 0, fmt.Errorf("limits: resolve %s: %w", def.Key, err)
	}
	if ok {
		return v, nil
	}
	if s.mode == config.ModeCloud {
		return def.Cloud, nil
	}
	return def.SelfHosted, nil
}
