package handlers

import (
	"testing"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/org"
)

func TestSplitOrgs_CarriesTheSystemFlag(t *testing.T) {
	sys := &org.Org{ID: uuid.New(), Slug: "ops", Name: "Ops", System: true}
	mine := &org.Org{ID: uuid.New(), Slug: "home", Name: "Home"}
	active, others := splitOrgs([]*org.Org{sys, mine}, mine.ID)
	if active == nil || active.System || len(others) != 1 || !others[0].System {
		t.Fatalf("active=%+v others=%+v, want the system flag on ops only", active, others)
	}
	active, _ = splitOrgs([]*org.Org{sys, mine}, uuid.New())
	if active == nil || !active.System {
		t.Fatalf("fallback active=%+v, want the first org with its system flag", active)
	}
}
