package transaction

import (
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Cursor is the keyset position of the last row of a page.
type Cursor struct {
	OccurredAt time.Time
	CreatedAt  time.Time
	ID         uuid.UUID
}

// Encode renders the cursor as an opaque URL-safe token.
func (c Cursor) Encode() string {
	raw := c.OccurredAt.UTC().Format(time.RFC3339Nano) + "|" +
		c.CreatedAt.UTC().Format(time.RFC3339Nano) + "|" +
		c.ID.String()
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// ParseCursor reads a token produced by Encode; callers treat an error as "start from the top".
func ParseCursor(s string) (Cursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return Cursor{}, fmt.Errorf("transaction: cursor: decode: %w", err)
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != 3 {
		return Cursor{}, fmt.Errorf("transaction: cursor: want 3 fields, got %d", len(parts))
	}
	occurredAt, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return Cursor{}, fmt.Errorf("transaction: cursor: occurred_at: %w", err)
	}
	createdAt, err := time.Parse(time.RFC3339Nano, parts[1])
	if err != nil {
		return Cursor{}, fmt.Errorf("transaction: cursor: created_at: %w", err)
	}
	id, err := uuid.Parse(parts[2])
	if err != nil {
		return Cursor{}, fmt.Errorf("transaction: cursor: id: %w", err)
	}
	return Cursor{OccurredAt: occurredAt.UTC(), CreatedAt: createdAt.UTC(), ID: id}, nil
}
