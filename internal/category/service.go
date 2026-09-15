package category

import (
	"context"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/platform/tenant"
)

//nolint:gochecknoglobals // OTel tracer is a package-level fixture, not runtime state.
var tracer = otel.Tracer("altalune.id/yasaku/internal/category")

// Service is the transaction categories driving port.
type Service struct {
	store      Store
	log        *slog.Logger
	unexpected apperror.UnexpectedFunc
	namer      Namer
}

// NewService binds the service to its dependencies.
func NewService(store Store, log *slog.Logger, unexpected apperror.UnexpectedFunc, namer Namer) *Service {
	return &Service{
		store:      store,
		log:        log.With("module", "category"),
		unexpected: unexpected,
		namer:      namer,
	}
}

// Create constructs a Category in the caller's tenant scope and persists it.
func (s *Service) Create(ctx context.Context, name string, kind Kind, icon, color string) (*Category, error) {
	ctx, span := tracer.Start(ctx, "category.Create")
	defer span.End()

	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, err
	}
	span.SetAttributes(
		attribute.String("org_id", tc.OrgID.String()),
		attribute.String("project_id", tc.ProjectID.String()),
	)
	if kind != KindExpense && kind != KindIncome {
		return nil, &InvalidKindError{Value: string(kind)}
	}

	sortOrder, err := s.nextSortOrder(ctx, tc, kind)
	if err != nil {
		return nil, err
	}

	c, err := New(tc.OrgID, tc.ProjectID, name, kind, icon, color, sortOrder)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	if err := s.store.Save(ctx, c); err != nil {
		span.RecordError(err)
		if IsAlreadyExistsError(err) {
			return nil, err
		}
		return nil, s.unexpected(ctx, "category.Create: save", err,
			"org_id", tc.OrgID, "project_id", tc.ProjectID)
	}
	span.SetAttributes(attribute.String("category.id", c.ID.String()))
	return c, nil
}

// Rename changes the display name of a category in the caller's tenant scope.
func (s *Service) Rename(ctx context.Context, id uuid.UUID, name string) (*Category, error) {
	ctx, span := tracer.Start(ctx, "category.Rename",
		trace.WithAttributes(attribute.String("category.id", id.String())))
	defer span.End()

	c, err := s.ByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := c.Rename(name); err != nil {
		span.RecordError(err)
		return nil, err
	}
	return s.save(ctx, span, c, "category.Rename")
}

// Update replaces the icon and colour of a category in the caller's tenant scope.
func (s *Service) Update(ctx context.Context, id uuid.UUID, icon, color string) (*Category, error) {
	ctx, span := tracer.Start(ctx, "category.Update",
		trace.WithAttributes(attribute.String("category.id", id.String())))
	defer span.End()

	c, err := s.ByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := c.Update(icon, color); err != nil {
		span.RecordError(err)
		return nil, err
	}
	return s.save(ctx, span, c, "category.Update")
}

// Archive hides the category from pickers, leaving its transactions intact.
func (s *Service) Archive(ctx context.Context, id uuid.UUID) (*Category, error) {
	ctx, span := tracer.Start(ctx, "category.Archive",
		trace.WithAttributes(attribute.String("category.id", id.String())))
	defer span.End()

	c, err := s.ByID(ctx, id)
	if err != nil {
		return nil, err
	}
	c.Archive()
	return s.save(ctx, span, c, "category.Archive")
}

// Unarchive returns the category to the pickers.
func (s *Service) Unarchive(ctx context.Context, id uuid.UUID) (*Category, error) {
	ctx, span := tracer.Start(ctx, "category.Unarchive",
		trace.WithAttributes(attribute.String("category.id", id.String())))
	defer span.End()

	c, err := s.ByID(ctx, id)
	if err != nil {
		return nil, err
	}
	c.Unarchive()
	return s.save(ctx, span, c, "category.Unarchive")
}

// Delete removes a category that no transaction references.
func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	ctx, span := tracer.Start(ctx, "category.Delete",
		trace.WithAttributes(attribute.String("category.id", id.String())))
	defer span.End()

	if _, err := s.ByID(ctx, id); err != nil {
		return err
	}
	if err := s.store.Delete(ctx, id); err != nil {
		span.RecordError(err)
		if IsInUseError(err) || IsNotFoundError(err) {
			return err
		}
		return s.unexpected(ctx, "category.Delete: delete", err, "category_id", id)
	}
	return nil
}

// ByID returns the identified category when it belongs to the caller's org and project.
func (s *Service) ByID(ctx context.Context, id uuid.UUID) (*Category, error) {
	ctx, span := tracer.Start(ctx, "category.ByID",
		trace.WithAttributes(attribute.String("category.id", id.String())))
	defer span.End()

	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, err
	}
	c, err := s.store.ByID(ctx, id)
	if err != nil {
		span.RecordError(err)
		if IsNotFoundError(err) {
			return nil, err
		}
		return nil, s.unexpected(ctx, "category.ByID: byID", err, "category_id", id)
	}
	// SECURITY: the store filters by org, not project; without this a sibling project's row is reachable. Reported as absent so the caller learns nothing about rows outside its scope.
	if c.OrgID != tc.OrgID || c.ProjectID != tc.ProjectID {
		return nil, &NotFoundError{ID: id.String()}
	}
	return c, nil
}

// List returns categories in the caller's tenant scope, ordered by sort order then name.
func (s *Service) List(ctx context.Context, opts ListOpts) ([]*Category, error) {
	ctx, span := tracer.Start(ctx, "category.List")
	defer span.End()

	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, err
	}
	span.SetAttributes(
		attribute.String("org_id", tc.OrgID.String()),
		attribute.String("project_id", tc.ProjectID.String()),
	)
	out, err := s.store.List(ctx, tc.OrgID, tc.ProjectID, opts)
	if err != nil {
		span.RecordError(err)
		return nil, s.unexpected(ctx, "category.List: list", err,
			"org_id", tc.OrgID, "project_id", tc.ProjectID)
	}
	return out, nil
}

