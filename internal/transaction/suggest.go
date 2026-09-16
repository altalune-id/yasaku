package transaction

import (
	"context"

	"github.com/google/uuid"
)

type lastUsedSuggester struct{ store Store }

var _ Suggester = lastUsedSuggester{}

func (s lastUsedSuggester) Suggest(ctx context.Context, orgID, projectID uuid.UUID, note string) (uuid.UUID, bool, error) {
	norm := NormalizeNote(note)
	if norm == "" {
		return uuid.Nil, false, nil
	}
	return s.store.LastCategoryForNote(ctx, orgID, projectID, norm)
}
