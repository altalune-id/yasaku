package user

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/limits"
)

// Limiter locks a subject's row and refuses a create past its limit.
type Limiter interface {
	Lock(ctx context.Context, subs ...limits.Subject) error
	Check(ctx context.Context, key limits.Key, sub limits.Subject, used int64) error
}

// MemberCounter counts what the membership limits measure, names the org a refusal shows and exempts staff from user limits.
type MemberCounter interface {
	CountMembers(ctx context.Context, orgID uuid.UUID) (int64, error)
	CountOrgsJoined(ctx context.Context, userID uuid.UUID) (int64, error)
	OrgName(ctx context.Context, orgID uuid.UUID) (string, error)
	IsStaff(ctx context.Context, userID uuid.UUID) (bool, error)
}

// WithOnboardLimits holds an auto-accepted invite to the user's org-membership limit and the org's seats.
func WithOnboardLimits(l Limiter, c MemberCounter) OnboardOption {
	return func(w *OnboardWorkflow) { w.limits, w.counter = l, c }
}

func (w *OnboardWorkflow) checkJoin(ctx context.Context, userID, orgID uuid.UUID) error {
	if w.limits == nil {
		return nil
	}
	userSub := limits.Subject{Kind: limits.SubjectUser, ID: userID}
	orgSub := limits.Subject{Kind: limits.SubjectOrg, ID: orgID}
	if err := w.limits.Lock(ctx, userSub, orgSub); err != nil {
		return w.unexpected(ctx, "user.Onboard: limit lock", err)
	}
	staff, joined, err := w.orgsJoined(ctx, userID)
	if err != nil {
		return err
	}
	if !staff {
		err = w.limits.Check(ctx, limits.UserOrgsJoined, userSub, joined)
	}
	if err == nil {
		members, cErr := w.counter.CountMembers(ctx, orgID)
		if cErr != nil {
			return w.unexpected(ctx, "user.Onboard: count members", cErr)
		}
		err = w.limits.Check(ctx, limits.OrgMembers, orgSub, members)
	}
	re, ok := errors.AsType[*limits.ReachedError](err)
	if !ok {
		return err
	}
	if re.Name, err = w.counter.OrgName(ctx, orgID); err != nil {
		return w.unexpected(ctx, "user.Onboard: org name", err)
	}
	return re
}

func (w *OnboardWorkflow) orgsJoined(ctx context.Context, userID uuid.UUID) (staff bool, joined int64, err error) { //nolint:nonamedreturns // two results of different meaning read cleaner named
	staff, err = w.counter.IsStaff(ctx, userID)
	if err != nil {
		return false, 0, w.unexpected(ctx, "user.Onboard: staff check", err)
	}
	if staff {
		return true, 0, nil
	}
	joined, err = w.counter.CountOrgsJoined(ctx, userID)
	if err != nil {
		return false, 0, w.unexpected(ctx, "user.Onboard: count orgs joined", err)
	}
	return false, joined, nil
}
