package org

import (
	"context"

	"github.com/google/uuid"
)

// Store persists Org aggregates and their memberships.
type Store interface {
	Save(ctx context.Context, o *Org) error
	BySlug(ctx context.Context, slug string) (*Org, error)
	SystemOrg(ctx context.Context) (*Org, error)
	ByID(ctx context.Context, id uuid.UUID) (*Org, error)
	List(ctx context.Context, userID uuid.UUID) ([]*Org, error)
	// CountCreatedBy counts the non-system orgs userID created, across every tenant.
	CountCreatedBy(ctx context.Context, userID uuid.UUID) (int64, error)
	// IsStaff reports whether userID holds the built-in owner or admin role in the system org.
	IsStaff(ctx context.Context, userID uuid.UUID) (bool, error)

	SaveMembership(ctx context.Context, m *Membership) error
	MembershipOf(ctx context.Context, orgID, userID uuid.UUID) (*Membership, error)
	// LockOrg holds orgID's row for the unit of work on ctx, so membership changes to one org run one at a time.
	LockOrg(ctx context.Context, orgID uuid.UUID) error
	// MemberRole returns userID's role in orgID, or "" when not a member, with the org's name, in one read.
	MemberRole(ctx context.Context, orgID, userID uuid.UUID) (Role, string, error)
	ListMembers(ctx context.Context, orgID uuid.UUID) ([]*Membership, error)
	ListMemberProfiles(ctx context.Context, orgID uuid.UUID) ([]*MemberProfile, error)
	RemoveMember(ctx context.Context, orgID, userID uuid.UUID) error
}
