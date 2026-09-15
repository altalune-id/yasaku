package fakes

import (
	"context"
	"sort"
	"sync"

	"github.com/google/uuid"

	"altalune.id/yasaku/civil"
	"altalune.id/yasaku/internal/period"
)

// Period is an in-memory period.Store.
type Period struct {
	mu       sync.Mutex
	data     map[uuid.UUID]*period.Period
	closings []*period.Closing

	SaveFn func(ctx context.Context, p *period.Period) error
}

// NewPeriod returns an empty in-memory period.Store.
func NewPeriod() *Period { return &Period{data: map[uuid.UUID]*period.Period{}} }

var _ period.Store = (*Period)(nil)

func clonePeriod(p *period.Period) *period.Period {
	cp := *p
	if p.EndDate != nil {
		d := *p.EndDate
		cp.EndDate = &d
	}
	if p.ClosedAt != nil {
		t := *p.ClosedAt
		cp.ClosedAt = &t
	}
	if p.Snapshot != nil {
		s := *p.Snapshot
		cp.Snapshot = &s
	}
	return &cp
}

// Save stores p, refusing a second current period in the same project the way the partial unique index does.
func (f *Period) Save(ctx context.Context, p *period.Period) error {
	if f.SaveFn != nil {
		return f.SaveFn(ctx, p)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if p.IsCurrent() {
		for _, other := range f.data {
			if other.ID != p.ID && other.ProjectID == p.ProjectID && other.IsCurrent() {
				return &period.OverlapError{Start: p.StartDate.String()}
			}
		}
	}
	f.data[p.ID] = clonePeriod(p)
	return nil
}

// ByID looks a period up by id alone. NOTE: it deliberately does not filter by scope, so the service's own check is what the scope tests exercise.
func (f *Period) ByID(_ context.Context, id uuid.UUID) (*period.Period, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.data[id]
	if !ok {
		return nil, &period.NotFoundError{ID: id.String()}
	}
	return clonePeriod(p), nil
}

func (f *Period) List(_ context.Context, orgID, projectID uuid.UUID, opts period.ListOpts) ([]*period.Period, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*period.Period, 0, len(f.data))
	for _, p := range f.data {
		if p.OrgID != orgID || p.ProjectID != projectID {
			continue
		}
		if opts.Before != nil && !p.StartDate.Before(*opts.Before) {
			continue
		}
		out = append(out, clonePeriod(p))
	}
	sort.Slice(out, func(i, j int) bool {
		if c := out[i].StartDate.Compare(out[j].StartDate); c != 0 {
			return c > 0
		}
		return out[i].ID.String() > out[j].ID.String()
	})
	if opts.Limit > 0 && len(out) > opts.Limit {
		out = out[:opts.Limit]
	}
	return out, nil
}

func (f *Period) Current(_ context.Context, orgID, projectID uuid.UUID) (*period.Period, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range f.data {
		if p.OrgID == orgID && p.ProjectID == projectID && p.IsCurrent() {
			return clonePeriod(p), nil
		}
	}
	return nil, &period.NotFoundError{ID: projectID.String()}
}

func (f *Period) Containing(_ context.Context, orgID, projectID uuid.UUID, d civil.Date) (*period.Period, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range f.data {
		if p.OrgID == orgID && p.ProjectID == projectID && p.Contains(d) {
			return clonePeriod(p), nil
		}
	}
	return nil, &period.NotFoundError{ID: d.String()}
}

func (f *Period) Neighbors(ctx context.Context, orgID, projectID, id uuid.UUID) (prev, next *period.Period, err error) { //nolint:nonamedreturns // mirrors the Store signature
	all, err := f.List(ctx, orgID, projectID, period.ListOpts{})
	if err != nil {
		return nil, nil, err
	}
	var self *period.Period
	for _, p := range all {
		if p.ID == id {
			self = p
			break
		}
	}
	if self == nil {
		return nil, nil, &period.NotFoundError{ID: id.String()}
	}
	for _, p := range all {
		if p.ID == id {
			continue
		}
		if p.StartDate.Before(self.StartDate) {
			if prev == nil || prev.StartDate.Before(p.StartDate) {
				prev = p
			}
			continue
		}
		if self.StartDate.Before(p.StartDate) {
			if next == nil || p.StartDate.Before(next.StartDate) {
				next = p
			}
		}
	}
	return prev, next, nil
}

func (f *Period) SaveClosing(_ context.Context, c *period.Closing) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *c
	f.closings = append(f.closings, &cp)
	return nil
}

func (f *Period) ListClosings(_ context.Context, orgID, projectID, periodID uuid.UUID) ([]*period.Closing, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*period.Closing, 0, len(f.closings))
	for _, c := range f.closings {
		if c.OrgID != orgID || c.ProjectID != projectID || c.PeriodID != periodID {
			continue
		}
		cp := *c
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].ClosedAt.Equal(out[j].ClosedAt) {
			return out[i].ClosedAt.After(out[j].ClosedAt)
		}
		return out[i].ID.String() > out[j].ID.String()
	})
	return out, nil
}
