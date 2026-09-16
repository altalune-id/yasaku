package period

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/google/uuid"

	"altalune.id/yasaku/civil"
	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/platform/tenant"
)

//nolint:gochecknoglobals // OTel tracer is a package-level fixture, not runtime state.
var tracer = otel.Tracer("altalune.id/yasaku/internal/period")

type rowReader func(ctx context.Context, id uuid.UUID) (*Period, error)

// Service is the periods driving port.
type Service struct {
	store      Store
	log        *slog.Logger
	unexpected apperror.UnexpectedFunc
	settings   SettingsReader
	snap       Snapshotter
	uow        UnitOfWork
	now        func() time.Time
}

// NewService binds the service to its dependencies.
func NewService(
	store Store,
	log *slog.Logger,
	unexpected apperror.UnexpectedFunc,
	settings SettingsReader,
	snap Snapshotter,
	uow UnitOfWork,
	now func() time.Time,
) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{
		store:      store,
		log:        log.With("module", "period"),
		unexpected: unexpected,
		settings:   settings,
		snap:       snap,
		uow:        uow,
		now:        now,
	}
}

// EnsureCurrent returns the project's current period, deriving the first one from the cycle start day when none exists.
func (s *Service) EnsureCurrent(ctx context.Context) (*Period, error) {
	ctx, span := tracer.Start(ctx, "period.EnsureCurrent")
	defer span.End()

	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, err
	}
	span.SetAttributes(
		attribute.String("org_id", tc.OrgID.String()),
		attribute.String("project_id", tc.ProjectID.String()),
	)

	cur, err := s.store.Current(ctx, tc.OrgID, tc.ProjectID)
	if err == nil {
		return cur, nil
	}
	if !IsNotFoundError(err) {
		span.RecordError(err)
		return nil, s.unexpected(ctx, "period.EnsureCurrent: current", err,
			"org_id", tc.OrgID, "project_id", tc.ProjectID)
	}

	loc, startDay, err := s.cycle(ctx, tc)
	if err != nil {
		return nil, err
	}
	start := FirstStart(civil.DateOf(s.now(), loc), startDay)
	first, err := New(tc.OrgID, tc.ProjectID, start, "")
	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	if saveErr := s.store.Save(ctx, first); saveErr != nil {
		if IsOverlapError(saveErr) {
			// NOTE: a concurrent creator won the partial unique index; its row is the current period.
			return s.currentAfterRace(ctx, tc)
		}
		span.RecordError(saveErr)
		return nil, s.unexpected(ctx, "period.EnsureCurrent: save", saveErr,
			"org_id", tc.OrgID, "project_id", tc.ProjectID)
	}
	span.SetAttributes(attribute.String("period.id", first.ID.String()))
	return first, nil
}

// SuggestedEnd returns the end date a close of the identified period defaults to under the project's cycle settings.
func (s *Service) SuggestedEnd(ctx context.Context, id uuid.UUID) (civil.Date, error) {
	ctx, span := tracer.Start(ctx, "period.SuggestedEnd",
		trace.WithAttributes(attribute.String("period.id", id.String())))
	defer span.End()

	tc, err := tenant.From(ctx)
	if err != nil {
		return civil.Date{}, err
	}
	p, err := s.scoped(ctx, tc, id)
	if err != nil {
		return civil.Date{}, err
	}
	if p.EndDate != nil {
		return *p.EndDate, nil
	}
	loc, startDay, err := s.cycle(ctx, tc)
	if err != nil {
		return civil.Date{}, err
	}
	return SuggestedEnd(p.StartDate, civil.DateOf(s.now(), loc), startDay), nil
}

// Reopenable returns the one period Reopen would accept in the caller's scope, or nil when none would.
func (s *Service) Reopenable(ctx context.Context) (*Period, error) {
	ctx, span := tracer.Start(ctx, "period.Reopenable")
	defer span.End()

	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, err
	}
	items, err := s.store.List(ctx, tc.OrgID, tc.ProjectID, ListOpts{})
	if err != nil {
		span.RecordError(err)
		return nil, s.unexpected(ctx, "period.Reopenable: list", err,
			"org_id", tc.OrgID, "project_id", tc.ProjectID)
	}
	return LatestClosed(items), nil
}

