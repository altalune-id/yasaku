package fakes

import (
	"context"
	"slices"
	"strings"
	"sync"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/role"
)

// Role is an in-memory role.Store that honors ifVersion, the per-org name index and the held-role foreign key.
type Role struct {
	mu   sync.Mutex
	rows map[uuid.UUID]*role.Role
	held map[uuid.UUID]bool
}

// NewRole returns an empty in-memory role.Store.
func NewRole() *Role {
	return &Role{rows: map[uuid.UUID]*role.Role{}, held: map[uuid.UUID]bool{}}
}

var _ role.Store = (*Role)(nil)

// Hold marks id as held by a member, so Delete refuses it.
func (f *Role) Hold(id uuid.UUID) { f.mu.Lock(); f.held[id] = true; f.mu.Unlock() }

// Release clears a Hold.
func (f *Role) Release(id uuid.UUID) { f.mu.Lock(); delete(f.held, id); f.mu.Unlock() }

// Save inserts or conditionally updates r in the org on ctx.
func (f *Role) Save(ctx context.Context, r *role.Role, ifVersion int64) error {
	tc, err := tenant.From(ctx)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, other := range f.rows {
		if id != r.ID && other.OrgID == tc.OrgID && strings.EqualFold(other.Name, r.Name) {
			return &role.AlreadyExistsError{Name: r.Name}
		}
	}
	c := *r
	c.OrgID = tc.OrgID
	c.Permissions = slices.Clone(r.Permissions)
	cur, ok := f.rows[r.ID]
	if ok {
		if cur.OrgID != tc.OrgID {
			return &role.NotFoundError{ID: r.ID.String()}
		}
		if ifVersion != 0 && cur.Version != ifVersion {
			return &role.StaleVersionError{Want: ifVersion, Got: cur.Version}
		}
		c.Version = cur.Version + 1
		c.CreatedAt = cur.CreatedAt
	}
	f.rows[r.ID] = &c
	return nil
}

// ByID returns the role in the org on ctx.
func (f *Role) ByID(ctx context.Context, id uuid.UUID) (*role.Role, error) {
	r, err := f.find(ctx, id)
	if err != nil {
		return nil, err
	}
	c := *r
	c.Permissions = slices.Clone(r.Permissions)
	return &c, nil
}

// List returns the org's roles by name.
func (f *Role) List(ctx context.Context) ([]*role.Role, error) {
	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*role.Role
	for _, r := range f.rows {
		if r.OrgID == tc.OrgID {
			c := *r
			c.Permissions = slices.Clone(r.Permissions)
			out = append(out, &c)
		}
	}
	slices.SortFunc(out, func(a, b *role.Role) int { return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) })
	return out, nil
}

// Delete removes the role unless it is held.
func (f *Role) Delete(ctx context.Context, id uuid.UUID) error {
	if _, err := f.find(ctx, id); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.held[id] {
		return &role.InUseError{ID: id.String()}
	}
	delete(f.rows, id)
	return nil
}

// HeldBy reports 1 for every role in the org on ctx marked by Hold.
func (f *Role) HeldBy(ctx context.Context) (map[uuid.UUID]int, error) {
	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[uuid.UUID]int{}
	for id := range f.held {
		if r, ok := f.rows[id]; ok && r.OrgID == tc.OrgID {
			out[id] = 1
		}
	}
	return out, nil
}

// Permissions returns the permissions of roleID in the org on ctx.
func (f *Role) Permissions(ctx context.Context, roleID uuid.UUID) ([]role.Permission, error) {
	r, err := f.find(ctx, roleID)
	if err != nil {
		return nil, err
	}
	return slices.Clone(r.Permissions), nil
}

func (f *Role) find(ctx context.Context, id uuid.UUID) (*role.Role, error) {
	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rows[id]
	if !ok || r.OrgID != tc.OrgID {
		return nil, &role.NotFoundError{ID: id.String()}
	}
	return r, nil
}
