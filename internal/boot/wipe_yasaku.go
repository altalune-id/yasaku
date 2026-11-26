package boot

import (
	"context"

	"altalune.id/yasaku/internal/ledger"
	"altalune.id/yasaku/internal/opensheetsync"
	"altalune.id/yasaku/internal/period"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/project"
)

type projectNamer struct{ store project.Store }

// SECURITY: a project row outside the caller's org reads as absent, so a wipe never confirms against another org's name.
func (n projectNamer) ProjectName(ctx context.Context) (string, error) {
	tc, err := tenant.From(ctx)
	if err != nil {
		return "", err
	}
	p, err := n.store.ByID(ctx, tc.ProjectID)
	if err != nil {
		return "", err
	}
	if p.OrgID != tc.OrgID {
		return "", &project.NotFoundError{ID: tc.ProjectID.String()}
	}
	return p.Name, nil
}

type currentPeriod struct{ periods *period.Service }

func (c currentPeriod) EnsureCurrent(ctx context.Context) error {
	_, err := c.periods.EnsureCurrent(ctx)
	return err
}

type opensheetDisconnect struct{ svc *opensheetsync.Service }

func (d opensheetDisconnect) PrepareRevoke(ctx context.Context) (func(context.Context) error, error) {
	return d.svc.PrepareRevoke(ctx)
}

func (d opensheetDisconnect) Unlink(ctx context.Context) error {
	return d.svc.Unlink(ctx)
}

func opensheetDisconnector(svc *opensheetsync.Service) ledger.Disconnector {
	if svc == nil {
		return nil
	}
	return opensheetDisconnect{svc: svc}
}
