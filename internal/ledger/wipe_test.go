package ledger_test

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/ledger"
	"altalune.id/yasaku/internal/limits"
	"altalune.id/yasaku/internal/platform/authz"
	"altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/testutil/fakes"
)

type wipeSteps struct {
	mu    sync.Mutex
	steps []string
	inTx  map[string]bool
}

func (w *wipeSteps) add(ctx context.Context, step string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, tx := db.CurrentTx(ctx)
	w.steps = append(w.steps, step)
	w.inTx[step] = tx
}

func (w *wipeSteps) list() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.steps...)
}

type stepDeleter struct {
	name  string
	n     int64
	steps *wipeSteps
}

func (d stepDeleter) DeleteAllInProject(ctx context.Context) (int64, error) {
	d.steps.add(ctx, d.name)
	return d.n, nil
}

type stepPorts struct {
	steps         *wipeSteps
	name          string
	disconnectErr error
	revokeErr     error
	seedErr       error
	currentErr    error
	locked        []limits.Subject
}

func (p *stepPorts) ProjectName(context.Context) (string, error) { return p.name, nil }

func (p *stepPorts) PrepareRevoke(ctx context.Context) (func(context.Context) error, error) {
	p.steps.add(ctx, "prepare")
	if p.disconnectErr != nil {
		return nil, p.disconnectErr
	}
	return func(ctx context.Context) error {
		p.steps.add(ctx, "revoke")
		return p.revokeErr
	}, nil
}

func (p *stepPorts) Unlink(ctx context.Context) error {
	p.steps.add(ctx, "unlink")
	return nil
}

func (p *stepPorts) Lock(ctx context.Context, subs ...limits.Subject) error {
	p.steps.add(ctx, "lock")
	p.locked = append(p.locked, subs...)
	return nil
}

func (p *stepPorts) SeedDefaults(ctx context.Context) (int, error) {
	p.steps.add(ctx, "seed")
	return 9, p.seedErr
}

func (p *stepPorts) EnsureCurrent(ctx context.Context) error {
	p.steps.add(ctx, "current")
	return p.currentErr
}

type wipeEnv struct {
	wf    *ledger.WipeWorkflow
	ports *stepPorts
	audit *fakes.Audit
	tc    tenant.Context
}

func newWipeEnv(t *testing.T) *wipeEnv {
	t.Helper()
	steps := &wipeSteps{inTx: map[string]bool{}}
	ports := &stepPorts{steps: steps, name: "Alpha"}
	rec := &fakes.Audit{}
	wf := ledger.NewWipeWorkflow(ledger.WipeDeps{
		Projects:     ports,
		Opensheet:    ports,
		Limits:       ports,
		Transactions: stepDeleter{name: "transactions", n: 7, steps: steps},
		Periods:      stepDeleter{name: "periods", n: 3, steps: steps},
		Wallets:      stepDeleter{name: "wallets", n: 2, steps: steps},
		Categories:   stepDeleter{name: "categories", n: 11, steps: steps},
		Seeder:       ports,
		Current:      ports,
		Audit:        rec,
		UnitOfWork:   fakes.UnitOfWork,
	}, slog.New(slog.DiscardHandler), passthrough)
	return &wipeEnv{wf: wf, ports: ports, audit: rec, tc: tenant.Context{OrgID: uuid.New(), ProjectID: uuid.New(), UserID: uuid.New()}}
}

func passthrough(_ context.Context, _ string, err error, _ ...any) *apperror.AppError {
	return apperror.New("yasaku.unexpected", err.Error(), codes.Internal, nil).WithCause(err)
}

func TestWipe_OwnerOnly(t *testing.T) {
	for _, tt := range []struct {
		name string
		ctx  func(tenant.Context) context.Context
	}{
		{"admin", func(tc tenant.Context) context.Context {
			return fakes.AsRole(t.Context(), tc, "admin", authz.Builtin("admin")...)
		}},
		{"custom role holding every permission", func(tc tenant.Context) context.Context {
			return fakes.AsRole(t.Context(), tc, "custom", authz.Builtin("owner")...)
		}},
		{"owner of another org", func(tc tenant.Context) context.Context {
			other := tc
			other.OrgID = uuid.New()
			return tenant.Into(fakes.Owner(t.Context(), other), tc)
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e := newWipeEnv(t)
			_, err := e.wf.Run(tt.ctx(e.tc), "Alpha")
			require.True(t, ledger.IsWipeOwnerOnlyError(err), "got %v", err)
			app, ok := apperror.AsAppError(err)
			require.True(t, ok)
			require.Equal(t, codes.PermissionDenied, app.GRPCCode())
			require.Empty(t, e.ports.steps.list(), "a refused wipe touches nothing")
		})
	}
}

