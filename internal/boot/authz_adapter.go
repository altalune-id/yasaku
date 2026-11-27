package boot

import (
	"context"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform/authz"
	"altalune.id/yasaku/internal/role"
)

type roleSourceAdapter struct {
	org.Counter
	roles role.Store
}

type roleNames struct{ store role.Store }

func (n roleNames) RoleName(ctx context.Context, roleID uuid.UUID) (string, error) {
	r, err := n.store.ByID(ctx, roleID)
	if err != nil {
		return "", err
	}
	return r.Name, nil
}

func (a roleSourceAdapter) CustomPermissions(ctx context.Context, roleID uuid.UUID) ([]authz.Permission, error) {
	return a.roles.Permissions(ctx, roleID)
}
