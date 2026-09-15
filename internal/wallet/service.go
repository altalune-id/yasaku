package wallet

import (
	"context"
	"log/slog"
	"slices"
	"strings"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/money"
)

//nolint:gochecknoglobals // OTel tracer is a package-level fixture, not runtime state.
var tracer = otel.Tracer("altalune.id/yasaku/internal/wallet")

// Service is the wallets driving port.
type Service struct {
	store      Store
	log        *slog.Logger
	unexpected apperror.UnexpectedFunc
}

// NewService binds the service to its dependencies.
func NewService(store Store, log *slog.Logger, unexpected apperror.UnexpectedFunc) *Service {
	return &Service{store: store, log: log.With("module", "wallet"), unexpected: unexpected}
}

// Create constructs a wallet in the caller's tenant scope and persists it.
func (s *Service) Create(ctx context.Context, p Params) (*Wallet, error) {
	ctx, span := tracer.Start(ctx, "wallet.Create")
	defer span.End()

	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, err
	}
	span.SetAttributes(
		attribute.String("org_id", tc.OrgID.String()),
		attribute.String("project_id", tc.ProjectID.String()),
	)

	cur, err := money.ParseCurrency(string(p.Currency))
	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	p.Currency = cur

	w, err := New(tc.OrgID, tc.ProjectID, p)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	if err := s.save(ctx, w, "wallet.Create"); err != nil {
		span.RecordError(err)
		return nil, err
	}
	span.SetAttributes(attribute.String("wallet.id", w.ID.String()))
	return w, nil
}

// Update replaces the kind, provider and exclude-from-total flag of a wallet in scope.
func (s *Service) Update(ctx context.Context, id uuid.UUID, kind Kind, provider string, exclude bool) (*Wallet, error) {
	ctx, span := tracer.Start(ctx, "wallet.Update",
		trace.WithAttributes(attribute.String("wallet.id", id.String())))
	defer span.End()

	w, err := s.ByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := w.Update(kind, provider, exclude); err != nil {
		span.RecordError(err)
		return nil, err
	}
	if err := s.save(ctx, w, "wallet.Update"); err != nil {
		span.RecordError(err)
		return nil, err
	}
	return w, nil
}

// Rename changes the display name of a wallet in scope.
func (s *Service) Rename(ctx context.Context, id uuid.UUID, name string) (*Wallet, error) {
	ctx, span := tracer.Start(ctx, "wallet.Rename",
		trace.WithAttributes(attribute.String("wallet.id", id.String())))
	defer span.End()

	w, err := s.ByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := w.Rename(name); err != nil {
		span.RecordError(err)
		return nil, err
	}
	if err := s.save(ctx, w, "wallet.Rename"); err != nil {
		span.RecordError(err)
		return nil, err
	}
	return w, nil
}

// Archive retires a wallet in scope, freeing its name for reuse.
func (s *Service) Archive(ctx context.Context, id uuid.UUID) (*Wallet, error) {
	ctx, span := tracer.Start(ctx, "wallet.Archive",
		trace.WithAttributes(attribute.String("wallet.id", id.String())))
	defer span.End()

	w, err := s.ByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if w.IsArchived() {
		return w, nil
	}
	w.Archive()
	if err := s.save(ctx, w, "wallet.Archive"); err != nil {
		span.RecordError(err)
		return nil, err
	}
	return w, nil
}

// Unarchive returns a wallet to active use; the freed name may since have been taken.
func (s *Service) Unarchive(ctx context.Context, id uuid.UUID) (*Wallet, error) {
	ctx, span := tracer.Start(ctx, "wallet.Unarchive",
		trace.WithAttributes(attribute.String("wallet.id", id.String())))
	defer span.End()

	w, err := s.ByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if !w.IsArchived() {
		return w, nil
	}
	w.Unarchive()
	if err := s.save(ctx, w, "wallet.Unarchive"); err != nil {
		span.RecordError(err)
		return nil, err
	}
	return w, nil
}

// Delete removes a wallet in scope; one still referenced by transactions is refused as InUseError.
func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	ctx, span := tracer.Start(ctx, "wallet.Delete",
		trace.WithAttributes(attribute.String("wallet.id", id.String())))
	defer span.End()

	// SECURITY: load first, so the org-and-project check runs before the row is destroyed.
	if _, err := s.ByID(ctx, id); err != nil {
		return err
	}
	if err := s.store.Delete(ctx, id); err != nil {
		span.RecordError(err)
		if IsInUseError(err) || IsNotFoundError(err) {
			return err
		}
		return s.unexpected(ctx, "wallet.Delete: delete", err, "wallet_id", id)
	}
	return nil
}

// ByID returns the identified wallet when it belongs to the caller's tenant scope.
func (s *Service) ByID(ctx context.Context, id uuid.UUID) (*Wallet, error) {
	ctx, span := tracer.Start(ctx, "wallet.ByID",
		trace.WithAttributes(attribute.String("wallet.id", id.String())))
	defer span.End()

	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, err
	}
	w, err := s.store.ByID(ctx, id)
	if err != nil {
		span.RecordError(err)
		if IsNotFoundError(err) {
			return nil, err
		}
		return nil, s.unexpected(ctx, "wallet.ByID: byID", err, "wallet_id", id)
	}
	// SECURITY: the store scopes by org only; a sibling project's row is reported absent.
	if w.OrgID != tc.OrgID || w.ProjectID != tc.ProjectID {
		return nil, &NotFoundError{ID: id.String()}
	}
	return w, nil
}

// List returns the wallets in the caller's tenant scope ordered by name.
func (s *Service) List(ctx context.Context, opts ListOpts) ([]*Wallet, error) {
	ctx, span := tracer.Start(ctx, "wallet.List")
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
		return nil, s.unexpected(ctx, "wallet.List: list", err,
			"org_id", tc.OrgID, "project_id", tc.ProjectID)
	}
	return out, nil
}

// ResolveByName finds one active wallet by an exact case-insensitive name, else by a unique case-insensitive substring.
func (s *Service) ResolveByName(ctx context.Context, q string) (*Wallet, error) {
	ctx, span := tracer.Start(ctx, "wallet.ResolveByName")
	defer span.End()

	q = strings.TrimSpace(q)
	needle := strings.ToLower(q)
	if needle == "" {
		return nil, &NotFoundError{ID: q}
	}
	active, err := s.List(ctx, ListOpts{})
	if err != nil {
		return nil, err
	}

	var exact, partial []*Wallet
	for _, w := range active {
		name := strings.ToLower(w.Name)
		switch {
		case name == needle:
			exact = append(exact, w)
		case strings.Contains(name, needle):
			partial = append(partial, w)
		}
	}
	candidates := exact
	if len(candidates) == 0 {
		candidates = partial
	}
	switch len(candidates) {
	case 0:
		return nil, &NotFoundError{ID: q}
	case 1:
		return candidates[0], nil
	default:
		names := make([]string, 0, len(candidates))
		for _, w := range candidates {
			names = append(names, w.Name)
		}
		slices.Sort(names)
		err := &AmbiguousNameError{Query: q, Candidates: names}
		span.RecordError(err)
		return nil, err
	}
}

func (s *Service) save(ctx context.Context, w *Wallet, op string) error {
	if err := s.store.Save(ctx, w); err != nil {
		if IsAlreadyExistsError(err) || IsNotFoundError(err) {
			return err
		}
		return s.unexpected(ctx, op+": save", err, "wallet_id", w.ID)
	}
	return nil
}