func TestWipe_ConfirmMismatch(t *testing.T) {
	for _, confirm := range []string{"", "alpha", "Alph", "Alpha!"} {
		e := newWipeEnv(t)
		_, err := e.wf.Run(fakes.Owner(t.Context(), e.tc), confirm)
		require.True(t, ledger.IsWipeConfirmMismatchError(err), "confirm %q: got %v", confirm, err)
		app, ok := apperror.AsAppError(err)
		require.True(t, ok)
		require.Equal(t, apperror.CodeDataWipeConfirmMismatch, app.Code())
		require.Equal(t, "DAT001", app.Code())
		require.Equal(t, codes.InvalidArgument, app.GRPCCode())
		require.Empty(t, e.ports.steps.list())
	}
}

func TestWipe_UnlinksOpensheetInTheWipeTransaction(t *testing.T) {
	e := newWipeEnv(t)
	counts, err := e.wf.Run(fakes.Owner(t.Context(), e.tc), " Alpha ")
	require.NoError(t, err)
	require.Equal(t, ledger.WipeCounts{Transactions: 7, Periods: 3, Wallets: 2, Categories: 11}, counts)
	require.Equal(t, []string{"prepare", "lock", "transactions", "periods", "wallets", "categories", "unlink", "revoke", "seed", "current"}, e.ports.steps.list())
	require.False(t, e.ports.steps.inTx["prepare"], "the grant is read before the wipe transaction")
	for _, s := range []string{"lock", "transactions", "periods", "wallets", "categories", "unlink"} {
		require.True(t, e.ports.steps.inTx[s], "%s runs in the wipe transaction", s)
	}
	require.False(t, e.ports.steps.inTx["revoke"], "the revoke runs after the commit")
	require.False(t, e.ports.steps.inTx["seed"], "the reseed runs after the commit")
	require.False(t, e.ports.steps.inTx["current"], "the current period is made after the commit")
	require.Equal(t, []limits.Subject{{Kind: limits.SubjectProject, ID: e.tc.ProjectID}}, e.ports.locked)

	failing := newWipeEnv(t)
	failing.ports.disconnectErr = errors.New("opensheet down")
	_, err = failing.wf.Run(fakes.Owner(t.Context(), failing.tc), "Alpha")
	require.Error(t, err)
	require.Equal(t, []string{"prepare"}, failing.ports.steps.list(), "nothing goes when the grant cannot be read")

	revoke := newWipeEnv(t)
	revoke.ports.revokeErr = errors.New("revoke refused")
	_, err = revoke.wf.Run(fakes.Owner(t.Context(), revoke.tc), "Alpha")
	require.NoError(t, err, "a failed revoke after the commit is logged, never a failed wipe")
	require.Contains(t, revoke.ports.steps.list(), "current")
}

func TestWipe_Audited(t *testing.T) {
	e := newWipeEnv(t)
	_, err := e.wf.Run(fakes.Owner(t.Context(), e.tc), "Alpha")
	require.NoError(t, err)
	recs := e.audit.Records()
	require.Len(t, recs, 1)
	r := recs[0]
	require.Equal(t, "project.wiped", r.Action)
	require.True(t, r.InTx)
	require.Equal(t, e.tc.OrgID, r.OrgID)
	require.Equal(t, e.tc.ProjectID, r.ProjectID)
	require.Equal(t, "project", r.TargetKind)
	require.Equal(t, e.tc.ProjectID.String(), r.TargetID)
	require.Equal(t, map[string]string{"transactions": "7", "periods": "3", "wallets": "2", "categories": "11"}, r.Detail)
}

func TestWipe_SetupFailureAfterCommitStillReportsTheWipe(t *testing.T) {
	for name, set := range map[string]func(*stepPorts){
		"seed":    func(p *stepPorts) { p.seedErr = errors.New("seed down") },
		"current": func(p *stepPorts) { p.currentErr = errors.New("period down") },
	} {
		t.Run(name, func(t *testing.T) {
			e := newWipeEnv(t)
			set(e.ports)
			counts, err := e.wf.Run(fakes.Owner(t.Context(), e.tc), "Alpha")
			require.True(t, ledger.IsWipeSetupIncompleteError(err), "got %v", err)
			require.Equal(t, int64(7), counts.Transactions, "the counts of the committed wipe come back")
			require.Equal(t, []string{"project.wiped"}, e.audit.Actions())
		})
	}
}