// Current returns the project's current period without creating one.
func (s *Service) Current(ctx context.Context) (*Period, error) {
	ctx, span := tracer.Start(ctx, "period.Current")
	defer span.End()

	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, err
	}
	cur, err := s.store.Current(ctx, tc.OrgID, tc.ProjectID)
	if err != nil {
		span.RecordError(err)
		if IsNotFoundError(err) {
			return nil, err
		}
		return nil, s.unexpected(ctx, "period.Current: current", err,
			"org_id", tc.OrgID, "project_id", tc.ProjectID)
	}
	return cur, nil
}

// List returns periods in the caller's scope, newest start first.
func (s *Service) List(ctx context.Context, opts ListOpts) ([]*Period, error) {
	ctx, span := tracer.Start(ctx, "period.List")
	defer span.End()

	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, err
	}
	out, err := s.store.List(ctx, tc.OrgID, tc.ProjectID, opts)
	if err != nil {
		span.RecordError(err)
		return nil, s.unexpected(ctx, "period.List: list", err,
			"org_id", tc.OrgID, "project_id", tc.ProjectID)
	}
	return out, nil
}

// ByID returns the identified period when it belongs to the caller's scope.
func (s *Service) ByID(ctx context.Context, id uuid.UUID) (*Period, error) {
	ctx, span := tracer.Start(ctx, "period.ByID",
		trace.WithAttributes(attribute.String("period.id", id.String())))
	defer span.End()

	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, err
	}
	return s.scoped(ctx, tc, id)
}

// Rename replaces the identified period's display name.
func (s *Service) Rename(ctx context.Context, id uuid.UUID, name string) (*Period, error) {
	ctx, span := tracer.Start(ctx, "period.Rename",
		trace.WithAttributes(attribute.String("period.id", id.String())))
	defer span.End()

	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, err
	}
	p, err := s.scoped(ctx, tc, id)
	if err != nil {
		return nil, err
	}
	if renameErr := p.Rename(name); renameErr != nil {
		span.RecordError(renameErr)
		return nil, renameErr
	}
	if saveErr := s.store.Save(ctx, p); saveErr != nil {
		span.RecordError(saveErr)
		if isDomainError(saveErr) {
			return nil, saveErr
		}
		return nil, s.unexpected(ctx, "period.Rename: save", saveErr, "period_id", id)
	}
	return p, nil
}

