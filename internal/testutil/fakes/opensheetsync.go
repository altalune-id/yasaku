package fakes

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/opensheetsync"
)

var _ opensheetsync.Store = (*OpensheetSync)(nil)

type osStateKey struct {
	org    uuid.UUID
	entity opensheetsync.Entity
	id     uuid.UUID
}

type osState struct {
	opensheetsync.State
	org         uuid.UUID
	leaseToken  uuid.UUID
	leasedUntil time.Time
	retryAfter  time.Time
}

type osTx struct {
	org, project, id           uuid.UUID
	wallet, toWallet, category uuid.UUID
}

// OpensheetSync is an in-memory opensheetsync.Store with the real stores' contract: links upsert by project behind an org guard, state rows version-bump, lease and settle by compare-and-set.
type OpensheetSync struct {
	mu       sync.Mutex
	links    map[uuid.UUID]opensheetsync.Link
	state    map[osStateKey]*osState
	entities map[[2]uuid.UUID]map[opensheetsync.Entity][]uuid.UUID
	txs      []osTx
	calls    []string
	MarkErr  error
}

// NewOpensheetSync returns an empty store.
func NewOpensheetSync() *OpensheetSync {
	return &OpensheetSync{
		links:    map[uuid.UUID]opensheetsync.Link{},
		state:    map[osStateKey]*osState{},
		entities: map[[2]uuid.UUID]map[opensheetsync.Entity][]uuid.UUID{},
	}
}

// SeedEntity records that id exists in the project, standing in for the wallets, categories and transactions tables MarkAll reads.
func (f *OpensheetSync) SeedEntity(orgID, projectID uuid.UUID, e opensheetsync.Entity, id uuid.UUID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := [2]uuid.UUID{orgID, projectID}
	if f.entities[k] == nil {
		f.entities[k] = map[opensheetsync.Entity][]uuid.UUID{}
	}
	f.entities[k][e] = append(f.entities[k][e], id)
}

// SeedTransaction records a transaction naming its wallet, to wallet and category, in that order (uuid.Nil or left out for none), standing in for what MarkReferencing reads.
func (f *OpensheetSync) SeedTransaction(orgID, projectID, id uuid.UUID, refs ...uuid.UUID) {
	f.SeedEntity(orgID, projectID, opensheetsync.EntityTransaction, id)
	refs = append(slices.Clone(refs), uuid.Nil, uuid.Nil, uuid.Nil)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.txs = append(f.txs, osTx{org: orgID, project: projectID, id: id, wallet: refs[0], toWallet: refs[1], category: refs[2]})
}

// DropEntity forgets that id exists in the project, standing in for a hard delete of the domain row.
func (f *OpensheetSync) DropEntity(orgID, projectID uuid.UUID, e opensheetsync.Entity, id uuid.UUID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := [2]uuid.UUID{orgID, projectID}
	if byEntity := f.entities[k]; byEntity != nil {
		byEntity[e] = slices.DeleteFunc(byEntity[e], func(x uuid.UUID) bool { return x == id })
	}
	if e == opensheetsync.EntityTransaction {
		f.txs = slices.DeleteFunc(f.txs, func(tx osTx) bool { return tx.org == orgID && tx.id == id })
	}
}

// Calls returns the Store methods called so far, in order.
func (f *OpensheetSync) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func (f *OpensheetSync) hasLink(orgID, projectID uuid.UUID) bool {
	l, ok := f.links[projectID]
	return ok && l.OrgID == orgID
}

func noLinkError(projectID uuid.UUID) error {
	return fmt.Errorf("fakes.OpensheetSync: opensheet_sync_state references no link for project %s", projectID)
}

// State returns one state row as the store holds it.
func (f *OpensheetSync) State(orgID uuid.UUID, e opensheetsync.Entity, id uuid.UUID) (opensheetsync.State, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	st, ok := f.state[osStateKey{org: orgID, entity: e, id: id}]
	if !ok {
		return opensheetsync.State{}, false
	}
	return st.State, true
}

func (f *OpensheetSync) SaveLink(_ context.Context, l *opensheetsync.Link) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "SaveLink")
	if prior, ok := f.links[l.ProjectID]; ok && prior.OrgID != l.OrgID {
		return &opensheetsync.LinkNotFoundError{ProjectID: l.ProjectID.String()}
	}
	cp := *l
	if prior, ok := f.links[l.ProjectID]; ok {
		cp.ID, cp.CreatedAt, cp.CreatedBy, cp.CreatedByKeyID, cp.LastSyncedAt = prior.ID, prior.CreatedAt, prior.CreatedBy, prior.CreatedByKeyID, prior.LastSyncedAt
	}
	cp.APIKeySealed = slices.Clone(l.APIKeySealed)
	f.links[l.ProjectID] = cp
	return nil
}

