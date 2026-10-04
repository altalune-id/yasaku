package opensheetsync

import (
	"fmt"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/platform/sealer"
)

// SECURITY: the additional data binds a sealed key to its org and project, so a row copied into another project never opens.
func keyAAD(orgID, projectID uuid.UUID) []byte {
	return []byte("opensheetsync.api_key|" + orgID.String() + "|" + projectID.String())
}

func sealKey(sl sealer.Sealer, orgID, projectID uuid.UUID, key string) ([]byte, error) {
	sealed, err := sl.Seal([]byte(key), keyAAD(orgID, projectID))
	if err != nil {
		return nil, fmt.Errorf("opensheetsync.sealKey: %w", err)
	}
	return sealed, nil
}

func openKey(sl sealer.Sealer, l *Link) (string, error) {
	plain, err := sl.Open(l.APIKeySealed, keyAAD(l.OrgID, l.ProjectID))
	if err != nil {
		return "", fmt.Errorf("opensheetsync.openKey: %w", err)
	}
	return string(plain), nil
}
