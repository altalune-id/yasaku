package fakes

import (
	"bytes"
	"context"
	"sync"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform/db"
)

// Org is an in-memory org.Store.
type Org struct {
	mu          sync.Mutex
	orgs        map[uuid.UUID]*org.Org
	memberships []*org.Membership
	userLookup  map[uuid.UUID]func() (string, string)
	takenSlugs  int
	// MembershipOfFailAfter makes every MembershipOf call after the first N return MembershipOfErr.
	MembershipOfFailAfter int
	MembershipOfErr       error
	membershipOfCalls     int
	// SaveMembershipErr makes every SaveMembership call fail with it.
	SaveMembershipErr error
	// DeferWritesToCommit applies Save and SaveMembership only when the unit of work on ctx commits, so a rollback leaves no row.
	DeferWritesToCommit bool
}

// NewOrg returns an empty in-memory org.Store.
func NewOrg() *Org {
	return &Org{
		orgs:       make(map[uuid.UUID]*org.Org),
		userLookup: map[uuid.UUID]func() (string, string){},
	}
}

var _ org.Store = (*Org)(nil)

func (r *Org) Save(ctx context.Context, o *org.Org) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, existing := range r.orgs {
		if existing.Slug == o.Slug && id != o.ID {
			return &org.AlreadyExistsError{Slug: o.Slug}
		}
		if o.System && existing.System && id != o.ID {
			return &org.SystemOrgExistsError{Slug: o.Slug}
		}
	}
	c := *o
	r.atCommit(ctx, func() { r.orgs[o.ID] = &c })
	return nil
}

func (r *Org) atCommit(ctx context.Context, apply func()) {
	if _, ok := db.CurrentTx(ctx); !ok || !r.DeferWritesToCommit {
		apply()
		return
	}
	db.AfterCommit(ctx, func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		apply()
	})
}

// TakeNextSlugs makes the next n BySlug lookups report the slug as taken, without storing a row.
func (r *Org) TakeNextSlugs(n int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.takenSlugs = n
}

func (r *Org) BySlug(_ context.Context, slug string) (*org.Org, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.takenSlugs > 0 {
		r.takenSlugs--
		return &org.Org{ID: uuid.New(), Slug: slug, Name: slug}, nil
	}
	for _, o := range r.orgs {
		if o.Slug == slug {
			c := *o
			return &c, nil
		}
	}
	return nil, &org.NotFoundError{Slug: slug}
}

// SystemOrg mirrors the real stores: the earliest system org by created_at, then id.
func (r *Org) SystemOrg(_ context.Context) (*org.Org, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var found *org.Org
	for _, o := range r.orgs {
		if !o.System {
			continue
		}
		if found == nil || o.CreatedAt.Before(found.CreatedAt) ||
			(o.CreatedAt.Equal(found.CreatedAt) && bytes.Compare(o.ID[:], found.ID[:]) < 0) {
			found = o
		}
	}
	if found == nil {
		return nil, &org.NotFoundError{System: true}
	}
	c := *found
	return &c, nil
}

func (r *Org) ByID(_ context.Context, id uuid.UUID) (*org.Org, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	o, ok := r.orgs[id]
	if !ok {
		return nil, &org.NotFoundError{ID: id.String()}
	}
	c := *o
	return &c, nil
}

func (r *Org) List(_ context.Context, userID uuid.UUID) ([]*org.Org, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*org.Org
	for _, m := range r.memberships {
		if m.UserID == userID {
			if o, ok := r.orgs[m.OrgID]; ok {
				c := *o
				out = append(out, &c)
			}
		}
	}
	return out, nil
}

func (r *Org) CountCreatedBy(_ context.Context, userID uuid.UUID) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var n int64
	for _, o := range r.orgs {
		if o.OwnerID == userID && !o.System {
			n++
		}
	}
	return n, nil
}

