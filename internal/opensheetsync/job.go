package opensheetsync

import (
	"fmt"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/platform/queue"
)

const maxRefsPerJob = 50

func syncJob() queue.Job { return queue.Job{Name: "opensheet.sync", Version: 1} }

type syncV1 struct {
	ProjectID uuid.UUID `json:"project_id"`
	Refs      []refV1   `json:"refs"`
}

type refV1 struct {
	Entity string    `json:"entity"`
	ID     uuid.UUID `json:"id"`
}

func payloadOf(projectID uuid.UUID, refs []Ref) syncV1 {
	p := syncV1{ProjectID: projectID, Refs: make([]refV1, 0, len(refs))}
	for _, r := range refs {
		p.Refs = append(p.Refs, refV1{Entity: string(r.Entity), ID: r.ID})
	}
	return p
}

func (p syncV1) refs() ([]Ref, error) {
	out := make([]Ref, 0, len(p.Refs))
	for _, r := range p.Refs {
		e, ok := ParseEntity(r.Entity)
		if !ok {
			return nil, fmt.Errorf("opensheetsync: payload: unknown entity %q", r.Entity)
		}
		out = append(out, Ref{Entity: e, ID: r.ID})
	}
	return out, nil
}