// ResolveByName resolves a typed-in name to one active category of the given kind: exact case-insensitive match first, then a unique substring match.
func (s *Service) ResolveByName(ctx context.Context, kind Kind, q string) (*Category, error) {
	ctx, span := tracer.Start(ctx, "category.ResolveByName")
	defer span.End()

	if kind != KindExpense && kind != KindIncome {
		return nil, &InvalidKindError{Value: string(kind)}
	}
	needle := FoldName(q)
	if needle == "" {
		return nil, &InvalidNameError{Reason: "empty"}
	}

	rows, err := s.List(ctx, ListOpts{Kind: kind})
	if err != nil {
		return nil, err
	}

	var partial []*Category
	for _, c := range rows {
		folded := FoldName(c.Name)
		if folded == needle {
			return c, nil
		}
		if strings.Contains(folded, needle) {
			partial = append(partial, c)
		}
	}
	switch len(partial) {
	case 0:
		return nil, &NotFoundError{Name: strings.TrimSpace(q)}
	case 1:
		return partial[0], nil
	default:
		return nil, &AmbiguousNameError{Name: strings.TrimSpace(q), Matches: len(partial)}
	}
}

// SeedDefaults inserts every missing default category and reports how many it added.
func (s *Service) SeedDefaults(ctx context.Context) (int, error) {
	ctx, span := tracer.Start(ctx, "category.SeedDefaults")
	defer span.End()

	tc, err := tenant.From(ctx)
	if err != nil {
		return 0, err
	}
	span.SetAttributes(
		attribute.String("org_id", tc.OrgID.String()),
		attribute.String("project_id", tc.ProjectID.String()),
	)

	// NOTE: archived rows are included on purpose — idempotency by lower(name) across every
	// row stops a previously archived default from being duplicated or resurrected.
	existing, err := s.store.List(ctx, tc.OrgID, tc.ProjectID, ListOpts{IncludeArchived: true})
	if err != nil {
		span.RecordError(err)
		return 0, s.unexpected(ctx, "category.SeedDefaults: list", err,
			"org_id", tc.OrgID, "project_id", tc.ProjectID)
	}
	taken := make(map[Kind]map[string]bool, 2)
	// NOTE: sort order is assigned per kind, as Create does, or seeded income rows would share
	// the global index range with expense rows and interleave in an unfiltered List.
	next := make(map[Kind]int, 2)
	for _, c := range existing {
		if taken[c.Kind] == nil {
			taken[c.Kind] = map[string]bool{}
		}
		taken[c.Kind][FoldName(c.Name)] = true
		if c.SortOrder >= next[c.Kind] {
			next[c.Kind] = c.SortOrder + 1
		}
	}

	added := 0
	seen := make(map[Kind]int, 2)
	for _, d := range Defaults {
		sortOrder := next[d.Kind] + seen[d.Kind]
		seen[d.Kind]++

		name := s.defaultName(ctx, d)
		if taken[d.Kind] != nil && taken[d.Kind][FoldName(name)] {
			continue
		}
		c, nErr := New(tc.OrgID, tc.ProjectID, name, d.Kind, d.Icon, d.Color, sortOrder)
		if nErr != nil {
			span.RecordError(nErr)
			return added, s.unexpected(ctx, "category.SeedDefaults: build default", nErr,
				"org_id", tc.OrgID, "project_id", tc.ProjectID, "default_key", d.Key)
		}
		if sErr := s.store.Save(ctx, c); sErr != nil {
			// NOTE: a concurrent seeder inserted this default first; that is the idempotent
			// outcome this method promises, so skip it rather than abort with a partial set.
			if IsAlreadyExistsError(sErr) {
				continue
			}
			span.RecordError(sErr)
			return added, s.unexpected(ctx, "category.SeedDefaults: save", sErr,
				"org_id", tc.OrgID, "project_id", tc.ProjectID, "default_key", d.Key)
		}
		if taken[d.Kind] == nil {
			taken[d.Kind] = map[string]bool{}
		}
		taken[d.Kind][FoldName(name)] = true
		added++
	}
	span.SetAttributes(attribute.Int("category.seeded", added))
	return added, nil
}

func (s *Service) defaultName(ctx context.Context, d Default) string {
	var name string
	if s.namer != nil {
		name = strings.TrimSpace(s.namer.DefaultName(ctx, DefaultNameKey(d.Key)))
	}
	if name == "" {
		return titleCase(d.Key)
	}
	return name
}

func (s *Service) nextSortOrder(ctx context.Context, tc tenant.Context, kind Kind) (int, error) {
	rows, err := s.store.List(ctx, tc.OrgID, tc.ProjectID, ListOpts{Kind: kind, IncludeArchived: true})
	if err != nil {
		return 0, s.unexpected(ctx, "category.Create: list", err,
			"org_id", tc.OrgID, "project_id", tc.ProjectID)
	}
	next := 0
	for _, c := range rows {
		if c.SortOrder >= next {
			next = c.SortOrder + 1
		}
	}
	return next, nil
}

func (s *Service) save(ctx context.Context, span trace.Span, c *Category, op string) (*Category, error) {
	if err := s.store.Save(ctx, c); err != nil {
		span.RecordError(err)
		if IsAlreadyExistsError(err) || IsNotFoundError(err) {
			return nil, err
		}
		return nil, s.unexpected(ctx, op+": save", err, "category_id", c.ID)
	}
	return c, nil
}
