package fakes

import (
	"context"
	"sync"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/limits"
)

// Limits is an in-memory limits.Store.
type Limits struct {
	mu        sync.Mutex
	plans     map[uuid.UUID]string
	planRows  map[[2]string]int64
	overrides map[limits.Subject]map[limits.Key]int64
	names     map[uuid.UUID]string
	// Locked records every Lock call's subjects, in the order given.
	Locked [][]limits.Subject
}

// NewLimits returns an empty in-memory limits.Store.
func NewLimits() *Limits {
	return &Limits{
		plans:     map[uuid.UUID]string{},
		planRows:  map[[2]string]int64{},
		overrides: map[limits.Subject]map[limits.Key]int64{},
		names:     map[uuid.UUID]string{},
	}
}

// SetOrgName names an org for refusal messages.
func (f *Limits) SetOrgName(orgID uuid.UUID, name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.names[orgID] = name
}

func (f *Limits) OrgName(_ context.Context, orgID uuid.UUID) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.names[orgID], nil
}

var _ limits.Store = (*Limits)(nil)

// SetOrgPlan puts the org or project subject id on plan.
func (f *Limits) SetOrgPlan(subjectID uuid.UUID, plan string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.plans[subjectID] = plan
}

func (f *Limits) Resolve(_ context.Context, key limits.Key, sub limits.Subject) (value int64, ok bool, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if v, found := f.overrides[sub][key]; found {
		return v, true, nil
	}
	plan := limits.DefaultPlan
	if sub.Kind != limits.SubjectUser {
		if p, ok := f.plans[sub.ID]; ok {
			plan = p
		}
	}
	value, ok = f.planRows[[2]string{plan, string(key)}]
	return value, ok, nil
}

func (f *Limits) Lock(_ context.Context, subs []limits.Subject) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Locked = append(f.Locked, append([]limits.Subject(nil), subs...))
	return nil
}

func (f *Limits) SetPlan(_ context.Context, plan string, key limits.Key, value int64, _ uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.planRows[[2]string{plan, string(key)}] = value
	return nil
}

func (f *Limits) ClearPlan(_ context.Context, plan string, key limits.Key) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.planRows, [2]string{plan, string(key)})
	return nil
}

func (f *Limits) SetOverride(_ context.Context, sub limits.Subject, key limits.Key, value int64, _ uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.overrides[sub] == nil {
		f.overrides[sub] = map[limits.Key]int64{}
	}
	f.overrides[sub][key] = value
	return nil
}

func (f *Limits) DeleteOverride(_ context.Context, sub limits.Subject, key limits.Key) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.overrides[sub], key)
	return nil
}

func (f *Limits) ListPlan(_ context.Context, plan string) ([]limits.PlanEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []limits.PlanEntry
	for k, v := range f.planRows {
		if k[0] == plan {
			out = append(out, limits.PlanEntry{Plan: plan, Key: limits.Key(k[1]), Value: v})
		}
	}
	return out, nil
}

func (f *Limits) ListOverrides(_ context.Context) ([]limits.Override, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []limits.Override
	for sub, m := range f.overrides {
		for k, v := range m {
			out = append(out, limits.Override{Subject: sub, Key: k, Value: v})
		}
	}
	return out, nil
}
