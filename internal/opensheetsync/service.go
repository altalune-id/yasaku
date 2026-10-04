package opensheetsync

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"

	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/platform/sealer"
	"altalune.id/yasaku/internal/platform/tenant"
)

const testTimeout = 10 * time.Second

// Members is the role gate: only an org owner or admin changes the link.
type Members interface {
	RequireManager(ctx context.Context, orgID, userID uuid.UUID) error
}

// UnitOfWork runs fn so that every write inside it commits or rolls back together.
type UnitOfWork func(ctx context.Context, fn func(ctx context.Context) error) error

// ServiceDeps is what Service needs beyond its store, logger and reporter.
type ServiceDeps struct {
	Mirror     *Mirror
	Members    Members
	Sealer     sealer.Sealer
	Endpoint   Endpoint
	UnitOfWork UnitOfWork
	Now        func() time.Time
}

// Service manages a project's opensheet link: the Test, Save, the Enabled switch, the backfill and removal.
type Service struct {
	store      Store
	log        *slog.Logger
	unexpected apperror.UnexpectedFunc
	mirror     *Mirror
	members    Members
	sealer     sealer.Sealer
	endpoint   Endpoint
	uow        UnitOfWork
	now        func() time.Time
}

// NewService binds the service to its dependencies; a nil UnitOfWork runs fn directly and a nil Now is time.Now.
func NewService(store Store, log *slog.Logger, unexpected apperror.UnexpectedFunc, d ServiceDeps) *Service {
	s := &Service{
		store: store, log: log.With("module", "opensheetsync"), unexpected: unexpected,
		mirror: d.Mirror, members: d.Members, sealer: d.Sealer, endpoint: d.Endpoint, uow: d.UnitOfWork, now: d.Now,
	}
	if s.uow == nil {
		s.uow = func(ctx context.Context, fn func(ctx context.Context) error) error { return fn(ctx) }
	}
	if s.now == nil {
		s.now = time.Now
	}
	return s
}

// Status is the module page's state: the link, nil when there is none, and the project's backlog.
type Status struct {
	Link *Link
	Backlog
}

// Status returns the project's link and backlog; any member may read it.
func (s *Service) Status(ctx context.Context) (Status, error) {
	ctx, span := tracer.Start(ctx, "opensheetsync.Status")
	defer span.End()

	tc, err := tenant.From(ctx)
	if err != nil {
		return Status{}, err
	}
	l, err := s.store.LinkByProject(ctx, tc.OrgID, tc.ProjectID)
	if IsLinkNotFoundError(err) {
		return Status{}, nil
	}
	if err != nil {
		return Status{}, s.unexpected(ctx, "opensheetsync.Status: link", err, "project_id", tc.ProjectID)
	}
	b, err := s.store.Backlog(ctx, tc.OrgID, tc.ProjectID)
	if err != nil {
		return Status{}, s.unexpected(ctx, "opensheetsync.Status: backlog", err, "project_id", tc.ProjectID)
	}
	return Status{Link: l, Backlog: b}, nil
}

// Test checks every tab against opensheet's /capabilities; failing tabs are in the checklist, not the error.
func (s *Service) Test(ctx context.Context, in Settings) (Checklist, error) {
	ctx, span := tracer.Start(ctx, "opensheetsync.Test")
	defer span.End()

	tc, err := s.manager(ctx)
	if err != nil {
		return nil, err
	}
	in, _, err = s.resolve(ctx, tc, in)
	if err != nil {
		return nil, err
	}
	return s.check(ctx, tc, in)
}

