package boot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/category"
	"altalune.id/yasaku/internal/ledger"
	"altalune.id/yasaku/internal/opensheetsync"
	"altalune.id/yasaku/internal/period"
	"altalune.id/yasaku/internal/platform"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/transaction"
	"altalune.id/yasaku/internal/wallet"
)

// NOTE: opensheetWiring is the Opensheet module's mirror half, which the domain services need before the module's own services exist; zero when the module is unmounted.
type opensheetWiring struct {
	store    opensheetsync.Store
	mirror   *opensheetsync.Mirror
	endpoint opensheetsync.Endpoint
	jobs     opensheetsync.Queue
	queued   bool
}

// SECURITY: opensheet.baseURL is server config; no tenant input reaches the endpoint.
func newOpensheetWiring(cfg *config.Config, k *platform.Kernel, jobs opensheetsync.Queue, log *slog.Logger) (opensheetWiring, error) {
	if !cfg.Opensheet.Mounted() {
		return opensheetWiring{}, nil
	}
	if strings.TrimSpace(cfg.Security.EncryptionKey) == "" {
		return opensheetWiring{}, errors.New("boot: opensheet.baseURL is set but security.encryptionKey is empty, so the Opensheet API key cannot be sealed (set YASAKU_SECURITY_ENCRYPTION_KEY to 32 bytes hex or base64, or unset YASAKU_OPENSHEET_BASE_URL)")
	}
	base := strings.TrimSpace(cfg.Opensheet.BaseURL)
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return opensheetWiring{}, errors.New("boot: opensheet.baseURL is not a valid URL (set YASAKU_OPENSHEET_BASE_URL to an https:// URL, or unset it)")
	}
	// SECURITY: the API key travels in the Authorization header, so plain http is refused unless the operator declared a private network.
	if u.Scheme != "https" && !cfg.Opensheet.AllowPrivateHosts {
		return opensheetWiring{}, fmt.Errorf("boot: opensheet.baseURL %s must be https (use https://, or set YASAKU_OPENSHEET_ALLOW_PRIVATE_HOSTS=true on a private network)", u.Redacted())
	}
	endpoint := opensheetsync.Endpoint{BaseURL: base, AllowPrivateHosts: cfg.Opensheet.AllowPrivateHosts}
	if err := endpoint.Validate(); err != nil {
		return opensheetWiring{}, fmt.Errorf("boot: opensheet.baseURL %s is refused (use a public host, or set YASAKU_OPENSHEET_ALLOW_PRIVATE_HOSTS=true on a private network): %w", u.Redacted(), err)
	}
	store := opensheetsync.NewStore(cfg.DB, k.Pool, k.PgConn)
	queued := cfg.Queue.Enabled
	return opensheetWiring{
		store:    store,
		mirror:   opensheetsync.NewMirror(store, jobs, queued, log, k.Reporter.Unexpected, time.Now),
		endpoint: endpoint,
		jobs:     jobs,
		queued:   queued,
	}, nil
}

func (w opensheetWiring) services(k *platform.Kernel, log *slog.Logger, src opensheetsync.Source, members opensheetsync.Members, uow tenant.UnitOfWork) (*opensheetsync.Service, *opensheetsync.Syncer) {
	if w.mirror == nil {
		return nil, nil
	}
	svc := opensheetsync.NewService(w.store, log, k.Reporter.Unexpected, opensheetsync.ServiceDeps{
		Mirror: w.mirror, Members: members, Sealer: k.Sealer, Endpoint: w.endpoint,
		UnitOfWork: opensheetsync.UnitOfWork(uow), Now: time.Now,
	})
	d := opensheetsync.SyncerDeps{Sealer: k.Sealer, Endpoint: w.endpoint, Source: src, Jobs: w.jobs, Queued: w.queued, Now: time.Now}
	return svc, opensheetsync.NewSyncer(w.store, log, k.Reporter.Unexpected, d)
}

type mirrorPort[R any] struct {
	m   *opensheetsync.Mirror
	ref func(R) opensheetsync.Ref
}

func (p mirrorPort[R]) Mark(ctx context.Context, refs ...R) error {
	return p.m.Mark(ctx, p.refs(refs)...)
}

func (p mirrorPort[R]) Kick(ctx context.Context, refs ...R) { p.m.Kick(ctx, p.refs(refs)...) }

func (p mirrorPort[R]) refs(in []R) []opensheetsync.Ref {
	out := make([]opensheetsync.Ref, len(in))
	for i, r := range in {
		out[i] = p.ref(r)
	}
	return out
}

func (w opensheetWiring) transactionMirror() transaction.Mirror {
	if w.mirror == nil {
		return nil
	}
	return mirrorPort[transaction.MirrorRef]{m: w.mirror, ref: func(r transaction.MirrorRef) opensheetsync.Ref {
		return opensheetsync.Ref{Entity: opensheetsync.Entity(r.Entity), ID: r.ID, Deleted: r.Deleted, Cascade: r.Cascade}
	}}
}

