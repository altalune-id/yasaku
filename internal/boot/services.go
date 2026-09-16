package boot

import (
	"context"
	"fmt"
	"strings"
	"time"

	"altalune.id/yasaku/internal/auth"
	"altalune.id/yasaku/internal/blog"
	blogcategory "altalune.id/yasaku/internal/blog/category"
	"altalune.id/yasaku/internal/blog/tag"
	"altalune.id/yasaku/internal/category"
	"altalune.id/yasaku/internal/invite"
	"altalune.id/yasaku/internal/ledger"
	"altalune.id/yasaku/internal/onboard"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/password"
	"altalune.id/yasaku/internal/period"
	"altalune.id/yasaku/internal/platform"
	"altalune.id/yasaku/internal/platform/capabilities"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/project"
	"altalune.id/yasaku/internal/report"
	"altalune.id/yasaku/internal/todo"
	"altalune.id/yasaku/internal/transaction"
	"altalune.id/yasaku/internal/user"
	"altalune.id/yasaku/internal/wallet"
)

// Services is every domain store, service and workflow the composition root wires.
type Services struct {
	UserStore    user.Store
	OrgStore     org.Store
	ProjectStore project.Store
	TodoStore    todo.Store
	InviteStore  invite.Store
	OnboardStore onboard.Store

	LedgerStore      ledger.Store
	WalletStore      wallet.Store
	TxCategoryStore  category.Store
	PeriodStore      period.Store
	TransactionStore transaction.Store

	Auth       *auth.Service
	Users      *user.Service
	Orgs       *org.Service
	Projects   *project.Service
	Todos      *todo.Service
	Invites    *invite.Service
	Onboards   *onboard.Service
	Posts      *blog.Service
	Categories *blogcategory.Service
	Tags       *tag.Service

	Ledgers      *ledger.Service
	Wallets      *wallet.Service
	TxCategories *category.Service
	Periods      *period.Service
	Transactions *transaction.Service
	Reports      *report.Service

	Onboard    *user.OnboardWorkflow
	WalletOpen *wallet.OpenWorkflow
}

