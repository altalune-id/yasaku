package fakes

import (
	"cmp"
	"context"
	"slices"
	"sync"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/category"
)

// TxCategory is an in-memory category.Store for the transaction categories module.
type TxCategory struct {
	mu   sync.Mutex
	rows map[uuid.UUID]*category.Category

	SaveFn   func(ctx context.Context, c *category.Category) error
	DeleteFn func(ctx context.Context, id uuid.UUID) error
	ListFn   func(ctx context.Context, orgID, projectID uuid.UUID, opts category.ListOpts) ([]*category.Category, error)
}

// NewTxCategory returns an empty in-memory category.Store.
func NewTxCategory() *TxCategory {
	return &TxCategory{rows: make(map[uuid.UUID]*category.Category)}
}

var _ category.Store = (*TxCategory)(nil)

// Save upserts by ID and emulates the partial unique index on (project_id, kind, lower(name)) WHERE archived_at IS NULL.
func (f *TxCategory) Save(ctx context.Context, c *category.Category) error {
	if f.SaveFn != nil {
		return f.SaveFn(ctx, c)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !c.IsArchived() {
		for _, row := range f.rows {
			if row.ID == c.ID || row.IsArchived() {
				continue
			}
			if row.ProjectID == c.ProjectID && row.Kind == c.Kind &&
				category.FoldName(row.Name) == category.FoldName(c.Name) {
				return &category.AlreadyExistsError{Name: c.Name, Kind: c.Kind}
			}
		}
	}
	cp := *c
	f.rows[c.ID] = &cp
	return nil
}

// ByID returns the row by id WITHOUT any tenant filtering, so a service's own scope check is what tests exercise.
func (f *TxCategory) ByID(_ context.Context, id uuid.UUID) (*category.Category, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.rows[id]
	if !ok {
		return nil, &category.NotFoundError{ID: id.String()}
	}
	cp := *c
	return &cp, nil
}

// ByIDs returns the rows in scope keyed by id; unknown ids are absent.
func (f *TxCategory) ByIDs(_ context.Context, orgID, projectID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]*category.Category, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make(map[uuid.UUID]*category.Category, len(ids))
	for _, id := range ids {
		c, ok := f.rows[id]
		if !ok || c.OrgID != orgID || c.ProjectID != projectID {
			continue
		}
		cp := *c
		out[id] = &cp
	}
	return out, nil
}

// List returns the rows in scope ordered by sort order, then name, then id.
func (f *TxCategory) List(ctx context.Context, orgID, projectID uuid.UUID, opts category.ListOpts) ([]*category.Category, error) {
	if f.ListFn != nil {
		return f.ListFn(ctx, orgID, projectID, opts)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*category.Category, 0, len(f.rows))
	for _, c := range f.rows {
		if c.OrgID != orgID || c.ProjectID != projectID {
			continue
		}
		if opts.Kind != "" && c.Kind != opts.Kind {
			continue
		}
		if !opts.IncludeArchived && c.IsArchived() {
			continue
		}
		cp := *c
		out = append(out, &cp)
	}
	slices.SortFunc(out, func(a, b *category.Category) int {
		return cmp.Or(
			cmp.Compare(a.SortOrder, b.SortOrder),
			cmp.Compare(a.Name, b.Name),
			cmp.Compare(a.ID.String(), b.ID.String()),
		)
	})
	return out, nil
}

// Delete removes the row by id.
func (f *TxCategory) Delete(ctx context.Context, id uuid.UUID) error {
	if f.DeleteFn != nil {
		return f.DeleteFn(ctx, id)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.rows[id]; !ok {
		return &category.NotFoundError{ID: id.String()}
	}
	delete(f.rows, id)
	return nil
}
