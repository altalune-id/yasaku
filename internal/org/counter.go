package org

import (
	"context"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/platform/capabilities"
)

// Counter answers the membership counts, org names and staff check that limit checks read, straight from the store.
type Counter struct {
	store Store
	caps  capabilities.Capabilities
}

// NewCounter binds a Counter to the store; staff is only ever true when caps grants support access.
func NewCounter(store Store, caps capabilities.Capabilities) Counter {
	return Counter{store: store, caps: caps}
}

// CountMembers counts orgID's memberships.
func (c Counter) CountMembers(ctx context.Context, orgID uuid.UUID) (int64, error) {
	members, err := c.store.ListMembers(ctx, orgID)
	return int64(len(members)), err
}

// CountOrgsJoined counts the orgs userID belongs to, the system org aside.
func (c Counter) CountOrgsJoined(ctx context.Context, userID uuid.UUID) (int64, error) {
	orgs, err := c.store.List(ctx, userID)
	if err != nil {
		return 0, err
	}
	var n int64
	for _, o := range orgs {
		if !o.System {
			n++
		}
	}
	return n, nil
}

// IsStaff reports whether userID owns or administers the system org, on a deployment with support access.
func (c Counter) IsStaff(ctx context.Context, userID uuid.UUID) (bool, error) {
	if !c.caps.SupportAccess {
		return false, nil
	}
	return c.store.IsStaff(ctx, userID)
}

// OrgName returns orgID's display name.
func (c Counter) OrgName(ctx context.Context, orgID uuid.UUID) (string, error) {
	o, err := c.store.ByID(ctx, orgID)
	if err != nil {
		return "", err
	}
	return o.Name, nil
}

// RoleOf returns userID's built-in role in orgID, or "" when userID is not a member, with the org's name. The role id is uuid.Nil until custom roles exist.
func (c Counter) RoleOf(ctx context.Context, orgID, userID uuid.UUID) (role string, roleID uuid.UUID, orgName string, err error) {
	r, name, err := c.store.MemberRole(ctx, orgID, userID)
	return string(r), uuid.Nil, name, err
}