func (w opensheetWiring) walletMirror() wallet.Mirror {
	if w.mirror == nil {
		return nil
	}
	return mirrorPort[wallet.MirrorRef]{m: w.mirror, ref: func(r wallet.MirrorRef) opensheetsync.Ref {
		return opensheetsync.Ref{Entity: opensheetsync.Entity(r.Entity), ID: r.ID, Deleted: r.Deleted, Cascade: r.Cascade}
	}}
}

func (w opensheetWiring) categoryMirror() category.Mirror {
	if w.mirror == nil {
		return nil
	}
	return mirrorPort[category.MirrorRef]{m: w.mirror, ref: func(r category.MirrorRef) opensheetsync.Ref {
		return opensheetsync.Ref{Entity: opensheetsync.Entity(r.Entity), ID: r.ID, Deleted: r.Deleted, Cascade: r.Cascade}
	}}
}

// NOTE: opensheetSource reads a row's current state through the domain services, each checking the tenant scope the job runs in.
type opensheetSource struct {
	txs     *transaction.Service
	wallets *wallet.Service
	cats    *category.Service
	periods *period.Service
	ledgers *ledger.Service
}

var _ opensheetsync.Source = opensheetSource{}

func (s opensheetSource) Transaction(ctx context.Context, id uuid.UUID) (opensheetsync.TransactionFacts, bool, error) {
	t, err := s.txs.ByID(ctx, id)
	if transaction.IsNotFoundError(err) {
		return opensheetsync.TransactionFacts{}, false, nil
	}
	if err != nil {
		return opensheetsync.TransactionFacts{}, false, err
	}
	loc, err := s.ledgers.Location(ctx, t.OrgID, t.ProjectID)
	if err != nil {
		return opensheetsync.TransactionFacts{}, false, err
	}
	f := opensheetsync.TransactionFacts{
		ID: t.ID, Kind: string(t.Kind), Amount: t.Amount, OccurredAt: t.OccurredAt, Location: loc,
		WalletID: t.WalletID, Note: t.Note, UpdatedAt: t.UpdatedAt,
	}
	if f.WalletName, err = s.walletName(ctx, t.WalletID); err != nil {
		return opensheetsync.TransactionFacts{}, false, err
	}
	if t.ToWalletID != nil {
		f.ToWalletID = *t.ToWalletID
		if f.ToWalletName, err = s.walletName(ctx, *t.ToWalletID); err != nil {
			return opensheetsync.TransactionFacts{}, false, err
		}
	}
	if t.CategoryID != nil {
		c, cErr := s.cats.ByID(ctx, *t.CategoryID)
		if cErr != nil {
			return opensheetsync.TransactionFacts{}, false, cErr
		}
		f.CategoryID, f.CategoryName = c.ID, c.Name
	}
	if t.PeriodID != nil {
		p, pErr := s.periods.ByID(ctx, *t.PeriodID)
		if pErr != nil {
			return opensheetsync.TransactionFacts{}, false, pErr
		}
		f.PeriodName = p.Name
	}
	return f, true, nil
}

func (s opensheetSource) walletName(ctx context.Context, id uuid.UUID) (string, error) {
	w, err := s.wallets.ByID(ctx, id)
	if err != nil {
		return "", err
	}
	return w.Name, nil
}

func (s opensheetSource) Wallet(ctx context.Context, id uuid.UUID) (opensheetsync.WalletFacts, bool, error) {
	w, err := s.wallets.ByID(ctx, id)
	if wallet.IsNotFoundError(err) {
		return opensheetsync.WalletFacts{}, false, nil
	}
	if err != nil {
		return opensheetsync.WalletFacts{}, false, err
	}
	balance, err := s.txs.Balance(ctx, id)
	if err != nil {
		return opensheetsync.WalletFacts{}, false, err
	}
	return opensheetsync.WalletFacts{
		ID: w.ID, Name: w.Name, Kind: string(w.Kind), Provider: w.Provider, Balance: balance,
		ExcludeFromTotal: w.ExcludeFromTotal, Archived: w.IsArchived(), UpdatedAt: w.UpdatedAt,
	}, true, nil
}

func (s opensheetSource) Category(ctx context.Context, id uuid.UUID) (opensheetsync.CategoryFacts, bool, error) {
	c, err := s.cats.ByID(ctx, id)
	if category.IsNotFoundError(err) {
		return opensheetsync.CategoryFacts{}, false, nil
	}
	if err != nil {
		return opensheetsync.CategoryFacts{}, false, err
	}
	return opensheetsync.CategoryFacts{
		ID: c.ID, Name: c.Name, Kind: string(c.Kind), Icon: c.Icon, Color: c.Color, Archived: c.IsArchived(), UpdatedAt: c.UpdatedAt,
	}, true, nil
}
