package opensheetsync

import (
	"github.com/google/uuid"

	"altalune.id/yasaku/internal/platform/queue"
	"altalune.id/yasaku/internal/platform/sealer"
)

// SyncJob exposes the sync job to the external tests.
func SyncJob() queue.Job { return syncJob() }

// SealKey exposes the key sealing to the external tests.
func SealKey(sl sealer.Sealer, orgID, projectID uuid.UUID, key string) ([]byte, error) {
	return sealKey(sl, orgID, projectID, key)
}

// SyncPayload is the v1 payload as the external tests build it.
type SyncPayload = syncV1

// SyncPayloadRef is one ref of a v1 payload.
type SyncPayloadRef = refV1

// RefusalText exposes the last_error text a link refusal stores.
func RefusalText(err error) string { return refusalText(err) }

// FailureText exposes the last_error text a released row stores.
func FailureText(err error) string { return failureText(err) }
