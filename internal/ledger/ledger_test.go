package ledger_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/ledger"
	"altalune.id/yasaku/money"
)

func ptr[T any](v T) *T { return &v }

func TestDefaults(t *testing.T) {
	orgID, projectID := uuid.New(), uuid.New()
	got := ledger.Defaults(orgID, projectID)

	if got.OrgID != orgID || got.ProjectID != projectID {
		t.Errorf("scope not propagated: org=%v project=%v", got.OrgID, got.ProjectID)
	}
	if got.Timezone != "Asia/Jakarta" {
		t.Errorf("Timezone=%q want Asia/Jakarta", got.Timezone)
	}
	if got.Currency != money.IDR {
		t.Errorf("Currency=%q want IDR", got.Currency)
	}
	if got.PeriodStartDay != 1 {
		t.Errorf("PeriodStartDay=%d want 1", got.PeriodStartDay)
	}
	if !got.UpdatedAt.IsZero() {
		t.Errorf("UpdatedAt=%v, want zero: nothing has been written yet", got.UpdatedAt)
	}
	if *ledger.Defaults(orgID, projectID) != *got {
		t.Error("Defaults is not deterministic; an unconfigured project must read the same every time")
	}
	if loc, err := got.Location(); err != nil || loc.String() != "Asia/Jakarta" {
		t.Errorf("Location()=%v, %v", loc, err)
	}
}

func TestSettings_Apply_Rejects(t *testing.T) {
	tests := []struct {
		name  string
		patch ledger.Patch
		check func(error) bool
	}{
		{"unknown timezone", ledger.Patch{Timezone: ptr("Mars/Olympus")}, ledger.IsInvalidTimezoneError},
		{"empty timezone", ledger.Patch{Timezone: ptr("")}, ledger.IsInvalidTimezoneError},
		{"start day 0", ledger.Patch{PeriodStartDay: ptr(0)}, ledger.IsInvalidStartDayError},
		{"start day 29", ledger.Patch{PeriodStartDay: ptr(29)}, ledger.IsInvalidStartDayError},
		{"unknown currency", ledger.Patch{Currency: ptr(money.Currency("XXX"))}, ledger.IsUnknownCurrencyError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := ledger.Defaults(uuid.New(), uuid.New())
			before := *s

			err := s.Apply(tt.patch)
			if err == nil {
				t.Fatal("Apply: want error, got nil")
			}
			if !tt.check(err) {
				t.Fatalf("Apply: wrong error type: %T %v", err, err)
			}
			if *s != before {
				t.Errorf("a rejected Apply mutated the aggregate: got %+v want %+v", *s, before)
			}
		})
	}
}

func TestSettings_Apply_Accepts(t *testing.T) {
	s := ledger.Defaults(uuid.New(), uuid.New())
	s.UpdatedAt = time.Now().UTC().Add(-time.Hour)
	before := s.UpdatedAt

	err := s.Apply(ledger.Patch{
		Timezone:       ptr("Asia/Tokyo"),
		Currency:       ptr(money.Currency("USD")),
		PeriodStartDay: ptr(25),
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if s.Timezone != "Asia/Tokyo" {
		t.Errorf("Timezone=%q", s.Timezone)
	}
	if s.Currency != money.Currency("USD") {
		t.Errorf("Currency=%q", s.Currency)
	}
	if s.PeriodStartDay != 25 {
		t.Errorf("PeriodStartDay=%d", s.PeriodStartDay)
	}
	if !s.UpdatedAt.After(before) {
		t.Errorf("UpdatedAt not bumped: got %v want after %v", s.UpdatedAt, before)
	}
	loc, err := s.Location()
	if err != nil {
		t.Fatalf("Location: %v", err)
	}
	if loc.String() != "Asia/Tokyo" {
		t.Errorf("Location=%q", loc)
	}
}

func TestSettings_Apply_EmptyPatchKeepsValues(t *testing.T) {
	s := ledger.Defaults(uuid.New(), uuid.New())
	s.UpdatedAt = time.Now().UTC().Add(-time.Hour)

	if err := s.Apply(ledger.Patch{}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if s.Timezone != "Asia/Jakarta" || s.Currency != money.IDR || s.PeriodStartDay != 1 {
		t.Errorf("empty patch changed values: %+v", *s)
	}
}

func TestSettings_Apply_LowercaseCurrencyNormalises(t *testing.T) {
	s := ledger.Defaults(uuid.New(), uuid.New())
	if err := s.Apply(ledger.Patch{Currency: ptr(money.Currency("usd"))}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if s.Currency != money.Currency("USD") {
		t.Errorf("Currency=%q want USD", s.Currency)
	}
}

func TestSettings_Location_InvalidTimezone(t *testing.T) {
	s := ledger.Defaults(uuid.New(), uuid.New())
	s.Timezone = "Mars/Olympus"
	if _, err := s.Location(); !ledger.IsInvalidTimezoneError(err) {
		t.Fatalf("Location: want IsInvalidTimezoneError, got %T %v", err, err)
	}
}