// PreviewClose computes what closing the identified period at end would freeze, persisting nothing.
func (s *Service) PreviewClose(ctx context.Context, id uuid.UUID, end civil.Date) (Snapshot, error) {
	ctx, span := tracer.Start(ctx, "period.PreviewClose",
		trace.WithAttributes(attribute.String("period.id", id.String())))
	defer span.End()

	tc, err := tenant.From(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	p, err := s.closable(ctx, tc, id, end)
	if err != nil {
		return Snapshot{}, err
	}
	snap, err := s.snap.Snapshot(ctx, tc.OrgID, tc.ProjectID, p.ID)
	if err != nil {
		span.RecordError(err)
		return Snapshot{}, s.unexpected(ctx, "period.PreviewClose: snapshot", err, "period_id", id)
	}
	return snap, nil
}

// Close freezes the identified period's totals and, on a first close, opens the next one the following day.
func (s *Service) Close(ctx context.Context, id uuid.UUID, end civil.Date, by uuid.UUID) (*Period, error) {
	ctx, span := tracer.Start(ctx, "period.Close",
		trace.WithAttributes(attribute.String("period.id", id.String())))
	defer span.End()

	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := s.closable(ctx, tc, id, end); err != nil {
		span.RecordError(err)
		return nil, err
	}
	closedAt := s.now().UTC()
	var closed *Period
	var snapErr error
	txErr := s.uow(ctx, func(ctx context.Context) error {
		// NOTE: the guard is re-read under a row lock here, so a second request for the same close cannot pass it on a stale row and append a duplicate closing.
		p, err := s.closableLocked(ctx, tc, id, end)
		if err != nil {
			return err
		}
		firstClose := p.EndDate == nil
		// NOTE: computed inside the unit of work so the window between freezing the totals and committing the close is the transaction's width, not unbounded; READ COMMITTED does not close it.
		snap, err := s.snap.Snapshot(ctx, tc.OrgID, tc.ProjectID, p.ID)
		if err != nil {
			snapErr = err
			return err
		}
		if firstClose {
			endCopy := end
			p.EndDate = &endCopy
		}
		p.Status = StatusClosed
		p.ClosedAt = &closedAt
		snapCopy := snap
		p.Snapshot = &snapCopy
		p.UpdatedAt = closedAt
		if err := s.store.Save(ctx, p); err != nil {
			return err
		}
		closing := &Closing{
			ID:        uuid.Must(uuid.NewV7()),
			OrgID:     p.OrgID,
			ProjectID: p.ProjectID,
			PeriodID:  p.ID,
			ClosedAt:  closedAt,
			ClosedBy:  by,
			Snapshot:  snap,
		}
		if err := s.store.SaveClosing(ctx, closing); err != nil {
			return err
		}
		closed = p
		if !firstClose {
			return nil
		}
		next, err := New(p.OrgID, p.ProjectID, end.AddDays(1), "")
		if err != nil {
			return err
		}
		return s.store.Save(ctx, next)
	})
	if txErr != nil {
		span.RecordError(txErr)
		if snapErr != nil {
			return nil, s.unexpected(ctx, "period.Close: snapshot", snapErr, "period_id", id)
		}
		if isDomainError(txErr) {
			return nil, txErr
		}
		return nil, s.unexpected(ctx, "period.Close: commit", txErr, "period_id", id)
	}
	return closed, nil
}

// Reopen unlocks the most recently closed period, keeping its end date and now-stale snapshot.
func (s *Service) Reopen(ctx context.Context, id uuid.UUID) (*Period, error) {
	ctx, span := tracer.Start(ctx, "period.Reopen",
		trace.WithAttributes(attribute.String("period.id", id.String())))
	defer span.End()

	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, err
	}
	p, err := s.scoped(ctx, tc, id)
	if err != nil {
		return nil, err
	}
	if !p.IsLocked() {
		return nil, &NotClosedError{ID: id.String()}
	}
	siblings, err := s.store.List(ctx, tc.OrgID, tc.ProjectID, ListOpts{})
	if err != nil {
		span.RecordError(err)
		return nil, s.unexpected(ctx, "period.Reopen: list", err, "period_id", id)
	}
	latest := LatestClosed(siblings)
	if latest == nil || latest.ID != p.ID {
		return nil, &NotLatestClosedError{ID: id.String()}
	}
	p.Status = StatusOpen
	p.UpdatedAt = s.now().UTC()
	if saveErr := s.store.Save(ctx, p); saveErr != nil {
		span.RecordError(saveErr)
		if isDomainError(saveErr) {
			return nil, saveErr
		}
		return nil, s.unexpected(ctx, "period.Reopen: save", saveErr, "period_id", id)
	}
	return p, nil
}

// Containing returns the period holding at in the project timezone, ensuring the first period exists.
func (s *Service) Containing(ctx context.Context, at time.Time) (*Period, error) {
	ctx, span := tracer.Start(ctx, "period.Containing")
	defer span.End()

	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := s.EnsureCurrent(ctx); err != nil {
		return nil, err
	}
	loc, err := s.location(ctx, tc)
	if err != nil {
		return nil, err
	}
	p, err := s.store.Containing(ctx, tc.OrgID, tc.ProjectID, civil.DateOf(at, loc))
	if err != nil {
		span.RecordError(err)
		if IsNotFoundError(err) {
			return nil, err
		}
		return nil, s.unexpected(ctx, "period.Containing: containing", err,
			"org_id", tc.OrgID, "project_id", tc.ProjectID)
	}
	return p, nil
}

// Neighbors returns the periods immediately before and after the identified one.
func (s *Service) Neighbors(ctx context.Context, id uuid.UUID) (prev, next *Period, err error) { //nolint:nonamedreturns // mirrors the Store signature
	ctx, span := tracer.Start(ctx, "period.Neighbors",
		trace.WithAttributes(attribute.String("period.id", id.String())))
	defer span.End()

	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, nil, err
	}
	if _, err := s.scoped(ctx, tc, id); err != nil {
		return nil, nil, err
	}
	prev, next, err = s.store.Neighbors(ctx, tc.OrgID, tc.ProjectID, id)
	if err != nil {
		span.RecordError(err)
		if IsNotFoundError(err) {
			return nil, nil, err
		}
		return nil, nil, s.unexpected(ctx, "period.Neighbors: neighbors", err, "period_id", id)
	}
	return prev, next, nil
}

