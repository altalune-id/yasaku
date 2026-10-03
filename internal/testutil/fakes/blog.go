package fakes

import (
	"context"
	"slices"
	"sort"
	"sync"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/blog"
)

// Blog is an in-memory blog.Store.
type Blog struct {
	mu   sync.Mutex
	data map[uuid.UUID]*blog.Post

	SaveFn   func(ctx context.Context, p *blog.Post, ifVersion int) error
	ByIDFn   func(ctx context.Context, id uuid.UUID) (*blog.Post, error)
	BySlugFn func(ctx context.Context, projectID uuid.UUID, slug string) (*blog.Post, error)
	ListFn   func(ctx context.Context, orgID, projectID uuid.UUID, opts blog.ListOpts) ([]*blog.Post, error)
	DeleteFn func(ctx context.Context, id uuid.UUID, ifVersion int) error
}

// NewBlog returns an empty in-memory blog.Store.
func NewBlog() *Blog { return &Blog{data: map[uuid.UUID]*blog.Post{}} }

var _ blog.Store = (*Blog)(nil)

// Seed inserts p without going through Save, bypassing the uniqueness check.
func (f *Blog) Seed(p *blog.Post) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.data[p.ID] = clonePost(p)
}

// Len reports how many posts are stored.
func (f *Blog) Len() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.data)
}

func (f *Blog) Save(ctx context.Context, p *blog.Post, ifVersion int) error {
	if f.SaveFn != nil {
		return f.SaveFn(ctx, p, ifVersion)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, existing := range f.data {
		if id != p.ID && existing.ProjectID == p.ProjectID && existing.Slug == p.Slug {
			return &blog.AlreadyExistsError{Slug: p.Slug}
		}
	}
	stored := clonePost(p)
	// NOTE: both real stores insert with the caller's version and, on conflict, set version = version + 1.
	if existing, ok := f.data[p.ID]; ok {
		if ifVersion != 0 && existing.Version != ifVersion {
			return &blog.StaleVersionError{Want: ifVersion, Got: existing.Version}
		}
		stored.Version = existing.Version + 1
	}
	f.data[p.ID] = stored
	return nil
}

func (f *Blog) ByID(ctx context.Context, id uuid.UUID) (*blog.Post, error) {
	if f.ByIDFn != nil {
		return f.ByIDFn(ctx, id)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.data[id]
	if !ok {
		return nil, &blog.NotFoundError{ID: id.String()}
	}
	return clonePost(p), nil
}

func (f *Blog) BySlug(ctx context.Context, projectID uuid.UUID, slug string) (*blog.Post, error) {
	if f.BySlugFn != nil {
		return f.BySlugFn(ctx, projectID, slug)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range f.data {
		if p.ProjectID == projectID && p.Slug == slug {
			return clonePost(p), nil
		}
	}
	return nil, &blog.NotFoundError{ID: slug}
}

func (f *Blog) List(ctx context.Context, orgID, projectID uuid.UUID, opts blog.ListOpts) ([]*blog.Post, error) {
	if f.ListFn != nil {
		return f.ListFn(ctx, orgID, projectID, opts)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*blog.Post, 0, len(f.data))
	for _, p := range f.data {
		if p.OrgID != orgID || p.ProjectID != projectID {
			continue
		}
		if opts.Status != nil && p.Status != *opts.Status {
			continue
		}
		if opts.CategoryID != nil && p.CategoryID != *opts.CategoryID {
			continue
		}
		out = append(out, clonePost(p))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID.String() > out[j].ID.String()
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}

func (f *Blog) Delete(ctx context.Context, id uuid.UUID, ifVersion int) error {
	if f.DeleteFn != nil {
		return f.DeleteFn(ctx, id, ifVersion)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	existing, ok := f.data[id]
	if !ok {
		return &blog.NotFoundError{ID: id.String()}
	}
	if ifVersion != 0 && existing.Version != ifVersion {
		return &blog.StaleVersionError{Want: ifVersion, Got: existing.Version}
	}
	delete(f.data, id)
	return nil
}

func (f *Blog) CountByCategory(_ context.Context, orgID, projectID uuid.UUID) (map[uuid.UUID]int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[uuid.UUID]int{}
	for _, p := range f.data {
		if p.OrgID != orgID || p.ProjectID != projectID {
			continue
		}
		out[p.CategoryID]++
	}
	return out, nil
}

func (f *Blog) CountByTag(_ context.Context, orgID, projectID uuid.UUID) (map[uuid.UUID]int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[uuid.UUID]int{}
	for _, p := range f.data {
		if p.OrgID != orgID || p.ProjectID != projectID {
			continue
		}
		for _, id := range p.TagIDs {
			out[id]++
		}
	}
	return out, nil
}

func clonePost(p *blog.Post) *blog.Post {
	cp := *p
	cp.TagIDs = slices.Clone(p.TagIDs)
	if p.FirstPublishedAt != nil {
		t := *p.FirstPublishedAt
		cp.FirstPublishedAt = &t
	}
	return &cp
}
