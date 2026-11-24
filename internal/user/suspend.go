package user

import (
	"context"
	"time"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/platform/audit"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/platform/tenant"
)

// StaffDirectory names platform staff and the system org that holds their audit rows.
type StaffDirectory interface {
	IsStaff(ctx context.Context, userID uuid.UUID) (bool, error)
	SystemOrgID(ctx context.Context) (uuid.UUID, error)
}

type suspension struct {
	staff   StaffDirectory
	uow     tenant.UnitOfWork
	audit   Auditor
	contact string
}

// WithSuspension lets staff suspend users, recording each change under the system org; contact is shown to a suspended user.
func WithSuspension(staff StaffDirectory, uow tenant.UnitOfWork, a Auditor, contact string) ServiceOption {
	return func(s *Service) { s.suspension = &suspension{staff: staff, uow: uow, audit: a, contact: contact} }
}

// Suspend stops userID from signing in or acting anywhere. SECURITY: only staff may call it, and staff, the caller included, cannot be suspended.
func (s *Service) Suspend(ctx context.Context, userID uuid.UUID) error {
	ctx, span := tracer.Start(ctx, "user.Suspend")
	defer span.End()
	return s.setSuspended(ctx, userID, new(s.now()), "user.suspended")
}

// Unsuspend lets userID sign in and act again. SECURITY: only staff may call it.
func (s *Service) Unsuspend(ctx context.Context, userID uuid.UUID) error {
	ctx, span := tracer.Start(ctx, "user.Unsuspend")
	defer span.End()
	return s.setSuspended(ctx, userID, nil, "user.unsuspended")
}

// RequireActive refuses a suspended user with *SuspendedError.
func (s *Service) RequireActive(ctx context.Context, userID uuid.UUID) error {
	u, err := s.store.ByID(ctx, userID)
	if err != nil {
		if IsNotFoundError(err) {
			return err
		}
		return s.unexpected(ctx, "user.RequireActive", err, "user_id", userID.String())
	}
	if u.SuspendedAt != nil {
		return &SuspendedError{Contact: s.contact()}
	}
	return nil
}

func (s *Service) contact() string {
	if s.suspension == nil {
		return ""
	}
	return s.suspension.contact
}

func (s *Service) setSuspended(ctx context.Context, userID uuid.UUID, at *time.Time, action string) error {
	sp := s.suspension
	if sp == nil {
		return &NotFoundError{ID: userID.String()}
	}
	caller := session.PrincipalFrom(ctx).UserID
	staff, err := s.isStaff(ctx, caller)
	if err != nil {
		return err
	}
	if !staff {
		return &NotFoundError{ID: userID.String()}
	}
	target, err := s.store.ByID(ctx, userID)
	if err != nil {
		if IsNotFoundError(err) {
			return err
		}
		return s.unexpected(ctx, "user.setSuspended: load", err, "user_id", userID.String())
	}
	if at != nil {
		if userID == caller {
			return &StaffProtectedError{}
		}
		if staff, err = s.isStaff(ctx, userID); err != nil {
			return err
		}
		if staff {
			return &StaffProtectedError{}
		}
	}
	sysID, err := sp.staff.SystemOrgID(ctx)
	if err != nil {
		return s.unexpected(ctx, "user.setSuspended: system org", err)
	}
	return sp.uow(tenant.Into(ctx, tenant.Context{OrgID: sysID, UserID: caller}), func(ctx context.Context) error {
		if err := s.store.SetSuspended(ctx, userID, at); err != nil {
			return s.unexpected(ctx, "user.setSuspended: save", err, "user_id", userID.String())
		}
		return sp.audit.Record(ctx, audit.Event{Action: action, TargetKind: "user", TargetID: userID.String(),
			Detail: map[string]string{"email": target.Email}})
	})
}

func (s *Service) isStaff(ctx context.Context, userID uuid.UUID) (bool, error) {
	if userID == uuid.Nil {
		return false, nil
	}
	staff, err := s.suspension.staff.IsStaff(ctx, userID)
	if err != nil {
		return false, s.unexpected(ctx, "user.isStaff", err, "user_id", userID.String())
	}
	return staff, nil
}
