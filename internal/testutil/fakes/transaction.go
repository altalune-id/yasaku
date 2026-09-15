package fakes

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/transaction"
	"altalune.id/yasaku/money"
)

// Transaction is an in-memory transaction.Store.
type Transaction struct {
	mu    sync.Mutex
	data  map[uuid.UUID]*transaction.Transaction
	locks []uuid.UUID

	SaveFn       func(ctx context.Context, t *transaction.Transaction) error
	LockWalletFn func(ctx context.Context, orgID, projectID, walletID uuid.UUID) error
}

// NewTransaction returns an empty in-memory transaction.Store.
func NewTransaction() *Transaction {
	return &Transaction{data: map[uuid.UUID]*transaction.Transaction{}}
}

var _ transaction.Store = (*Transaction)(nil)

func (f *Transaction) Save(ctx context.Context, t *transaction.Transaction) error {
	if f.SaveFn != nil {
		return f.SaveFn(ctx, t)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *t
	f.data[t.ID] = &cp
	return nil
}

// ByID looks a row up by id alone. NOTE: it deliberately does not filter by org or project, so a service scope test cannot pass vacuously.
func (f *Transaction) ByID(_ context.Context, id uuid.UUID) (*transaction.Transaction, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.data[id]
	if !ok {
		return nil, &transaction.NotFoundError{ID: id.String()}
	}
	cp := *t
	return &cp, nil
}

func (f *Transaction) List(_ context.Context, orgID, projectID uuid.UUID, opts transaction.ListOpts) ([]*transaction.Transaction, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := make([]*transaction.Transaction, 0, len(f.data))
	for _, t := range f.data {
		if t.OrgID != orgID || t.ProjectID != projectID {
			continue
		}
		if !matchesListOpts(t, opts) {
			continue
		}
		cp := *t
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return keysetAfter(out[i], out[j]) })

	if opts.After != nil {
		cut := 0
		for cut < len(out) && !afterCursor(out[cut], *opts.After) {
			cut++
		}
		out = out[cut:]
	}
	if limit := opts.NormalizedLimit(); len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func matchesListOpts(t *transaction.Transaction, opts transaction.ListOpts) bool {
	if opts.WalletID != nil {
		to := t.ToWalletID != nil && *t.ToWalletID == *opts.WalletID
		if t.WalletID != *opts.WalletID && !to {
			return false
		}
	}
	if opts.CategoryID != nil && (t.CategoryID == nil || *t.CategoryID != *opts.CategoryID) {
		return false
	}
	if opts.PeriodID != nil && (t.PeriodID == nil || *t.PeriodID != *opts.PeriodID) {
		return false
	}
	if len(opts.Kinds) > 0 {
		found := false
		for _, k := range opts.Kinds {
			if t.Kind == k {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if opts.From != nil && t.OccurredAt.Before(*opts.From) {
		return false
	}
	if opts.To != nil && t.OccurredAt.After(*opts.To) {
		return false
	}
	if opts.Search != "" && !strings.Contains(strings.ToLower(t.Note), strings.ToLower(opts.Search)) {
		return false
	}
	return true
}

func keysetAfter(a, b *transaction.Transaction) bool {
	if !a.OccurredAt.Equal(b.OccurredAt) {
		return a.OccurredAt.After(b.OccurredAt)
	}
	if !a.CreatedAt.Equal(b.CreatedAt) {
		return a.CreatedAt.After(b.CreatedAt)
	}
	return a.ID.String() > b.ID.String()
}

func afterCursor(t *transaction.Transaction, c transaction.Cursor) bool {
	if !t.OccurredAt.Equal(c.OccurredAt) {
		return t.OccurredAt.Before(c.OccurredAt)
	}
	if !t.CreatedAt.Equal(c.CreatedAt) {
		return t.CreatedAt.Before(c.CreatedAt)
	}
	return t.ID.String() < c.ID.String()
}

func (f *Transaction) Delete(_ context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.data[id]; !ok {
		return &transaction.NotFoundError{ID: id.String()}
	}
	delete(f.data, id)
	return nil
}

func (f *Transaction) Balances(_ context.Context, orgID, projectID uuid.UUID) (map[uuid.UUID]money.Amount, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[uuid.UUID]money.Amount{}
	for _, t := range f.data {
		if t.OrgID != orgID || t.ProjectID != projectID {
			continue
		}
		addTxnBalance(out, t.WalletID, t.Effect(t.WalletID))
		if t.ToWalletID != nil {
			addTxnBalance(out, *t.ToWalletID, t.Effect(*t.ToWalletID))
		}
	}
	return out, nil
}

func addTxnBalance(out map[uuid.UUID]money.Amount, id uuid.UUID, delta money.Amount) {
	cur, ok := out[id]
	if !ok {
		out[id] = delta
		return
	}
	out[id] = cur.Add(delta)
}

func (f *Transaction) Balance(ctx context.Context, orgID, projectID, walletID uuid.UUID) (money.Amount, error) {
	all, err := f.Balances(ctx, orgID, projectID)
	if err != nil {
		return money.Amount{}, err
	}
	return all[walletID], nil
}

func (f *Transaction) LastCategoryForNote(_ context.Context, orgID, projectID uuid.UUID, noteNorm string) (uuid.UUID, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if noteNorm == "" {
		return uuid.Nil, false, nil
	}
	var best *transaction.Transaction
	for _, t := range f.data {
		if t.OrgID != orgID || t.ProjectID != projectID || t.CategoryID == nil {
			continue
		}
		if transaction.NormalizeNote(t.Note) != noteNorm {
			continue
		}
		if best == nil || keysetAfter(t, best) {
			best = t
		}
	}
	if best == nil {
		return uuid.Nil, false, nil
	}
	return *best.CategoryID, true, nil
}

// LockWallet records the lock request. NOTE: it enforces nothing, so a test asserting serialization must assert on Locks rather than on observed behaviour.
func (f *Transaction) LockWallet(ctx context.Context, orgID, projectID, walletID uuid.UUID) error {
	if f.LockWalletFn != nil {
		return f.LockWalletFn(ctx, orgID, projectID, walletID)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.locks = append(f.locks, walletID)
	return nil
}

// Locks returns the wallet ids LockWallet was called with, in order.
func (f *Transaction) Locks() []uuid.UUID {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]uuid.UUID(nil), f.locks...)
}
