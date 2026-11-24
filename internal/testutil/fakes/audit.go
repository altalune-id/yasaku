package fakes

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/platform/audit"
	"altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/platform/tenant"
)

// AuditRecord is one event the fake Audit saw, with whether it came inside a unit of work.
type AuditRecord struct {
	audit.Event
	OrgID uuid.UUID
	InTx  bool
}

// Audit is an in-memory audit recorder that, like the real one, refuses a Record outside a unit of work.
type Audit struct {
	mu      sync.Mutex
	records []AuditRecord
}

// Record keeps e when ctx carries a tenant scope and a unit of work.
func (a *Audit) Record(ctx context.Context, e audit.Event) error {
	tc, err := tenant.From(ctx)
	if err != nil {
		return err
	}
	_, inTx := db.CurrentTx(ctx)
	if !inTx {
		return &audit.NoUnitOfWorkError{Action: e.Action}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.records = append(a.records, AuditRecord{Event: e, OrgID: tc.OrgID, InTx: inTx})
	return nil
}

// Records returns a copy of every recorded event.
func (a *Audit) Records() []AuditRecord {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]AuditRecord(nil), a.records...)
}

// Actions returns the recorded actions in order.
func (a *Audit) Actions() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]string, 0, len(a.records))
	for _, r := range a.records {
		out = append(out, r.Action)
	}
	return out
}

// List returns the events recorded for the org on ctx, newest first.
func (a *Audit) List(ctx context.Context, f audit.Filter) ([]audit.Entry, error) {
	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []audit.Entry
	for i, r := range slices.Backward(a.records) {
		if r.OrgID != tc.OrgID {
			continue
		}
		out = append(out, audit.Entry{OrgID: r.OrgID, Action: r.Action, TargetKind: r.TargetKind, TargetID: r.TargetID,
			Reason: r.Reason, Detail: r.Detail, OccurredAt: time.Unix(int64(i), 0).UTC()})
		if f.Limit > 0 && len(out) == f.Limit {
			break
		}
	}
	return out, nil
}
