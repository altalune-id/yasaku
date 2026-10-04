package boot

import (
	"log/slog"

	"altalune.id/yasaku/internal/opensheetsync"
	"altalune.id/yasaku/scheduler"
)

func yasakuSchedulerDomains() []string { return []string{"opensheetsync"} }

// NOTE: the providers are always built; an unmounted module's scheduler declares no job.
func yasakuSchedulerProviders(s *Services, log *slog.Logger) []scheduler.Provider {
	return []scheduler.Provider{opensheetsync.NewScheduler(s.OpensheetMirror, log)}
}
