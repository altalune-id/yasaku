package transaction_test

import (
	"encoding/base64"
	"testing"
	"time"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/transaction"
)

func TestCursor_RoundTrip(t *testing.T) {
	want := transaction.Cursor{
		OccurredAt: time.Date(2026, 9, 15, 10, 30, 0, 123456789, time.UTC),
		CreatedAt:  time.Date(2026, 9, 15, 10, 30, 1, 0, time.UTC),
		ID:         uuid.New(),
	}
	got, err := transaction.ParseCursor(want.Encode())
	if err != nil {
		t.Fatalf("ParseCursor: %v", err)
	}
	if !got.OccurredAt.Equal(want.OccurredAt) {
		t.Errorf("OccurredAt=%v want %v", got.OccurredAt, want.OccurredAt)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) {
		t.Errorf("CreatedAt=%v want %v", got.CreatedAt, want.CreatedAt)
	}
	if got.ID != want.ID {
		t.Errorf("ID=%v want %v", got.ID, want.ID)
	}
}

func TestCursor_EncodeIsURLSafeAndUnpadded(t *testing.T) {
	c := transaction.Cursor{OccurredAt: time.Now().UTC(), CreatedAt: time.Now().UTC(), ID: uuid.New()}
	enc := c.Encode()
	for _, r := range enc {
		if r == '+' || r == '/' || r == '=' {
			t.Fatalf("cursor %q must be raw url-safe base64", enc)
		}
	}
	if _, err := base64.RawURLEncoding.DecodeString(enc); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestParseCursor_Garbage(t *testing.T) {
	for _, in := range []string{
		"",
		"!!!not base64!!!",
		base64.RawURLEncoding.EncodeToString([]byte("only|two")),
		base64.RawURLEncoding.EncodeToString([]byte("nope|nope|nope")),
		base64.RawURLEncoding.EncodeToString([]byte("2026-09-15T10:00:00Z|2026-09-15T10:00:00Z|not-a-uuid")),
	} {
		if _, err := transaction.ParseCursor(in); err == nil {
			t.Errorf("ParseCursor(%q): want an error", in)
		}
	}
}
