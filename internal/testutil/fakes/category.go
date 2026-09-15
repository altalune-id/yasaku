package fakes

import (
	"context"
	"sort"
	"sync"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/blog/category"
)

// Category is an in-memory category.Store.
type Category struct {
	mu   sync.Mutex
	rows map[uuid.UUID]*category.Category

	SaveFn   func(ctx context.Context, c *category.Category) error
	DeleteFn func(ctx context.Context, id uuid.UUID) error
	ListFn   func(ctx context.Context, orgID, projectID uuid.UUID) ([]*category.Category, error)
}

// NewCategory returns an empty in-memory category.Store.
func NewCategory() *Category {
	return &Category{rows: make(map[uuid.UUID]*category.Category)}
}

var _ category.Store = (*Category)(nil)

// Save upserts by ID; a slug collision inside the same project returns AlreadyExistsError.
func (r *Category) Save(ctx context.Context, c *category.Category) error {
	if r.SaveFn != nil {
		return r.SaveFn(ctx, c)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, existing := range r.rows {
		if id == c.ID {
			continue
		}
		if existing.ProjectID == c.ProjectID && existing.Slug == c.Slug {
			return &category.AlreadyExistsError{Slug: c.Slug}
		}
	}
	cp := *c
	r.rows[c.ID] = &cp
	return nil
}

// ByID returns the category with the given ID or a *NotFoundError.
func (r *Category) ByID(_ context.Context, id uuid.UUID) (*category.Category, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.rows[id]
	if !ok {
		return nil, &category.NotFoundError{ID: id.String()}
	}
	cp := *c
	return &cp, nil
}

// ByIDs returns the categories in scope keyed by ID; unknown ids are absent, not an error.
func (r *Category) ByIDs(_ context.Context, orgID, projectID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]*category.Category, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[uuid.UUID]*category.Category, len(ids))
	for _, id := range ids {
		c, ok := r.rows[id]
		if !ok || c.OrgID != orgID || c.ProjectID != projectID {
			continue
		}
		cp := *c
		out[id] = &cp
	}
	return out, nil
}

// List returns the categories in scope, newest first.
func (r *Category) List(ctx context.Context, orgID, projectID uuid.UUID) ([]*category.Category, error) {
	if r.ListFn != nil {
		return r.ListFn(ctx, orgID, projectID)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*category.Category, 0, len(r.rows))
	for _, c := range r.rows {
		if c.OrgID != orgID || c.ProjectID != projectID {
			continue
		}
		cp := *c
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID.String() > out[j].ID.String()
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}

// Delete removes the category or returns a *NotFoundError.
func (r *Category) Delete(ctx context.Context, id uuid.UUID) error {
	if r.DeleteFn != nil {
		return r.DeleteFn(ctx, id)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.rows[id]; !ok {
		return &category.NotFoundError{ID: id.String()}
	}
	delete(r.rows, id)
	return nil
}
