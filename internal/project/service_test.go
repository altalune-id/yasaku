package project_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/limits"
	"altalune.id/yasaku/internal/platform/authz"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/project"
	"altalune.id/yasaku/internal/testutil/fakes"
)

func newTestService(t *testing.T) (*project.Service, *fakes.Project) {
	t.Helper()
	store := fakes.NewProject()
	return project.NewService(store, discardLog(), failOnUnexpected(t), fakes.UnitOfWork), store
}

func discardLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func failOnUnexpected(t *testing.T) apperror.UnexpectedFunc {
	t.Helper()
	return func(_ context.Context, msg string, cause error, _ ...any) *apperror.AppError {
		t.Helper()
		t.Errorf("unexpected() called: %s: %v", msg, cause)
		return nil
	}
}

func tenantCtx(orgID, userID uuid.UUID) context.Context {
	return fakes.Owner(context.Background(), tenant.Context{OrgID: orgID, UserID: userID})
}

func TestService_Create_OK(t *testing.T) {
	orgID, userID := uuid.New(), uuid.New()
	svc, _ := newTestService(t)
	ctx := tenantCtx(orgID, userID)

	p, err := svc.Create(ctx, orgID, "web", "Web")
	if err != nil {
		t.Fatal(err)
	}
	if p.Slug != "web" || p.Name != "Web" || p.OrgID != orgID {
		t.Errorf("unexpected project: %+v", p)
	}
}

func TestService_Create_MissingTenant(t *testing.T) {
	svc, _ := newTestService(t)
	_, err := svc.Create(context.Background(), uuid.New(), "web", "Web")
	if !tenant.IsMissingError(err) {
		t.Errorf("want *tenant.MissingError, got %T (%v)", err, err)
	}
}

func TestService_Create_SlugTaken(t *testing.T) {
	orgID, userID := uuid.New(), uuid.New()
	svc, _ := newTestService(t)
	ctx := tenantCtx(orgID, userID)
	if _, err := svc.Create(ctx, orgID, "web", "Web"); err != nil {
		t.Fatal(err)
	}
	_, err := svc.Create(ctx, orgID, "web", "Web 2")
	if !project.IsAlreadyExistsError(err) {
		t.Errorf("want *AlreadyExistsError, got %T (%v)", err, err)
	}
}

func TestService_Create_InvalidSlug(t *testing.T) {
	orgID, userID := uuid.New(), uuid.New()
	svc, _ := newTestService(t)
	ctx := tenantCtx(orgID, userID)
	_, err := svc.Create(ctx, orgID, "BAD SLUG", "Name")
	if !project.IsInvalidSlugError(err) {
		t.Errorf("want *InvalidSlugError, got %T (%v)", err, err)
	}
}

func TestService_Create_InvalidName(t *testing.T) {
	orgID, userID := uuid.New(), uuid.New()
	svc, _ := newTestService(t)
	ctx := tenantCtx(orgID, userID)
	_, err := svc.Create(ctx, orgID, "abc", "   ")
	if !project.IsInvalidNameError(err) {
		t.Errorf("want *InvalidNameError, got %T (%v)", err, err)
	}
}

func TestService_List(t *testing.T) {
	orgID, userID := uuid.New(), uuid.New()
	svc, _ := newTestService(t)
	ctx := tenantCtx(orgID, userID)
	if _, err := svc.Create(ctx, orgID, "web", "Web"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, orgID, "api", "API"); err != nil {
		t.Fatal(err)
	}
	got, err := svc.List(ctx, orgID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("len=%d want 2", len(got))
	}
}

func TestService_List_MissingTenant(t *testing.T) {
	svc, _ := newTestService(t)
	_, err := svc.List(context.Background(), uuid.New())
	if !tenant.IsMissingError(err) {
		t.Errorf("want *tenant.MissingError, got %T", err)
	}
}

func TestService_Rename_OK(t *testing.T) {
	orgID, userID := uuid.New(), uuid.New()
	svc, _ := newTestService(t)
	ctx := tenantCtx(orgID, userID)
	p, err := svc.Create(ctx, orgID, "web", "Old")
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.Rename(ctx, p.ID, "New")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "New" {
		t.Errorf("name=%q want New", got.Name)
	}
	if got.Slug != "web" {
		t.Errorf("slug=%q must not change", got.Slug)
	}
}

func TestService_Rename_SystemProtected(t *testing.T) {
	orgID, userID := uuid.New(), uuid.New()
	svc, store := newTestService(t)
	ctx := tenantCtx(orgID, userID)
	p, err := svc.Create(ctx, orgID, "web", "Old")
	if err != nil {
		t.Fatal(err)
	}
	p.System = true
	if err := store.Save(ctx, p); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Rename(ctx, p.ID, "New"); !project.IsSystemProtectedError(err) {
		t.Fatalf("want *SystemProtectedError, got %T (%v)", err, err)
	}
}

