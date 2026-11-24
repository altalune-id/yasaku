package project

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/limits"
	"altalune.id/yasaku/internal/platform/authz"
	"altalune.id/yasaku/internal/platform/tenant"
	slugs "altalune.id/yasaku/slug"
)

//nolint:gochecknoglobals // OTel tracer is a package-level fixture, not runtime state.
var tracer trace.Tracer = otel.Tracer("altalune.id/yasaku/internal/project")

// Service is the projects driving port.
type Service struct {
	store      Store
	log        *slog.Logger
	unexpected apperror.UnexpectedFunc
	uow        tenant.UnitOfWork
	limits     Limiter
	orgNames   OrgNamer
}

// Limiter locks a subject's row and refuses a create past its limit.
type Limiter interface {
	Lock(ctx context.Context, subs ...limits.Subject) error
	Check(ctx context.Context, key limits.Key, sub limits.Subject, used int64) error
}

// OrgNamer names the org a limit refusal shows.
type OrgNamer interface {
	OrgName(ctx context.Context, orgID uuid.UUID) (string, error)
}

// Option configures a Service.
type Option func(*Service)

// WithLimits holds every Create to the org's project limit; names fills the org name a refusal shows.
func WithLimits(l Limiter, names OrgNamer) Option {
	return func(s *Service) { s.limits, s.orgNames = l, names }
}

