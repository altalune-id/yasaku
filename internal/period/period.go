// Package period is the budget periods bounded context: user-closable books ("tutup buku").
package period

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"altalune.id/yasaku/civil"
)

// Status is the lifecycle state of a period.
type Status string

const (
	// StatusOpen marks a period whose totals may still change.
	StatusOpen Status = "open"
	// StatusClosed marks a period whose totals are snapshotted and locked.
	StatusClosed Status = "closed"
)

// MaxNameRunes is the longest accepted period name.
const MaxNameRunes = 60

// MinStartDay is the earliest accepted cycle start day.
const MinStartDay = 1

// MaxStartDay caps the cycle start day so the derived date exists in every month.
const MaxStartDay = 28

// Period is the aggregate root. Exactly one period per project is current (EndDate == nil).
type Period struct {
	ID        uuid.UUID
	OrgID     uuid.UUID
	ProjectID uuid.UUID
	Name      string
	StartDate civil.Date
	EndDate   *civil.Date
	Status    Status
	ClosedAt  *time.Time
	Snapshot  *Snapshot
	CreatedAt time.Time
	UpdatedAt time.Time
}

// New enforces creation invariants: a non-zero start, a name of at most 60 runes defaulting to DefaultName.
func New(orgID, projectID uuid.UUID, start civil.Date, name string) (*Period, error) {
	if start.IsZero() {
		return nil, &InvalidRangeError{Reason: "start date is empty"}
	}
	if strings.TrimSpace(name) == "" {
		name = DefaultName(start)
	}
	clean, err := cleanName(name)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	return &Period{
		ID:        uuid.Must(uuid.NewV7()),
		OrgID:     orgID,
		ProjectID: projectID,
		Name:      clean,
		StartDate: start,
		Status:    StatusOpen,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// DefaultName renders the start month in English, e.g. "Sep 2026"; the UI translates it.
func DefaultName(start civil.Date) string { return start.In(time.UTC).Format("Jan 2006") }

// ClampStartDay confines a configured cycle start day to a day that exists in every month.
func ClampStartDay(day int) int {
	if day < MinStartDay {
		return MinStartDay
	}
	if day > MaxStartDay {
		return MaxStartDay
	}
	return day
}

// FirstStart returns the most recent cycle start on or before today.
func FirstStart(today civil.Date, startDay int) civil.Date {
	day := ClampStartDay(startDay)
	if today.Day >= day {
		return civil.Date{Year: today.Year, Month: today.Month, Day: day}
	}
	prev := civil.Date{Year: today.Year, Month: today.Month, Day: 1}.AddDays(-1)
	return civil.Date{Year: prev.Year, Month: prev.Month, Day: day}
}

// Rename replaces the display name, leaving the aggregate untouched when the name is rejected.
func (p *Period) Rename(name string) error {
	clean, err := cleanName(name)
	if err != nil {
		return err
	}
	p.Name = clean
	p.UpdatedAt = time.Now().UTC()
	return nil
}

// IsCurrent reports whether this is the project's open-ended period.
func (p *Period) IsCurrent() bool { return p.EndDate == nil }

// IsLocked reports whether the period is closed.
func (p *Period) IsLocked() bool { return p.Status == StatusClosed }

// Contains reports whether d falls in the period, inclusive of both ends.
func (p *Period) Contains(d civil.Date) bool {
	if d.Before(p.StartDate) {
		return false
	}
	return p.EndDate == nil || !p.EndDate.Before(d)
}

func cleanName(name string) (string, error) {
	clean := strings.TrimSpace(name)
	if clean == "" {
		return "", &InvalidNameError{Reason: "empty"}
	}
	if utf8.RuneCountInString(clean) > MaxNameRunes {
		return "", &InvalidNameError{Reason: "over 60 characters"}
	}
	return clean, nil
}

// ListOpts filters Store.List. Zero value returns every period in scope.
type ListOpts struct {
	Limit  int
	Before *civil.Date
}
