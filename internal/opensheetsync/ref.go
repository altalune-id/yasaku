package opensheetsync

import (
	"bytes"
	"cmp"
	"slices"
	"time"

	"github.com/google/uuid"
)

// Entity names a kind of row yasaku mirrors.
type Entity string

const (
	EntityTransaction Entity = "transaction"
	EntityWallet      Entity = "wallet"
	EntityCategory    Entity = "category"
)

// LockRank orders entities the way every writer locks their state rows: transactions, then categories, then wallets.
func (e Entity) LockRank() int {
	switch e {
	case EntityTransaction:
		return 0
	case EntityCategory:
		return 1
	}
	return 2
}

// ParseEntity maps a wire name to its Entity; ok is false for an unknown name.
func ParseEntity(s string) (Entity, bool) {
	switch e := Entity(s); e {
	case EntityTransaction, EntityWallet, EntityCategory:
		return e, true
	}
	return "", false
}

// Ref names one row whose current state the mirror must push; Deleted marks it gone and Cascade also re-marks the transactions that show its name.
type Ref struct {
	Entity  Entity
	ID      uuid.UUID
	Deleted bool
	Cascade bool
}

type refKey struct {
	entity Entity
	id     uuid.UUID
}

// Dedupe merges refs naming one row, keeping first-seen order; Deleted and Cascade survive the merge.
func Dedupe(refs []Ref) []Ref {
	out := make([]Ref, 0, len(refs))
	at := make(map[refKey]int, len(refs))
	for _, r := range refs {
		k := refKey{entity: r.Entity, id: r.ID}
		if i, seen := at[k]; seen {
			out[i].Deleted = out[i].Deleted || r.Deleted
			out[i].Cascade = out[i].Cascade || r.Cascade
			continue
		}
		at[k] = len(out)
		out = append(out, r)
	}
	return out
}

// LockOrder dedupes refs into the order every writer locks state rows in: by LockRank, then by id as Postgres orders a uuid.
func LockOrder(refs []Ref) []Ref {
	out := Dedupe(refs)
	slices.SortFunc(out, func(a, b Ref) int {
		return cmp.Or(cmp.Compare(a.Entity.LockRank(), b.Entity.LockRank()), bytes.Compare(a.ID[:], b.ID[:]))
	})
	return out
}

// State is one row's sync bookkeeping; the sheet trails the row while SyncedVersion is below Version.
type State struct {
	Ref
	ProjectID     uuid.UUID
	Version       int64
	SyncedVersion int64
	Attempts      int
	LastError     string
	UpdatedAt     time.Time
}

// Dirty reports whether the sheet still trails the row.
func (s State) Dirty() bool { return s.SyncedVersion < s.Version }

// MaxRowAttempts is how many times a row opensheet refuses is tried before it waits for its next edit or a "Sync everything now".
const MaxRowAttempts = 5

// RowBackoff is how long a refused row waits after its attempts-th refusal: 1, 2, 4, then 8 minutes.
func RowBackoff(attempts int) time.Duration {
	return time.Minute << min(max(attempts-1, 0), 3)
}
