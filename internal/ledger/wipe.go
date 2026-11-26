package ledger

import (
	"context"
	"log/slog"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/limits"
	"altalune.id/yasaku/internal/platform/audit"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/platform/tenant"
)

// Deleter removes every row one ledger store holds in the project on ctx and reports how many went.
type Deleter interface {
	DeleteAllInProject(ctx context.Context) (int64, error)
}

// ProjectNamer returns the name of the project on ctx.
type ProjectNamer interface {
	ProjectName(ctx context.Context) (string, error)
}

// Disconnector removes the project's opensheet link and its sync state with the wipe, leaving the Sheet as it is.
type Disconnector interface {
	// PrepareRevoke runs before the wipe transaction and returns what revokes the project's opensheet grant once the wipe commits, or nil when there is nothing to revoke.
	PrepareRevoke(ctx context.Context) (func(ctx context.Context) error, error)
	// Unlink removes the link and its sync state in the caller's unit of work; no link is not an error.
	Unlink(ctx context.Context) error
}

// Locker takes the limit row locks a create in the project waits on.
type Locker interface {
	Lock(ctx context.Context, subs ...limits.Subject) error
}

// Seeder puts back the default categories of the project on ctx.
type Seeder interface {
	SeedDefaults(ctx context.Context) (int, error)
}

// CurrentEnsurer makes sure the project on ctx has a current period.
type CurrentEnsurer interface {
	EnsureCurrent(ctx context.Context) error
}

// WipeDeps is what a WipeWorkflow needs; a nil Opensheet means the module is unmounted.
type WipeDeps struct {
	Projects     ProjectNamer
	Opensheet    Disconnector
	Limits       Locker
	Transactions Deleter
	Periods      Deleter
	Wallets      Deleter
	Categories   Deleter
	Seeder       Seeder
	Current      CurrentEnsurer
	Audit        Auditor
	UnitOfWork   tenant.UnitOfWork
}

// WipeCounts is how many rows a wipe removed from each ledger table.
type WipeCounts struct {
	Transactions int64
	Periods      int64
	Wallets      int64
	Categories   int64
}

// WipeWorkflow deletes a project's wallets, categories, transactions and periods, keeping the project, its members, settings and keys.
type WipeWorkflow struct {
	d          WipeDeps
	log        *slog.Logger
	unexpected apperror.UnexpectedFunc
}

// NewWipeWorkflow binds the workflow to its dependencies.
func NewWipeWorkflow(d WipeDeps, log *slog.Logger, unexpected apperror.UnexpectedFunc) *WipeWorkflow {
	return &WipeWorkflow{d: d, log: log.With("module", "ledger"), unexpected: unexpected}
}

// Run wipes the project on ctx when the caller is a built-in owner and confirm is the project's name, then reseeds the default categories and the current period; a reseed failure after the commit is reported here and returned as *WipeSetupIncompleteError.
func (w *WipeWorkflow) Run(ctx context.Context, confirm string) (WipeCounts, error) {
	ctx, span := tracer.Start(ctx, "ledger.Wipe")
	defer span.End()

	tc, err := tenant.From(ctx)
	if err != nil {
		return WipeCounts{}, err
	}
	if !CanWipe(ctx) {
		return WipeCounts{}, &WipeOwnerOnlyError{}
	}
	name, err := w.d.Projects.ProjectName(ctx)
	if err != nil {
		return WipeCounts{}, w.unexpected(ctx, "ledger.Wipe: project name", err, "project_id", tc.ProjectID)
	}
	if strings.TrimSpace(confirm) != name {
		return WipeCounts{}, &WipeConfirmMismatchError{}
	}
	var revoke func(ctx context.Context) error
	if w.d.Opensheet != nil {
		if revoke, err = w.d.Opensheet.PrepareRevoke(ctx); err != nil {
			return WipeCounts{}, w.pass(ctx, "ledger.Wipe: prepare revoke", err, tc)
		}
	}
	var n WipeCounts
	err = w.d.UnitOfWork(ctx, func(ctx context.Context) error {
		if err := w.d.Limits.Lock(ctx, limits.Subject{Kind: limits.SubjectProject, ID: tc.ProjectID}); err != nil {
			return err
		}
		for _, step := range []struct {
			store Deleter
			count *int64
		}{
			{w.d.Transactions, &n.Transactions},
			{w.d.Periods, &n.Periods},
			{w.d.Wallets, &n.Wallets},
			{w.d.Categories, &n.Categories},
		} {
			c, err := step.store.DeleteAllInProject(ctx)
			if err != nil {
				return err
			}
			*step.count = c
		}
		// NOTE: the unlink comes last; writers take the link's share lock after their row locks, so unlinking first would deadlock with a concurrent write.
		if w.d.Opensheet != nil {
			if err := w.d.Opensheet.Unlink(ctx); err != nil {
				return err
			}
		}
		return w.d.Audit.Record(ctx, audit.Event{Action: "project.wiped", TargetKind: "project", TargetID: tc.ProjectID.String(),
			ProjectID: tc.ProjectID, Detail: map[string]string{
				"transactions": strconv.FormatInt(n.Transactions, 10), "periods": strconv.FormatInt(n.Periods, 10),
				"wallets": strconv.FormatInt(n.Wallets, 10), "categories": strconv.FormatInt(n.Categories, 10),
			}})
	})
	if err != nil {
		return WipeCounts{}, w.pass(ctx, "ledger.Wipe: delete", err, tc)
	}
	if revoke != nil {
		if err := revoke(ctx); err != nil {
			w.log.WarnContext(ctx, "ledger: wipe: opensheet revoke failed", "project_id", tc.ProjectID.String(), "err", err.Error())
		}
	}
	if _, err := w.d.Seeder.SeedDefaults(ctx); err != nil {
		return n, &WipeSetupIncompleteError{Cause: w.unexpected(ctx, "ledger.Wipe: seed categories", err, "org_id", tc.OrgID, "project_id", tc.ProjectID)}
	}
	if err := w.d.Current.EnsureCurrent(ctx); err != nil {
		return n, &WipeSetupIncompleteError{Cause: w.unexpected(ctx, "ledger.Wipe: current period", err, "org_id", tc.OrgID, "project_id", tc.ProjectID)}
	}
	return n, nil
}

// CanWipe reports whether the principal on ctx is a built-in owner of the tenant org on ctx. SECURITY: a custom role holding every permission is still not one.
func CanWipe(ctx context.Context) bool {
	tc, err := tenant.From(ctx)
	if err != nil {
		return false
	}
	p := session.PrincipalFrom(ctx)
	return p.HeldOrgID == tc.OrgID && tc.OrgID != uuid.Nil && p.HeldRole == "owner"
}

func (w *WipeWorkflow) pass(ctx context.Context, op string, err error, tc tenant.Context) error {
	if _, ok := apperror.AsAppError(err); ok {
		return err
	}
	return w.unexpected(ctx, op, err, "org_id", tc.OrgID, "project_id", tc.ProjectID)
}