// Save re-runs the Test on the server and stores the settings only when every tab passes; an empty key keeps the saved one.
func (s *Service) Save(ctx context.Context, in Settings) (*Link, Checklist, error) {
	ctx, span := tracer.Start(ctx, "opensheetsync.Save")
	defer span.End()

	tc, err := s.manager(ctx)
	if err != nil {
		return nil, nil, err
	}
	entered := in.Normalized().APIKey != ""
	in, existing, err := s.resolve(ctx, tc, in)
	if err != nil {
		return nil, nil, err
	}
	cl, err := s.check(ctx, tc, in)
	if err != nil {
		return nil, nil, err
	}
	if !cl.OK() {
		span.SetAttributes(attribute.Bool("opensheetsync.refused", true))
		return nil, cl, cl.Err()
	}
	var sealed []byte
	hint := ""
	if entered {
		if sealed, err = sealKey(s.sealer, tc.OrgID, tc.ProjectID, in.APIKey); err != nil {
			return nil, nil, s.passOrReport(ctx, "opensheetsync.Save: seal", err, tc)
		}
		hint = KeyHint(in.APIKey)
	}
	at := s.stamp(existing)
	l := existing
	if l == nil {
		l = NewLink(tc.OrgID, tc.ProjectID, tc.UserID, uuid.Nil, at)
	}
	backfill := l.Enabled && l.Retargets(in)
	l.Configure(in, sealed, hint, at)
	if err := s.persist(ctx, l, backfill, at); err != nil {
		return nil, nil, s.passOrReport(ctx, "opensheetsync.Save: save", err, tc)
	}
	if backfill {
		s.mirror.KickDirty(ctx)
	}
	return l, cl, nil
}

// SetEnabled turns the mirror on or off; turning it on needs a verified link with a readable key and marks every row for a backfill.
func (s *Service) SetEnabled(ctx context.Context, on bool) (*Link, error) {
	ctx, span := tracer.Start(ctx, "opensheetsync.SetEnabled")
	defer span.End()
	span.SetAttributes(attribute.Bool("opensheetsync.enabled", on))

	tc, err := s.manager(ctx)
	if err != nil {
		return nil, err
	}
	l, err := s.link(ctx, tc)
	if err != nil {
		return nil, err
	}
	if l.Enabled == on {
		return l, nil
	}
	at := s.stamp(l)
	if !on {
		l.Disable(at)
		if err := s.store.SaveLink(ctx, l); err != nil {
			return nil, s.passOrReport(ctx, "opensheetsync.SetEnabled: save", err, tc)
		}
		return l, nil
	}
	if err := l.Enable(at); err != nil {
		return nil, err
	}
	if _, err := s.savedKey(ctx, tc, l); err != nil {
		return nil, err
	}
	if err := s.persist(ctx, l, true, at); err != nil {
		return nil, s.passOrReport(ctx, "opensheetsync.SetEnabled: save", err, tc)
	}
	s.mirror.KickDirty(ctx)
	return l, nil
}

// SyncNow marks every wallet, category and transaction of the project; the follow-up jobs or the reconciler drain what the first job does not.
func (s *Service) SyncNow(ctx context.Context) (int64, error) {
	ctx, span := tracer.Start(ctx, "opensheetsync.SyncNow")
	defer span.End()

	tc, err := s.manager(ctx)
	if err != nil {
		return 0, err
	}
	l, err := s.link(ctx, tc)
	if err != nil {
		return 0, err
	}
	if !l.Enabled {
		return 0, &LinkDisabledError{}
	}
	n, err := s.store.MarkAll(ctx, tc.OrgID, tc.ProjectID, s.now())
	if err != nil {
		return 0, s.unexpected(ctx, "opensheetsync.SyncNow: mark all", err, "project_id", tc.ProjectID)
	}
	s.mirror.KickDirty(ctx)
	span.SetAttributes(attribute.Int64("opensheetsync.marked", n))
	return n, nil
}

// Delete removes the link and its sync state; the sheet's rows are left alone.
func (s *Service) Delete(ctx context.Context) error {
	ctx, span := tracer.Start(ctx, "opensheetsync.Delete")
	defer span.End()

	tc, err := s.manager(ctx)
	if err != nil {
		return err
	}
	if err := s.store.DeleteLink(ctx, tc.OrgID, tc.ProjectID); err != nil {
		return s.passOrReport(ctx, "opensheetsync.Delete: delete", err, tc)
	}
	return nil
}

