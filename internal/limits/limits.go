// Package limits holds the count limits a deployment enforces, their resolution order and the row locks that keep concurrent creates under them.
package limits

import "github.com/google/uuid"

// Key names one limit, such as "org.projects".
type Key string

// Kind says how a limit is measured.
type Kind uint8

const (
	// Count limits how many rows exist at once.
	Count Kind = iota
	// Metered limits usage over a Period; reserved.
	Metered
)

// Period is the window a Metered limit resets over; reserved.
type Period uint8

// SubjectKind names what a limit counts against.
type SubjectKind string

const (
	// SubjectUser counts against one person across every org.
	SubjectUser SubjectKind = "user"
	// SubjectOrg counts against one org.
	SubjectOrg SubjectKind = "org"
	// SubjectProject counts against one project.
	SubjectProject SubjectKind = "project"
)

// Subject is the user, org or project a limit is checked for.
type Subject struct {
	Kind SubjectKind
	ID   uuid.UUID
}

// Unlimited is the value of a limit that never refuses.
const Unlimited int64 = -1

// DefaultPlan is the plan every org starts on and every user subject resolves against.
const DefaultPlan = "first"

// Def is one catalogue entry: a key's subject and its default per deployment mode.
type Def struct {
	Key        Key
	Kind       Kind
	Subject    SubjectKind
	Cloud      int64
	SelfHosted int64
	Period     Period
	Hidden     bool
}

// PlanEntry is one plan_limits row.
type PlanEntry struct {
	Plan  string
	Key   Key
	Value int64
}

// Override is one limit_overrides row.
type Override struct {
	Subject Subject
	Key     Key
	Value   int64
}
