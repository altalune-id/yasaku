package limits

import (
	"context"

	"github.com/google/uuid"
)

// Store persists plan limits and overrides and takes the row locks Lock asks for.
type Store interface {
	// Resolve returns the override for (key, sub), else the plan row for sub's plan; ok is false when neither exists. A NULL value reads as Unlimited.
	Resolve(ctx context.Context, key Key, sub Subject) (value int64, ok bool, err error)
	// Lock takes a row lock per subject, in the order given, inside the transaction on ctx.
	Lock(ctx context.Context, subs []Subject) error
	SetPlan(ctx context.Context, plan string, key Key, value int64, by uuid.UUID) error
	ClearPlan(ctx context.Context, plan string, key Key) error
	SetOverride(ctx context.Context, sub Subject, key Key, value int64, by uuid.UUID) error
	DeleteOverride(ctx context.Context, sub Subject, key Key) error
	ListPlan(ctx context.Context, plan string) ([]PlanEntry, error)
	ListOverrides(ctx context.Context) ([]Override, error)
	// OrgName returns the display name of orgID, for the refusal message.
	OrgName(ctx context.Context, orgID uuid.UUID) (string, error)
}