func buildServices(cfg *config.Config, k *platform.Kernel, caps capabilities.Capabilities) (*Services, error) {
	pool := k.Pool
	pgConn := k.PgConn
	log := k.Log
	reporter := k.Reporter
	mail := k.Mail

	userStore := user.NewStore(cfg.DB, pool)
	orgStore := org.NewStore(cfg.DB, pool, pgConn)
	projectStore := project.NewStore(cfg.DB, pool, pgConn)
	todoStore := todo.NewStore(cfg.DB, pool, pgConn)
	inviteStore := invite.NewStore(cfg.DB, pool, pgConn)
	onboardStore := onboard.NewStore(cfg.DB, pool)

	orgs := org.NewService(orgStore, caps, log, reporter.Unexpected)
	projects := project.NewService(projectStore, log, reporter.Unexpected)
	todos := todo.NewService(todoStore, log, reporter.Unexpected)
	onboards := onboard.NewService(onboardStore, log, reporter.Unexpected)
	posts := blog.NewService(blog.NewStore(cfg.DB, pool, pgConn), log, reporter.Unexpected)
	categories := blogcategory.NewService(blogcategory.NewStore(cfg.DB, pool, pgConn), log, reporter.Unexpected)
	tags := tag.NewService(tag.NewStore(cfg.DB, pool, pgConn), log, reporter.Unexpected)

	ledgerStore := ledger.NewStore(cfg.DB, pool, pgConn)
	walletStore := wallet.NewStore(cfg.DB, pool, pgConn)
	txCategoryStore := category.NewStore(cfg.DB, pool, pgConn)
	periodStore := period.NewStore(cfg.DB, pool, pgConn)
	transactionStore := transaction.NewStore(cfg.DB, pool, pgConn)

	uow := unitOfWork(cfg.DB, pool, pgConn)

	ledgers := ledger.NewService(ledgerStore, log, reporter.Unexpected)
	wallets := wallet.NewService(walletStore, log, reporter.Unexpected)
	txCategories := category.NewService(txCategoryStore, log, reporter.Unexpected, namerAdapter{})
	reports := report.NewService(report.NewReader(cfg.DB, pool, pgConn), log, reporter.Unexpected, ledgers)
	periods := period.NewService(periodStore, log, reporter.Unexpected,
		ledgers, snapshotterAdapter{reports: reports, now: time.Now}, period.UnitOfWork(uow), time.Now)
	transactions := transaction.NewService(transactionStore, log, reporter.Unexpected,
		walletReaderFor(wallets), categoryReaderFor(txCategories),
		periodResolverAdapter{periods: periods}, transaction.UnitOfWork(uow))
	walletOpen := wallet.NewOpenWorkflow(wallets, transactions, wallet.UnitOfWork(uow), log, reporter.Unexpected)

	invitesEnabled := cfg.Mode == config.ModeCloud || cfg.OIDC.Issuer != ""

	sendWorkflow := invite.NewSendWorkflow(
		inviteStore,
		mail,
		strings.TrimRight(cfg.HTTP.BaseURL, "/")+cfg.HTTP.BasePath,
		log,
		reporter.Unexpected,
	)
	acceptWorkflow := invite.NewAcceptWorkflow(
		inviteStore,
		userStoreForInvite{store: userStore},
		orgStoreForInvite{store: orgStore},
		log,
		reporter.Unexpected,
	)
	invites := invite.NewService(inviteStore, sendWorkflow, acceptWorkflow, invitesEnabled, log, reporter.Unexpected)

	users := user.NewService(
		userStore,
		user.GenesisConfig{Email: cfg.Genesis.Email, Password: cfg.Genesis.Password},
		log,
		reporter.Unexpected,
		user.WithInviteFinder(invites),
	)

	onboardWorkflow := user.NewOnboardWorkflow(
		userStore,
		orgStoreForOnboard{store: orgStore},
		projectStoreForOnboard{store: projectStore},
		inviteStoreForOnboard{store: inviteStore},
		onboardPolicyFrom(cfg),
		log,
		reporter.Unexpected,
	)

	genesisHash, err := hashGenesisPassword(cfg.Genesis.Password)
	if err != nil {
		return nil, fmt.Errorf("boot: hash genesis password: %w", err)
	}
	local := auth.NewLocalLogin(
		userStoreForAuth{store: userStore},
		auth.Genesis{
			Email:        cfg.Genesis.Email,
			PasswordHash: genesisHash,
			Name:         cfg.Genesis.Email,
		},
		log,
		reporter.Unexpected,
		auth.WithLocalNotFound(user.IsNotFoundError),
	)

	oidcOpts := []auth.OIDCOption{
		auth.WithSignupRequired(user.IsSignupRequiredError),
	}
	if cfg.Mode == config.ModeSelfhosted {
		oidcOpts = append(oidcOpts, auth.WithAllowSignup(func(ctx context.Context, email string) error {
			if req, rerr := onboards.Required(ctx); rerr == nil && req {
				return nil
			}
			return users.CheckOIDCSignupEligibility(ctx, email)
		}))
	}
	oidcLogin := auth.NewOIDCLogin(
		func(ctx context.Context, claims auth.EnsureClaims) (*auth.UserRef, bool, error) {
			u, err := users.EnsureFromOIDC(ctx, user.Claims(claims))
			if err != nil {
				return nil, false, err
			}
			return &auth.UserRef{ID: u.ID, Email: u.Email, Name: u.Name, Source: u.Source, IsAdmin: u.IsAdmin, Locale: u.Locale, TermsAcceptedAt: u.TermsAcceptedAt}, false, nil
		},
		func(ctx context.Context, req auth.OnboardRequest) (auth.OnboardResult, error) {
			res, err := onboardWorkflow.Onboard(ctx, req.UserID, req.Email)
			if err != nil {
				return auth.OnboardResult{}, err
			}
			return auth.OnboardResult{OrgID: res.OrgID, ProjectID: res.ProjectID}, nil
		},
		log,
		reporter.Unexpected,
		oidcOpts...,
	)

	auths := auth.NewService(local, oidcLogin, log, reporter.Unexpected)

	return &Services{
		UserStore:    userStore,
		OrgStore:     orgStore,
		ProjectStore: projectStore,
		TodoStore:    todoStore,
		InviteStore:  inviteStore,
		OnboardStore: onboardStore,

		LedgerStore:      ledgerStore,
		WalletStore:      walletStore,
		TxCategoryStore:  txCategoryStore,
		PeriodStore:      periodStore,
		TransactionStore: transactionStore,

		Auth:       auths,
		Users:      users,
		Orgs:       orgs,
		Projects:   projects,
		Todos:      todos,
		Invites:    invites,
		Onboards:   onboards,
		Posts:      posts,
		Categories: categories,
		Tags:       tags,

		Ledgers:      ledgers,
		Wallets:      wallets,
		TxCategories: txCategories,
		Periods:      periods,
		Transactions: transactions,
		Reports:      reports,

		Onboard:    onboardWorkflow,
		WalletOpen: walletOpen,
	}, nil
}

func onboardPolicyFrom(cfg *config.Config) user.Policy {
	policyMode := user.PolicyModeCloud
	if cfg.Mode == config.ModeSelfhosted {
		policyMode = user.PolicyModeSelfhosted
	}
	return user.Policy{
		Mode:             policyMode,
		SingletonOrgSlug: cfg.Tenant.SingletonOrg.Slug,
	}
}

func hashGenesisPassword(plain string) (string, error) {
	if strings.TrimSpace(plain) == "" {
		return "", nil
	}
	return password.Hash(plain)
}
