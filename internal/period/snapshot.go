package period

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"altalune.id/yasaku/money"
)

// WalletClosing is one wallet's balance at the moment of closing.
type WalletClosing struct {
	WalletID uuid.UUID `json:"wallet_id"`
	Name     string    `json:"name"`
	Closing  int64     `json:"closing"`
}

// Snapshot is the frozen result of a period, persisted as a JSON document.
type Snapshot struct {
	Currency   money.Currency  `json:"currency"`
	Income     int64           `json:"income"`
	Expense    int64           `json:"expense"`
	Net        int64           `json:"net"`
	TxCount    int             `json:"tx_count"`
	Wallets    []WalletClosing `json:"wallets"`
	ComputedAt time.Time       `json:"computed_at"`
}

// Closing is one "tutup buku" event; a reopened period accumulates more than one.
type Closing struct {
	ID        uuid.UUID
	OrgID     uuid.UUID
	ProjectID uuid.UUID
	PeriodID  uuid.UUID
	ClosedAt  time.Time
	ClosedBy  uuid.UUID
	Snapshot  Snapshot
}

func marshalSnapshot(s *Snapshot) (*string, error) {
	if s == nil {
		return nil, nil //nolint:nilnil // a nil snapshot is the absent-document case, not a failure
	}
	b, err := json.Marshal(s)
	if err != nil {
		return nil, fmt.Errorf("period: marshal snapshot: %w", err)
	}
	js := string(b)
	return &js, nil
}

func unmarshalSnapshot(raw []byte) (*Snapshot, error) {
	if len(raw) == 0 {
		return nil, nil //nolint:nilnil // a NULL column is the absent-document case, not a failure
	}
	var s Snapshot
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("period: unmarshal snapshot: %w", err)
	}
	return &s, nil
}

// jsonDoc scans a JSON column from either driver: pgx hands back string or []byte, SQLite a TEXT string.
type jsonDoc struct{ raw []byte }

func (j *jsonDoc) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		j.raw = nil
	case []byte:
		j.raw = append([]byte(nil), v...)
	case string:
		j.raw = []byte(v)
	default:
		return fmt.Errorf("period: cannot scan %T into a json document", src)
	}
	return nil
}

func (j jsonDoc) Value() (driver.Value, error) {
	if len(j.raw) == 0 {
		return nil, nil
	}
	return string(j.raw), nil
}
