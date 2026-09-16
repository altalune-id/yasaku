// Package ledger is the per-project ledger settings bounded context.
package ledger

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"altalune.id/yasaku/money"
)

// DefaultTimezone is the IANA zone a project starts with.
const DefaultTimezone = "Asia/Jakarta"

// DefaultPeriodStartDay is the day-of-month a project's budget period starts on by default.
const DefaultPeriodStartDay = 1

// MinPeriodStartDay is the earliest day-of-month a period may start on.
const MinPeriodStartDay = 1

// MaxPeriodStartDay is the latest day-of-month a period may start on, so every month has one.
const MaxPeriodStartDay = 28

// Settings is the aggregate root: one project's ledger configuration.
type Settings struct {
	OrgID          uuid.UUID
	ProjectID      uuid.UUID
	Timezone       string
	Currency       money.Currency
	PeriodStartDay int
	UpdatedAt      time.Time
}

// Defaults returns the settings a project that has never been configured behaves as; UpdatedAt stays zero because nothing has been written yet.
func Defaults(orgID, projectID uuid.UUID) *Settings {
	return &Settings{
		OrgID:          orgID,
		ProjectID:      projectID,
		Timezone:       DefaultTimezone,
		Currency:       money.IDR,
		PeriodStartDay: DefaultPeriodStartDay,
	}
}

// Patch carries the fields a caller wants changed; a nil field is left alone.
type Patch struct {
	Timezone       *string
	Currency       *money.Currency
	PeriodStartDay *int
}

// Apply validates p and writes it onto s, bumping UpdatedAt; a rejected patch leaves s untouched.
func (s *Settings) Apply(p Patch) error {
	next := *s
	if p.Timezone != nil {
		tz := strings.TrimSpace(*p.Timezone)
		if tz == "" {
			return &InvalidTimezoneError{Value: *p.Timezone}
		}
		if _, err := time.LoadLocation(tz); err != nil {
			return &InvalidTimezoneError{Value: *p.Timezone}
		}
		next.Timezone = tz
	}
	if p.Currency != nil {
		c, err := money.ParseCurrency(string(*p.Currency))
		if err != nil {
			return &UnknownCurrencyError{Code: string(*p.Currency)}
		}
		next.Currency = c
	}
	if p.PeriodStartDay != nil {
		if *p.PeriodStartDay < MinPeriodStartDay || *p.PeriodStartDay > MaxPeriodStartDay {
			return &InvalidStartDayError{Value: *p.PeriodStartDay}
		}
		next.PeriodStartDay = *p.PeriodStartDay
	}
	next.UpdatedAt = time.Now().UTC()
	*s = next
	return nil
}

// Location resolves Timezone to an IANA location.
func (s *Settings) Location() (*time.Location, error) {
	loc, err := time.LoadLocation(s.Timezone)
	if err != nil {
		return nil, &InvalidTimezoneError{Value: s.Timezone}
	}
	return loc, nil
}
