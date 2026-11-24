package wallet

import (
	"context"

	"altalune.id/yasaku/internal/limits"
	"altalune.id/yasaku/internal/platform/tenant"
)

// Limiter locks a subject's row and refuses a create past its limit.
type Limiter interface {
	Lock(ctx context.Context, subs ...limits.Subject) error
	Check(ctx context.Context, key limits.Key, sub limits.Subject, used int64) error
}

// WithLimits holds every create and unarchive to the project's active-wallet limit.
func WithLimits(l Limiter) Option {
	return func(s *Service) { s.limits = l }
}

// NOTE: runs inside the write's unit of work, so the lock holds until the new row commits.
func (s *Service) checkActiveLimit(ctx context.Context, tc tenant.Context) error {
	if s.limits == nil {
		return nil
	}
	sub := limits.Subject{Kind: limits.SubjectProject, ID: tc.ProjectID}
	if err := s.limits.Lock(ctx, sub); err != nil {
		return s.unexpected(ctx, "wallet: limit lock", err, "project_id", tc.ProjectID)
	}
	active, err := s.store.List(ctx, tc.OrgID, tc.ProjectID, ListOpts{})
	if err != nil {
		return s.unexpected(ctx, "wallet: count active", err, "project_id", tc.ProjectID)
	}
	return s.limits.Check(ctx, limits.ProjectWallets, sub, int64(len(active)))
}