func (f *OpensheetSync) LinkByProject(_ context.Context, orgID, projectID uuid.UUID) (*opensheetsync.Link, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "LinkByProject")
	l, ok := f.links[projectID]
	if !ok || l.OrgID != orgID {
		return nil, &opensheetsync.LinkNotFoundError{ProjectID: projectID.String()}
	}
	l.APIKeySealed = slices.Clone(l.APIKeySealed)
	return &l, nil
}

func (f *OpensheetSync) LinkEnabled(_ context.Context, orgID, projectID uuid.UUID) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "LinkEnabled")
	l, ok := f.links[projectID]
	return ok && l.OrgID == orgID && l.Enabled, nil
}

func (f *OpensheetSync) ListEnabledLinks(_ context.Context, orgID uuid.UUID) ([]*opensheetsync.Link, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "ListEnabledLinks")
	var out []*opensheetsync.Link
	for _, l := range f.links {
		if l.OrgID == orgID && l.Enabled {
			cp := l
			out = append(out, &cp)
		}
	}
	slices.SortFunc(out, func(a, b *opensheetsync.Link) int {
		return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), cmp.Compare(a.ID.String(), b.ID.String()))
	})
	return out, nil
}

func (f *OpensheetSync) DeleteLink(_ context.Context, orgID, projectID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "DeleteLink")
	l, ok := f.links[projectID]
	if !ok || l.OrgID != orgID {
		return &opensheetsync.LinkNotFoundError{ProjectID: projectID.String()}
	}
	delete(f.links, projectID)
	for k, st := range f.state {
		if st.org == orgID && st.ProjectID == projectID {
			delete(f.state, k)
		}
	}
	return nil
}

func (f *OpensheetSync) SaveOutcome(_ context.Context, orgID, projectID uuid.UUID, o opensheetsync.Outcome) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "SaveOutcome")
	l, ok := f.links[projectID]
	if !ok || l.OrgID != orgID || !l.UpdatedAt.Equal(o.LinkUpdatedAt) {
		return false, nil
	}
	at := o.At.UTC()
	if o.Err == "" {
		l.FailureStreak, l.LastError, l.LastSyncedAt = 0, "", &at
		f.links[projectID] = l
		return false, nil
	}
	l.FailureStreak++
	l.LastError = o.Err
	disabled := false
	if l.FailureStreak >= opensheetsync.DisableAfter && l.Enabled {
		l.Enabled, l.VerifiedAt, l.AutoDisabledAt, disabled = false, nil, &at, true
	}
	f.links[projectID] = l
	return disabled, nil
}

func (f *OpensheetSync) mark(orgID, projectID uuid.UUID, r opensheetsync.Ref, at time.Time) {
	k := osStateKey{org: orgID, entity: r.Entity, id: r.ID}
	st, ok := f.state[k]
	if !ok {
		f.state[k] = &osState{org: orgID, State: opensheetsync.State{
			Ref: opensheetsync.Ref{Entity: r.Entity, ID: r.ID, Deleted: r.Deleted}, ProjectID: projectID, Version: 1, UpdatedAt: at.UTC(),
		}}
		return
	}
	st.Version++
	st.Attempts = 0
	st.retryAfter = time.Time{}
	st.UpdatedAt = at.UTC()
	st.Deleted = st.Deleted || r.Deleted
}

func (f *OpensheetSync) Mark(_ context.Context, orgID, projectID uuid.UUID, refs []opensheetsync.Ref, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "Mark")
	if f.MarkErr != nil {
		return f.MarkErr
	}
	if len(refs) > 0 && !f.hasLink(orgID, projectID) {
		return noLinkError(projectID)
	}
	for _, r := range opensheetsync.LockOrder(refs) {
		f.mark(orgID, projectID, r, at)
	}
	return nil
}

func (f *OpensheetSync) MarkReferencing(_ context.Context, orgID, projectID uuid.UUID, ref opensheetsync.Ref, at time.Time) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "MarkReferencing")
	var ids []uuid.UUID
	for _, tx := range f.txs {
		if tx.org != orgID || tx.project != projectID || !tx.names(ref) {
			continue
		}
		ids = append(ids, tx.id)
	}
	if len(ids) > 0 && !f.hasLink(orgID, projectID) {
		return 0, noLinkError(projectID)
	}
	for _, id := range ids {
		f.mark(orgID, projectID, opensheetsync.Ref{Entity: opensheetsync.EntityTransaction, ID: id}, at)
	}
	return int64(len(ids)), nil
}

func (tx osTx) names(ref opensheetsync.Ref) bool {
	switch ref.Entity {
	case opensheetsync.EntityWallet:
		return tx.wallet == ref.ID || tx.toWallet == ref.ID
	case opensheetsync.EntityCategory:
		return tx.category == ref.ID
	}
	return false
}

