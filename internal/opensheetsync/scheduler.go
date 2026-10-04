package opensheetsync

import (
	"context"
	"log/slog"
	"time"

	"altalune.id/yasaku/scheduler"
)

const (
	reconcileJobName = "opensheet-reconcile"
	reconcileEvery   = 5 * time.Minute
	reconcileJitter  = 30 * time.Second
	// NOTE: the scheduler applies Timeout per tenant, not per tick; 60s leaves room for two inline jobs per org (submitHeadroom), and a queued reconcile only publishes.
	reconcileTimeout = time.Minute
)

// Scheduler adapts *Mirror to scheduler.Provider.
type Scheduler struct {
	mirror *Mirror
	log    *slog.Logger
}

// NewScheduler binds the reconciler; a nil mirror, for an unmounted module, declares no job.
func NewScheduler(mirror *Mirror, log *slog.Logger) *Scheduler {
	return &Scheduler{mirror: mirror, log: log.With("module", "opensheetsync")}
}

// SchedulerJobs implements scheduler.Provider.
func (a *Scheduler) SchedulerJobs() []scheduler.Job {
	if a.mirror == nil {
		return nil
	}
	return []scheduler.Job{{
		Name:      reconcileJobName,
		Scope:     scheduler.ScopeTenant,
		Schedule:  scheduler.MustEveryInterval(reconcileEvery, reconcileJitter),
		Timeout:   reconcileTimeout,
		Singleton: true,
		Run: func(ctx context.Context) error {
			n, err := a.mirror.Reconcile(ctx)
			if n > 0 {
				a.log.LogAttrs(ctx, slog.LevelInfo, "opensheetsync.reconcile", slog.Int("resubmitted", n))
			}
			return err
		},
	}}
}
