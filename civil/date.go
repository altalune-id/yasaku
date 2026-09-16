// Package civil holds a timezone-free calendar date.
package civil

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"time"
)

// Date is a calendar date without a time or zone.
type Date struct {
	Year  int
	Month time.Month
	Day   int
}

const layout = "2006-01-02"

// DateOf returns the calendar date of t in loc.
func DateOf(t time.Time, loc *time.Location) Date {
	y, m, d := t.In(loc).Date()
	return Date{Year: y, Month: m, Day: d}
}

// ParseDate parses a YYYY-MM-DD string.
func ParseDate(s string) (Date, error) {
	t, err := time.Parse(layout, s)
	if err != nil {
		return Date{}, &ParseError{Input: s}
	}
	return DateOf(t, time.UTC), nil
}

// AddDays returns the date n days after d (n may be negative).
func (d Date) AddDays(n int) Date { return DateOf(d.In(time.UTC).AddDate(0, 0, n), time.UTC) }

// Compare returns -1, 0 or 1.
func (d Date) Compare(o Date) int { return d.In(time.UTC).Compare(o.In(time.UTC)) }

// Before reports whether d is earlier than o.
func (d Date) Before(o Date) bool { return d.Compare(o) < 0 }

// In returns local midnight of d in loc.
func (d Date) In(loc *time.Location) time.Time {
	return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, loc)
}

// IsZero reports whether d is the zero Date.
func (d Date) IsZero() bool { return d == Date{} }

// String formats d as YYYY-MM-DD.
func (d Date) String() string { return d.In(time.UTC).Format(layout) }

// MarshalText implements encoding.TextMarshaler.
func (d Date) MarshalText() ([]byte, error) { return []byte(d.String()), nil }

// UnmarshalText implements encoding.TextUnmarshaler.
func (d *Date) UnmarshalText(b []byte) error {
	p, err := ParseDate(string(b))
	if err != nil {
		return err
	}
	*d = p
	return nil
}

// Value implements driver.Valuer as a YYYY-MM-DD string.
func (d Date) Value() (driver.Value, error) { return d.String(), nil }

// Scan implements sql.Scanner for time.Time, string and []byte sources.
func (d *Date) Scan(src any) error {
	switch v := src.(type) {
	case time.Time:
		*d = DateOf(v, time.UTC)
		return nil
	case string:
		return d.UnmarshalText([]byte(v))
	case []byte:
		return d.UnmarshalText(v)
	case nil:
		*d = Date{}
		return nil
	}
	return fmt.Errorf("civil: cannot scan %T into Date", src)
}

// ParseError reports an input that is not YYYY-MM-DD.
type ParseError struct{ Input string }

func (e *ParseError) Error() string { return fmt.Sprintf("civil: %q: not YYYY-MM-DD", e.Input) }

// IsParseError reports whether err's chain contains a *ParseError.
func IsParseError(err error) bool {
	_, ok := errors.AsType[*ParseError](err)
	return ok
}