// Closings returns the identified period's closing history, newest first.
func (s *Service) Closings(ctx context.Context, id uuid.UUID) ([]*Closing, error) {
	ctx, span := tracer.Start(ctx, "period.Closings",
		trace.WithAttributes(attribute.String("period.id", id.String())))
	defer span.End()

	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := s.scoped(ctx, tc, id); err != nil {
		return nil, err
	}
	out, err := s.store.ListClosings(ctx, tc.OrgID, tc.ProjectID, id)
	if err != nil {
		span.RecordError(err)
		return nil, s.unexpected(ctx, "period.Closings: list", err, "period_id", id)
	}
	return out, nil
}

func (s *Service) scoped(ctx context.Context, tc tenant.Context, id uuid.UUID) (*Period, error) {
	return s.scopedRow(ctx, tc, id, s.store.ByID)
}

func (s *Service) scopedLocked(ctx context.Context, tc tenant.Context, id uuid.UUID) (*Period, error) {
	read := s.store.ByID
	if ls, ok := s.store.(LockingStore); ok {
		read = ls.ByIDLocked
	}
	return s.scopedRow(ctx, tc, id, read)
}

// SECURITY: the store filters by org only, so the project must be checked here or a sibling project's row is reachable.
func (s *Service) scopedRow(ctx context.Context, tc tenant.Context, id uuid.UUID, read rowReader) (*Period, error) {
	p, err := read(ctx, id)
	if err != nil {
		if IsNotFoundError(err) {
			return nil, err
		}
		return nil, s.unexpected(ctx, "period.scoped: byID", err, "period_id", id)
	}
	if p.OrgID != tc.OrgID || p.ProjectID != tc.ProjectID {
		return nil, &NotFoundError{ID: id.String()}
	}
	return p, nil
}

func (s *Service) closable(ctx context.Context, tc tenant.Context, id uuid.UUID, end civil.Date) (*Period, error) {
	p, err := s.scoped(ctx, tc, id)
	if err != nil {
		return nil, err
	}
	if checkErr := s.checkClosable(ctx, tc, p, end); checkErr != nil {
		return nil, checkErr
	}
	return p, nil
}

func (s *Service) closableLocked(ctx context.Context, tc tenant.Context, id uuid.UUID, end civil.Date) (*Period, error) {
	p, err := s.scopedLocked(ctx, tc, id)
	if err != nil {
		return nil, err
	}
	if checkErr := s.checkClosable(ctx, tc, p, end); checkErr != nil {
		return nil, checkErr
	}
	return p, nil
}

func (s *Service) checkClosable(ctx context.Context, tc tenant.Context, p *Period, end civil.Date) error {
	if p.IsLocked() {
		return &AlreadyClosedError{ID: p.ID.String()}
	}
	if p.EndDate != nil {
		if end.Compare(*p.EndDate) != 0 {
			return &InvalidRangeError{Reason: "end date is fixed after reopen"}
		}
		return nil
	}
	if end.Before(p.StartDate) {
		return &InvalidRangeError{Reason: "end date is before the start date"}
	}
	loc, err := s.location(ctx, tc)
	if err != nil {
		return err
	}
	if civil.DateOf(s.now(), loc).Before(end) {
		return &InvalidRangeError{Reason: "end date is in the future"}
	}
	return nil
}

func (s *Service) cycle(ctx context.Context, tc tenant.Context) (*time.Location, int, error) {
	loc, err := s.location(ctx, tc)
	if err != nil {
		return nil, 0, err
	}
	day, err := s.settings.StartDay(ctx, tc.OrgID, tc.ProjectID)
	if err != nil {
		return nil, 0, s.unexpected(ctx, "period.cycle: start day", err,
			"org_id", tc.OrgID, "project_id", tc.ProjectID)
	}
	return loc, day, nil
}

func (s *Service) location(ctx context.Context, tc tenant.Context) (*time.Location, error) {
	loc, err := s.settings.Location(ctx, tc.OrgID, tc.ProjectID)
	if err != nil {
		return nil, s.unexpected(ctx, "period.location: location", err,
			"org_id", tc.OrgID, "project_id", tc.ProjectID)
	}
	if loc == nil {
		return time.UTC, nil
	}
	return loc, nil
}

func (s *Service) currentAfterRace(ctx context.Context, tc tenant.Context) (*Period, error) {
	cur, err := s.store.Current(ctx, tc.OrgID, tc.ProjectID)
	if err != nil {
		if IsNotFoundError(err) {
			return nil, err
		}
		return nil, s.unexpected(ctx, "period.EnsureCurrent: reread after race", err,
			"org_id", tc.OrgID, "project_id", tc.ProjectID)
	}
	return cur, nil
}