// SECURITY: an API key principal has no user, so RequireManager refuses it; a leaked key must not re-point the mirror.
func (s *Service) manager(ctx context.Context) (tenant.Context, error) {
	tc, err := tenant.From(ctx)
	if err != nil {
		return tenant.Context{}, err
	}
	if err := s.members.RequireManager(ctx, tc.OrgID, tc.UserID); err != nil {
		return tenant.Context{}, s.passOrReport(ctx, "opensheetsync: role", err, tc)
	}
	return tc, nil
}

func (s *Service) link(ctx context.Context, tc tenant.Context) (*Link, error) {
	l, err := s.store.LinkByProject(ctx, tc.OrgID, tc.ProjectID)
	if err != nil {
		return nil, s.passOrReport(ctx, "opensheetsync: link", err, tc)
	}
	return l, nil
}

func (s *Service) resolve(ctx context.Context, tc tenant.Context, in Settings) (Settings, *Link, error) {
	in = in.Normalized()
	existing, err := s.store.LinkByProject(ctx, tc.OrgID, tc.ProjectID)
	if IsLinkNotFoundError(err) {
		existing, err = nil, nil
	}
	if err != nil {
		return in, nil, s.unexpected(ctx, "opensheetsync: link", err, "project_id", tc.ProjectID)
	}
	if err := in.Validate(existing == nil); err != nil {
		return in, existing, err
	}
	if in.APIKey != "" {
		return in, existing, nil
	}
	key, err := s.savedKey(ctx, tc, existing)
	if err != nil {
		return in, existing, err
	}
	in.APIKey = key
	return in, existing, nil
}

func (s *Service) savedKey(ctx context.Context, tc tenant.Context, l *Link) (string, error) {
	key, err := openKey(s.sealer, l)
	if sealer.IsOpenFailedError(err) {
		s.log.WarnContext(ctx, "opensheetsync: the saved API key cannot be opened; it must be entered again",
			slog.String("project_id", tc.ProjectID.String()))
		return "", &KeyUnreadableError{Cause: err}
	}
	if err != nil {
		return "", s.passOrReport(ctx, "opensheetsync: open key", err, tc)
	}
	return key, nil
}

// NOTE: one goroutine per tab, owned here and waited for before returning; each writes only its own slot.
func (s *Service) check(ctx context.Context, tc tenant.Context, in Settings) (Checklist, error) {
	c, err := s.endpoint.client(in.OSOrg, in.OSProject, in.APIKey)
	if err != nil {
		return nil, s.unexpected(ctx, "opensheetsync.Test: client", err, "org_id", tc.OrgID, "project_id", tc.ProjectID)
	}
	ctx, cancel := context.WithTimeout(ctx, testTimeout)
	defer cancel()
	tabs := Contract()
	out := make(Checklist, len(tabs))
	var wg sync.WaitGroup
	for i, tab := range tabs {
		wg.Go(func() {
			slug := in.Sheets.For(tab.Entity)
			caps, err := c.Capabilities(ctx, slug)
			out[i] = CheckTab(tab, slug, caps, err)
		})
	}
	wg.Wait()
	return out, nil
}

func (s *Service) persist(ctx context.Context, l *Link, backfill bool, at time.Time) error {
	return s.uow(ctx, func(ctx context.Context) error {
		if err := s.store.SaveLink(ctx, l); err != nil {
			return err
		}
		if !backfill {
			return nil
		}
		_, err := s.store.MarkAll(ctx, l.OrgID, l.ProjectID, at)
		return err
	})
}

// NOTE: every change moves updated_at strictly forward, at the database's microsecond precision, so SaveOutcome ignores a job that loaded the link before it.
func (s *Service) stamp(prior *Link) time.Time {
	at := s.now().UTC().Truncate(time.Microsecond)
	if prior != nil && !at.After(prior.UpdatedAt) {
		return prior.UpdatedAt.UTC().Add(time.Microsecond)
	}
	return at
}

func (s *Service) passOrReport(ctx context.Context, op string, err error, tc tenant.Context) error {
	if _, ok := apperror.AsAppError(err); ok {
		return err
	}
	return s.unexpected(ctx, op, err, "org_id", tc.OrgID, "project_id", tc.ProjectID)
}