// IsStaff mirrors the stores: an owner or admin of the earliest system org.
func (r *Org) IsStaff(ctx context.Context, userID uuid.UUID) (bool, error) {
	sys, err := r.SystemOrg(ctx)
	if err != nil {
		return false, nil //nolint:nilerr // no system org means nobody is staff
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, m := range r.memberships {
		if m.OrgID == sys.ID && m.UserID == userID && (m.Role == org.RoleOwner || m.Role == org.RoleAdmin) {
			return true, nil
		}
	}
	return false, nil
}

func (r *Org) SaveMembership(ctx context.Context, m *org.Membership) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.SaveMembershipErr != nil {
		return r.SaveMembershipErr
	}
	c := *m
	r.atCommit(ctx, func() {
		for i, x := range r.memberships {
			if x.OrgID == m.OrgID && x.UserID == m.UserID {
				r.memberships[i] = &c
				return
			}
		}
		r.memberships = append(r.memberships, &c)
	})
	return nil
}

// LockOrg is a no-op; fakes.UnitOfWork runs one call at a time.
func (r *Org) LockOrg(context.Context, uuid.UUID) error { return nil }

// MemberRole returns userID's role in orgID and the org's name, as the real stores join them; it shares MembershipOf's call count and injected failure.
func (r *Org) MemberRole(_ context.Context, orgID, userID uuid.UUID) (org.Role, string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.membershipOfCalls++
	if r.MembershipOfErr != nil && r.membershipOfCalls > r.MembershipOfFailAfter {
		return "", "", r.MembershipOfErr
	}
	name := ""
	if o, ok := r.orgs[orgID]; ok {
		name = o.Name
	}
	for _, x := range r.memberships {
		if x.OrgID == orgID && x.UserID == userID {
			return x.Role, name, nil
		}
	}
	return "", name, nil
}

func (r *Org) MembershipOf(_ context.Context, orgID, userID uuid.UUID) (*org.Membership, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.membershipOfCalls++
	if r.MembershipOfErr != nil && r.membershipOfCalls > r.MembershipOfFailAfter {
		return nil, r.MembershipOfErr
	}
	for _, x := range r.memberships {
		if x.OrgID == orgID && x.UserID == userID {
			c := *x
			return &c, nil
		}
	}
	return nil, &org.MembershipMissingError{OrgID: orgID.String(), UserID: userID.String()}
}

// MembershipOfCalls reports how many times MembershipOf ran.
func (r *Org) MembershipOfCalls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.membershipOfCalls
}

func (r *Org) ListMembers(_ context.Context, orgID uuid.UUID) ([]*org.Membership, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*org.Membership
	for _, x := range r.memberships {
		if x.OrgID == orgID {
			c := *x
			out = append(out, &c)
		}
	}
	return out, nil
}

func (r *Org) ListMemberProfiles(_ context.Context, orgID uuid.UUID) ([]*org.MemberProfile, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*org.MemberProfile, 0, len(r.memberships))
	for _, x := range r.memberships {
		if x.OrgID != orgID {
			continue
		}
		var email, name string
		if lookup, ok := r.userLookup[x.UserID]; ok {
			email, name = lookup()
		}
		out = append(out, &org.MemberProfile{
			UserID:    x.UserID,
			Email:     email,
			Name:      name,
			Role:      x.Role,
			CreatedAt: x.CreatedAt,
			System:    x.System,
		})
	}
	return out, nil
}

// SetUser registers an email/name for a user id, so ListMemberProfiles can join.
func (r *Org) SetUser(id uuid.UUID, email, name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.userLookup == nil {
		r.userLookup = map[uuid.UUID]func() (string, string){}
	}
	r.userLookup[id] = func() (string, string) { return email, name }
}

func (r *Org) RemoveMember(_ context.Context, orgID, userID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, x := range r.memberships {
		if x.OrgID == orgID && x.UserID == userID {
			r.memberships = append(r.memberships[:i], r.memberships[i+1:]...)
			return nil
		}
	}
	return &org.MembershipMissingError{OrgID: orgID.String(), UserID: userID.String()}
}
