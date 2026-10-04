package boot

import (
	"log/slog"

	"altalune.id/yasaku/internal/opensheetsync"
	"altalune.id/yasaku/internal/platform/queue"
)

func yasakuConsumerDomains() []string { return []string{"opensheetsync"} }

// NOTE: the providers are always built; an unmounted module's consumer declares no job.
func yasakuConsumerProviders(s *Services, _ *slog.Logger) []queue.Provider {
	return []queue.Provider{opensheetsync.NewConsumer(s.OpensheetSync)}
}
