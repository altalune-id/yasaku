package user

import (
	"context"

	"altalune.id/yasaku/internal/platform/audit"
)

// Auditor records an onboarding action in the caller's unit of work.
type Auditor interface {
	Record(ctx context.Context, e audit.Event) error
}

// WithOnboardAudit records each auto-accepted invite as member.joined.
func WithOnboardAudit(a Auditor) OnboardOption {
	return func(w *OnboardWorkflow) { w.audit = a }
}
