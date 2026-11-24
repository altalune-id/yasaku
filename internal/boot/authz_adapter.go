package boot

import (
	"context"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform/authz"
)

type roleSourceAdapter struct {
	org.Counter
	noCustomRoles
}

type noCustomRoles struct{}

func (noCustomRoles) CustomPermissions(context.Context, uuid.UUID) ([]authz.Permission, error) {
	return nil, nil
}