func TestService_BootstrapSystem_CreatesAndPromotes(t *testing.T) {
	orgID, userID := uuid.New(), uuid.New()
	svc, _ := newTestService(t)
	ctx := tenantCtx(orgID, userID)
	p, err := svc.BootstrapSystem(ctx, orgID, "def", "Default")
	if err != nil {
		t.Fatal(err)
	}
	if !p.System {
		t.Error("System = false, want true on fresh bootstrap")
	}

	svc2, store2 := newTestService(t)
	ctx2 := tenantCtx(orgID, userID)
	prior, err := svc2.Create(ctx2, orgID, "def", "Default")
	if err != nil {
		t.Fatal(err)
	}
	if prior.System {
		t.Fatal("precondition: fresh non-system project")
	}
	got, err := svc2.BootstrapSystem(ctx2, orgID, "def", "Default")
	if err != nil {
		t.Fatal(err)
	}
	if !got.System {
		t.Error("BootstrapSystem did not promote existing project to system")
	}
	reload, err := store2.ByID(ctx2, prior.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reload.System {
		t.Error("existing project not persisted as system")
	}
}

func TestService_Rename_NotFound(t *testing.T) {
	orgID, userID := uuid.New(), uuid.New()
	svc, _ := newTestService(t)
	ctx := tenantCtx(orgID, userID)
	_, err := svc.Rename(ctx, uuid.New(), "New")
	if !project.IsNotFoundError(err) {
		t.Errorf("want *NotFoundError, got %T", err)
	}
}

func TestService_Rename_InvalidName(t *testing.T) {
	orgID, userID := uuid.New(), uuid.New()
	svc, _ := newTestService(t)
	ctx := tenantCtx(orgID, userID)
	p, err := svc.Create(ctx, orgID, "web", "Old")
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Rename(ctx, p.ID, "")
	if !project.IsInvalidNameError(err) {
		t.Errorf("want *InvalidNameError, got %T", err)
	}
}

func TestService_ByID(t *testing.T) {
	orgID, userID := uuid.New(), uuid.New()
	svc, _ := newTestService(t)
	ctx := tenantCtx(orgID, userID)
	created, err := svc.Create(ctx, orgID, "web", "Web")
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.ByID(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != created.ID {
		t.Errorf("id mismatch")
	}
	if _, err := svc.ByID(ctx, uuid.New()); !project.IsNotFoundError(err) {
		t.Errorf("want *NotFoundError, got %T", err)
	}
}

func TestService_BySlug(t *testing.T) {
	orgID, userID := uuid.New(), uuid.New()
	svc, _ := newTestService(t)
	ctx := tenantCtx(orgID, userID)
	created, err := svc.Create(ctx, orgID, "web", "Web")
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.BySlug(ctx, orgID, "web")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != created.ID {
		t.Errorf("id mismatch")
	}
	if _, err := svc.BySlug(ctx, orgID, "missing"); !project.IsNotFoundError(err) {
		t.Errorf("want *NotFoundError, got %T", err)
	}
}

func TestService_Unexpected_Wraps(t *testing.T) {
	orgID, userID := uuid.New(), uuid.New()
	sentinel := errors.New("boom")
	store := &erroringStore{err: sentinel}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	var called bool
	unexpected := func(_ context.Context, _ string, cause error, _ ...any) *apperror.AppError {
		called = true
		if !errors.Is(cause, sentinel) {
			t.Errorf("cause=%v want sentinel", cause)
		}
		return apperror.New(apperror.CodeUnexpectedError, "unexpected", 0)
	}
	svc := project.NewService(store, log, unexpected, fakes.UnitOfWork)
	ctx := tenantCtx(orgID, userID)
	if _, err := svc.List(ctx, orgID); err == nil {
		t.Fatal("expected error")
	}
	if !called {
		t.Error("unexpected() was not called")
	}
}

type erroringStore struct{ err error }

func (s *erroringStore) Save(context.Context, *project.Project) error { return s.err }
func (s *erroringStore) ByID(context.Context, uuid.UUID) (*project.Project, error) {
	return nil, s.err
}
func (s *erroringStore) BySlug(context.Context, uuid.UUID, string) (*project.Project, error) {
	return nil, s.err
}
func (s *erroringStore) List(context.Context, uuid.UUID) ([]*project.Project, error) {
	return nil, s.err
}

// SECURITY: membership is org-wide, so this gate is what keeps a plain member from creating or renaming projects on every surface.
func TestService_CreateAndRename_NeedProjectsWrite(t *testing.T) {
	store := fakes.NewProject()
	svc := project.NewService(store, discardLog(), failOnUnexpected(t), fakes.UnitOfWork)
	orgID, member, manager := uuid.New(), uuid.New(), uuid.New()
	memberCtx := fakes.Allow(context.Background(), tenant.Context{OrgID: orgID, UserID: member}, authz.Builtin("member")...)

	_, err := svc.Create(memberCtx, orgID, "side", "Side")
	if !authz.IsDeniedError(err) {
		t.Fatalf("member Create err = %v, want DeniedError", err)
	}
	items, err := svc.List(tenantCtx(orgID, manager), orgID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("a refused Create stored %d projects", len(items))
	}

	p, err := svc.Create(tenantCtx(orgID, manager), orgID, "side", "Side")
	if err != nil {
		t.Fatalf("manager Create: %v", err)
	}
	if _, err := svc.Rename(memberCtx, p.ID, "Mine"); !authz.IsDeniedError(err) {
		t.Fatalf("member Rename err = %v, want DeniedError", err)
	}
	got, err := svc.Rename(tenantCtx(orgID, manager), p.ID, "Ours")
	if err != nil {
		t.Fatalf("manager Rename: %v", err)
	}
	if got.Name != "Ours" {
		t.Fatalf("Name = %q, want Ours", got.Name)
	}
}

// SECURITY: the gate authorizes the tenant org, so a create naming another org is refused before it is written there.
func TestService_Create_RefusesAnOrgOtherThanTheTenants(t *testing.T) {
	store := fakes.NewProject()
	svc := project.NewService(store, discardLog(), failOnUnexpected(t), fakes.UnitOfWork)
	orgA, orgB, manager := uuid.New(), uuid.New(), uuid.New()

	_, err := svc.Create(tenantCtx(orgA, manager), orgB, "side", "Side")
	if !authz.IsDeniedError(err) {
		t.Fatalf("cross-org Create err = %v, want DeniedError: the held set belongs to the bound org", err)
	}
	items, err := svc.List(tenantCtx(orgB, manager), orgB)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("a refused cross-org Create stored %d projects in the other org", len(items))
	}
}

type txRecordingStore struct {
	*fakes.Project
	bySlugInTx, saveInTx bool
}

func (s *txRecordingStore) BySlug(ctx context.Context, orgID uuid.UUID, slug string) (*project.Project, error) {
	_, s.bySlugInTx = db.CurrentTx(ctx)
	return s.Project.BySlug(ctx, orgID, slug)
}

func (s *txRecordingStore) Save(ctx context.Context, p *project.Project) error {
	_, s.saveInTx = db.CurrentTx(ctx)
	return s.Project.Save(ctx, p)
}

func TestProjectCreate_RunsInUnitOfWork(t *testing.T) {
	store := &txRecordingStore{Project: fakes.NewProject()}
	svc := project.NewService(store, discardLog(), failOnUnexpected(t), fakes.UnitOfWork)
	orgID := uuid.New()

	if _, err := svc.Create(tenantCtx(orgID, uuid.New()), orgID, "home", "Home"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !store.bySlugInTx || !store.saveInTx {
		t.Fatalf("slug check and save must share one unit of work: bySlug=%v save=%v", store.bySlugInTx, store.saveInTx)
	}
}

func TestProjectCreate_FourthRefused(t *testing.T) {
	orgID := uuid.New()
	ctx := tenantCtx(orgID, uuid.New())
	lstore := fakes.NewLimits()
	svc := project.NewService(fakes.NewProject(), discardLog(), failOnUnexpected(t), fakes.UnitOfWork,
		project.WithLimits(limits.NewService(lstore, config.ModeCloud), orgNames{orgID: "Acme"}))
	for _, slug := range []string{"one", "two", "three"} {
		if _, err := svc.Create(ctx, orgID, slug, slug); err != nil {
			t.Fatalf("Create %s: %v", slug, err)
		}
	}
	_, err := svc.Create(ctx, orgID, "four", "Four")
	re, ok := errors.AsType[*limits.ReachedError](err)
	if !ok {
		t.Fatalf("the fourth project must hit org.projects, got %T: %v", err, err)
	}
	if re.Name != "Acme" {
		t.Fatalf("the refusal names the org, got %q", re.Name)
	}
	if len(lstore.Locked) != 4 {
		t.Fatalf("every create locks the org first, got %d locks", len(lstore.Locked))
	}
}

type orgNames map[uuid.UUID]string

func (n orgNames) OrgName(_ context.Context, orgID uuid.UUID) (string, error) { return n[orgID], nil }