func (f *OpensheetSync) MarkAll(_ context.Context, orgID, projectID uuid.UUID, at time.Time) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "MarkAll")
	byEntity := f.entities[[2]uuid.UUID{orgID, projectID}]
	var n int64
	for _, ids := range byEntity {
		n += int64(len(ids))
	}
	if n > 0 && !f.hasLink(orgID, projectID) {
		return 0, noLinkError(projectID)
	}
	for e, ids := range byEntity {
		for _, id := range ids {
			f.mark(orgID, projectID, opensheetsync.Ref{Entity: e, ID: id}, at)
		}
	}
	for _, st := range f.state {
		if st.org == orgID && st.ProjectID == projectID && !f.exists(orgID, st.Entity, st.ID) {
			f.mark(orgID, projectID, opensheetsync.Ref{Entity: st.Entity, ID: st.ID, Deleted: true}, at)
			n++
		}
	}
	return n, nil
}

func (f *OpensheetSync) exists(orgID uuid.UUID, e opensheetsync.Entity, id uuid.UUID) bool {
	for k, byEntity := range f.entities {
		if k[0] == orgID && slices.Contains(byEntity[e], id) {
			return true
		}
	}
	return false
}

func (f *OpensheetSync) row(orgID, projectID uuid.UUID, ref opensheetsync.Ref) (*osState, bool) {
	st, ok := f.state[osStateKey{org: orgID, entity: ref.Entity, id: ref.ID}]
	if !ok || st.ProjectID != projectID {
		return nil, false
	}
	return st, true
}

func (st *osState) claimable(at time.Time) bool {
	return st.Dirty() && st.Attempts < opensheetsync.MaxRowAttempts &&
		(st.leasedUntil.IsZero() || st.leasedUntil.Before(at)) && (st.retryAfter.IsZero() || st.retryAfter.Before(at))
}

func (f *OpensheetSync) Claim(_ context.Context, orgID, projectID uuid.UUID, ref opensheetsync.Ref, token uuid.UUID, at time.Time, ttl time.Duration) (opensheetsync.State, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "Claim")
	st, ok := f.row(orgID, projectID, ref)
	if !ok || !st.claimable(at) {
		return opensheetsync.State{}, false, nil
	}
	st.leaseToken, st.leasedUntil = token, at.Add(ttl)
	return st.State, true, nil
}

func (f *OpensheetSync) Settle(_ context.Context, orgID, projectID uuid.UUID, ref opensheetsync.Ref, version int64, token uuid.UUID) (settled, dirty bool, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "Settle")
	st, ok := f.row(orgID, projectID, ref)
	if !ok || st.leaseToken != token {
		return false, false, nil
	}
	st.SyncedVersion, st.Attempts, st.LastError = version, 0, ""
	st.leaseToken, st.leasedUntil, st.retryAfter = uuid.Nil, time.Time{}, time.Time{}
	return true, st.Version > version, nil
}

func (f *OpensheetSync) Release(_ context.Context, orgID, projectID uuid.UUID, ref opensheetsync.Ref, token uuid.UUID, fl opensheetsync.Failure) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "Release")
	st, ok := f.row(orgID, projectID, ref)
	if !ok || st.leaseToken != token {
		return nil
	}
	st.LastError = fl.Reason
	st.leaseToken, st.leasedUntil = uuid.Nil, time.Time{}
	if fl.Version == 0 || fl.Version == st.Version {
		if fl.Count {
			st.Attempts++
		}
		st.retryAfter = fl.RetryAfter
	}
	st.UpdatedAt = fl.At.UTC()
	return nil
}

func (f *OpensheetSync) ListDirty(_ context.Context, orgID, projectID uuid.UUID, cutoff, at time.Time, limit int) ([]opensheetsync.Ref, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "ListDirty")
	var rows []*osState
	for _, st := range f.state {
		if st.org == orgID && st.ProjectID == projectID && st.UpdatedAt.Before(cutoff) && st.claimable(at) {
			rows = append(rows, st)
		}
	}
	slices.SortFunc(rows, func(a, b *osState) int {
		return cmp.Or(a.UpdatedAt.Compare(b.UpdatedAt), cmp.Compare(a.Entity, b.Entity), cmp.Compare(a.ID.String(), b.ID.String()))
	})
	out := make([]opensheetsync.Ref, 0, min(limit, len(rows)))
	for _, st := range rows[:min(limit, len(rows))] {
		out = append(out, st.Ref)
	}
	return out, nil
}

func (f *OpensheetSync) Backlog(_ context.Context, orgID, projectID uuid.UUID) (opensheetsync.Backlog, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "Backlog")
	var b opensheetsync.Backlog
	for _, st := range f.state {
		if st.org != orgID || st.ProjectID != projectID || !st.Dirty() {
			continue
		}
		switch {
		case st.Attempts >= opensheetsync.MaxRowAttempts:
			b.GivenUp++
		case st.Attempts > 0:
			b.Pending++
			b.Failing++
		default:
			b.Pending++
		}
	}
	return b, nil
}