// NewService binds the service to its dependencies.
func NewService(store Store, log *slog.Logger, unexpected apperror.UnexpectedFunc, uow tenant.UnitOfWork, opts ...Option) *Service {
	s := &Service{store: store, log: log.With("module", "project"), unexpected: unexpected, uow: uow}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Create constructs a Project inside orgID and persists it; an empty slug is generated.
func (s *Service) Create(ctx context.Context, orgID uuid.UUID, slug, name string) (*Project, error) {
	ctx, span := tracer.Start(ctx, "project.Create",
		trace.WithAttributes(
			attribute.String("org_id", orgID.String()),
			attribute.String("slug", slug),
		))
	defer span.End()

	if _, err := tenant.From(ctx); err != nil {
		return nil, err
	}
	if err := authz.Can(tenant.WithOrg(ctx, orgID), authz.ProjectsWrite); err != nil {
		span.RecordError(err)
		return nil, err
	}

	if chosen := strings.TrimSpace(slug); chosen != "" {
		return s.createInUnitOfWork(ctx, span, orgID, chosen, name)
	}
	return slugs.Retry(slugs.MaxAttempts, IsAlreadyExistsError, func(candidate string) (*Project, error) {
		return s.createInUnitOfWork(ctx, span, orgID, candidate, name)
	})
}

func (s *Service) createInUnitOfWork(ctx context.Context, span trace.Span, orgID uuid.UUID, chosen, name string) (*Project, error) {
	var p *Project
	err := s.uow(ctx, func(ctx context.Context) error {
		if err := s.checkProjectLimit(ctx, orgID); err != nil {
			return err
		}
		var err error
		p, err = s.createWithSlug(ctx, span, orgID, chosen, name, false)
		return err
	})
	if err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Service) createWithSlug(ctx context.Context, span trace.Span, orgID uuid.UUID, chosen, name string, system bool) (*Project, error) {
	_, err := s.store.BySlug(ctx, orgID, chosen)
	if err == nil {
		return nil, &AlreadyExistsError{Field: "slug", Value: chosen}
	}
	if !IsNotFoundError(err) {
		return nil, s.unexpected(ctx, "project.Create: bySlug", err,
			"org_id", orgID, "slug", chosen)
	}

	p, err := New(orgID, chosen, name)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	p.System = system
	if err := s.store.Save(ctx, p); err != nil {
		if IsAlreadyExistsError(err) || IsSystemProjectExistsError(err) {
			return nil, err
		}
		span.RecordError(err)
		return nil, s.unexpected(ctx, "project.Create: save", err,
			"org_id", orgID, "slug", chosen)
	}
	span.SetAttributes(attribute.String("project.id", p.ID.String()))
	return p, nil
}

func (s *Service) checkProjectLimit(ctx context.Context, orgID uuid.UUID) error {
	if s.limits == nil {
		return nil
	}
	sub := limits.Subject{Kind: limits.SubjectOrg, ID: orgID}
	if err := s.limits.Lock(ctx, sub); err != nil {
		return s.unexpected(ctx, "project.Create: limit lock", err, "org_id", orgID)
	}
	existing, err := s.store.List(ctx, orgID)
	if err != nil {
		return s.unexpected(ctx, "project.Create: count", err, "org_id", orgID)
	}
	err = s.limits.Check(ctx, limits.OrgProjects, sub, int64(len(existing)))
	re, ok := errors.AsType[*limits.ReachedError](err)
	if !ok {
		return err
	}
	if re.Name, err = s.orgNames.OrgName(ctx, orgID); err != nil {
		return s.unexpected(ctx, "project.Create: org name", err, "org_id", orgID)
	}
	return re
}

// BootstrapSystem idempotently ensures a project with the given slug exists inside orgID and is stamped System=true; a blank slug reuses the system project or generates one.
func (s *Service) BootstrapSystem(ctx context.Context, orgID uuid.UUID, slug, name string) (*Project, error) {
	ctx, span := tracer.Start(ctx, "project.BootstrapSystem",
		trace.WithAttributes(
			attribute.String("org_id", orgID.String()),
			attribute.String("slug", slug),
		))
	defer span.End()

	if _, err := tenant.From(ctx); err != nil {
		return nil, err
	}

	p, err := s.ensureSystem(ctx, span, orgID, strings.TrimSpace(slug), name)
	if !IsSystemProjectExistsError(err) && !IsAlreadyExistsError(err) {
		return p, err
	}
	winner, wErr := s.systemProject(ctx, span, orgID)
	if wErr != nil {
		return nil, wErr
	}
	if winner == nil {
		return nil, err
	}
	return winner, nil
}

func (s *Service) ensureSystem(ctx context.Context, span trace.Span, orgID uuid.UUID, slug, name string) (*Project, error) {
	existing, err := s.systemProject(ctx, span, orgID)
	if err != nil || existing != nil {
		return existing, err
	}
	if slug == "" {
		return slugs.Retry(slugs.MaxAttempts, IsAlreadyExistsError, func(candidate string) (*Project, error) {
			return s.createWithSlug(ctx, span, orgID, candidate, name, true)
		})
	}

	bySlug, err := s.store.BySlug(ctx, orgID, slug)
	if err == nil {
		bySlug.System = true
		if sErr := s.store.Save(ctx, bySlug); sErr != nil {
			if IsSystemProjectExistsError(sErr) {
				return nil, sErr
			}
			span.RecordError(sErr)
			return nil, s.unexpected(ctx, "project.BootstrapSystem: promote", sErr,
				"org_id", orgID, "slug", slug)
		}
		return bySlug, nil
	}
	if !IsNotFoundError(err) {
		return nil, s.unexpected(ctx, "project.BootstrapSystem: bySlug", err,
			"org_id", orgID, "slug", slug)
	}
	return s.createWithSlug(ctx, span, orgID, slug, name, true)
}

func (s *Service) systemProject(ctx context.Context, span trace.Span, orgID uuid.UUID) (*Project, error) {
	list, err := s.store.List(ctx, orgID)
	if err != nil {
		span.RecordError(err)
		return nil, s.unexpected(ctx, "project.BootstrapSystem: list", err, "org_id", orgID)
	}
	for _, p := range list {
		if p.System {
			return p, nil
		}
	}
	return nil, nil
}

// List returns every project inside orgID.
func (s *Service) List(ctx context.Context, orgID uuid.UUID) ([]*Project, error) {
	ctx, span := tracer.Start(ctx, "project.List",
		trace.WithAttributes(attribute.String("org_id", orgID.String())))
	defer span.End()

	if _, err := tenant.From(ctx); err != nil {
		return nil, err
	}
	out, err := s.store.List(ctx, orgID)
	if err != nil {
		span.RecordError(err)
		return nil, s.unexpected(ctx, "project.List: list", err, "org_id", orgID)
	}
	return out, nil
}

// Rename updates the display name on the identified project.
func (s *Service) Rename(ctx context.Context, id uuid.UUID, name string) (*Project, error) {
	ctx, span := tracer.Start(ctx, "project.Rename",
		trace.WithAttributes(attribute.String("project.id", id.String())))
	defer span.End()

	if _, err := tenant.From(ctx); err != nil {
		return nil, err
	}
	if err := authz.Can(ctx, authz.ProjectsWrite); err != nil {
		span.RecordError(err)
		return nil, err
	}
	p, err := s.store.ByID(ctx, id)
	if err != nil {
		if IsNotFoundError(err) {
			return nil, err
		}
		span.RecordError(err)
		return nil, s.unexpected(ctx, "project.Rename: byID", err, "project_id", id)
	}
	if p.System {
		return nil, &SystemProtectedError{Op: "rename", ProjectID: id.String()}
	}
	if err := p.Rename(name); err != nil {
		span.RecordError(err)
		return nil, err
	}
	if err := s.store.Save(ctx, p); err != nil {
		span.RecordError(err)
		return nil, s.unexpected(ctx, "project.Rename: save", err, "project_id", id)
	}
	return p, nil
}

// ByID looks up a project by its aggregate ID.
func (s *Service) ByID(ctx context.Context, id uuid.UUID) (*Project, error) {
	ctx, span := tracer.Start(ctx, "project.ByID",
		trace.WithAttributes(attribute.String("project.id", id.String())))
	defer span.End()

	if _, err := tenant.From(ctx); err != nil {
		return nil, err
	}
	p, err := s.store.ByID(ctx, id)
	if err != nil {
		if IsNotFoundError(err) {
			return nil, err
		}
		span.RecordError(err)
		return nil, s.unexpected(ctx, "project.ByID: byID", err, "project_id", id)
	}
	return p, nil
}

// BySlug looks up a project by (org, slug).
func (s *Service) BySlug(ctx context.Context, orgID uuid.UUID, slug string) (*Project, error) {
	ctx, span := tracer.Start(ctx, "project.BySlug",
		trace.WithAttributes(
			attribute.String("org_id", orgID.String()),
			attribute.String("slug", slug),
		))
	defer span.End()

	if _, err := tenant.From(ctx); err != nil {
		return nil, err
	}
	p, err := s.store.BySlug(ctx, orgID, slug)
	if err != nil {
		if IsNotFoundError(err) {
			return nil, err
		}
		span.RecordError(err)
		return nil, s.unexpected(ctx, "project.BySlug: bySlug", err,
			"org_id", orgID, "slug", slug)
	}
	return p, nil
}
