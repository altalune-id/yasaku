package opensheetsync

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/altalune-id/oidcrp"
	"github.com/google/uuid"

	"altalune.id/yasaku/internal/platform/sealer"
)

const (
	refreshHeadroom = 60 * time.Second
	refreshTimeout  = 5 * time.Second
	revokeTimeout   = 5 * time.Second
)

// Refresher renews and revokes a grant's tokens at the upstream IdP; *oidcrp.Client is one.
type Refresher interface {
	Refresh(ctx context.Context, prev oidcrp.TokenSet) (*oidcrp.TokenSet, error)
	Revoke(ctx context.Context, token, hint string) error
}

type credentialSource interface {
	Bearer(ctx context.Context, l *Link) (string, error)
}

type keySource struct{ sealer sealer.Sealer }

func (k keySource) Bearer(_ context.Context, l *Link) (string, error) {
	key, err := openKey(k.sealer, l)
	if err != nil {
		return "", &KeyUnreadableError{Cause: err}
	}
	return key, nil
}

// NOTE: SQLite is single-process, so the per-project mutex is its whole single-flight; on Postgres, LockGrant's row lock extends it across processes.
type grantSource struct {
	store  GrantStore
	sealer sealer.Sealer
	rp     Refresher
	now    func() time.Time

	mu    sync.Mutex
	locks map[uuid.UUID]*sync.Mutex
}

func (g *grantSource) Bearer(ctx context.Context, l *Link) (string, error) {
	return g.bearer(ctx, l, "")
}

// NOTE: stale is a token opensheet just refused as expired; a current token equal to it is refreshed whatever its expiry says.
func (g *grantSource) bearer(ctx context.Context, l *Link, stale string) (string, error) {
	gr, err := g.store.GrantByProject(ctx, l.OrgID, l.ProjectID)
	if IsGrantNotFoundError(err) {
		return "", &ReconnectNeededError{Cause: err}
	}
	if err != nil {
		return "", fmt.Errorf("opensheetsync: grant: %w", err)
	}
	if token, ok := g.usable(gr, stale); ok {
		return token, nil
	}
	lock := g.lockFor(l.ProjectID)
	lock.Lock()
	defer lock.Unlock()
	var token string
	err = g.store.LockGrant(ctx, l.OrgID, l.ProjectID, func(ctx context.Context, gr *Grant) error {
		if t, ok := g.usable(gr, stale); ok {
			token = t
			return nil
		}
		t, err := g.refresh(ctx, gr)
		token = t
		return err
	})
	if IsGrantNotFoundError(err) {
		return "", &ReconnectNeededError{Cause: err}
	}
	return token, err
}

func (g *grantSource) usable(gr *Grant, stale string) (string, bool) {
	if gr.Status != GrantOK {
		return "", false
	}
	token, err := gr.accessToken(g.sealer)
	if err != nil || token == "" || token == stale {
		return "", false
	}
	if gr.AccessExpiresAt != nil && gr.AccessExpiresAt.Sub(g.now()) < refreshHeadroom {
		return "", false
	}
	return token, true
}

// NOTE: no retry here: the sync job is retried by its queue, and a refresh token is spent by every attempt.
func (g *grantSource) refresh(ctx context.Context, gr *Grant) (string, error) {
	if gr.Status != GrantOK {
		return "", &ReconnectNeededError{}
	}
	rt, err := gr.RefreshToken(g.sealer)
	if err != nil {
		return "", &ReconnectNeededError{Cause: err}
	}
	if g.rp == nil {
		return "", &ReconnectNeededError{Cause: fmt.Errorf("no OAuth client for opensheet")}
	}
	rctx, cancel := context.WithTimeout(ctx, refreshTimeout)
	set, rerr := g.rp.Refresh(rctx, oidcrp.TokenSet{RefreshToken: rt, Scopes: strings.Fields(gr.Scopes)})
	cancel()
	if oidcrp.OAuthErrorOf(rerr) == "invalid_grant" {
		gr.Status, gr.UpdatedAt = GrantReconnectNeeded, g.now().UTC()
		if err := g.store.UpdateGrantTokens(ctx, gr); err != nil {
			return "", fmt.Errorf("opensheetsync: grant: mark reconnect: %w", err)
		}
		return "", &ReconnectNeededError{Cause: rerr}
	}
	if set != nil {
		// SECURITY: RFC 9700 4.14.2: the issuer may have spent rt already, so the rotated token is saved even when the refresh failed.
		if err := gr.apply(g.sealer, *set, g.now()); err != nil {
			return "", err
		}
		// SECURITY: an update, never an upsert, so a refresh racing a disconnect or a member removal cannot bring the grant back.
		if err := g.store.UpdateGrantTokens(ctx, gr); err != nil {
			return "", fmt.Errorf("opensheetsync: grant: save: %w", err)
		}
	}
	if rerr != nil {
		return "", fmt.Errorf("opensheetsync: grant: refresh: %w", rerr)
	}
	return set.AccessToken, nil
}

// NOTE: shortcut: one mutex per project ever refreshed, never pruned; prune if a process serves many thousands of projects.
func (g *grantSource) lockFor(projectID uuid.UUID) *sync.Mutex {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.locks == nil {
		g.locks = map[uuid.UUID]*sync.Mutex{}
	}
	m, ok := g.locks[projectID]
	if !ok {
		m = &sync.Mutex{}
		g.locks[projectID] = m
	}
	return m
}

// NOTE: unreadable or missing tokens leave nothing to revoke; the caller deletes the grant either way.
func (g *grantSource) prepareRevoke(gr *Grant, log *slog.Logger) func(ctx context.Context) error {
	if g.rp == nil {
		return nil
	}
	rt, err := gr.RefreshToken(g.sealer)
	if err != nil {
		log.Warn("opensheetsync: the grant's refresh token cannot be opened, so it is not revoked", slog.String("project_id", gr.ProjectID.String()))
		return nil
	}
	return func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, revokeTimeout)
		defer cancel()
		return g.rp.Revoke(ctx, rt, "refresh_token")
	}
}

// Credentials hands each link its bearer token: the sealed API key of a key link, or a grant link's access token, refreshed single-flight per project. NOTE: build one per process and share it, or SQLite loses its single-flight.
type Credentials struct {
	keys   keySource
	grants *grantSource
	log    *slog.Logger
}

var _ credentialSource = (*Credentials)(nil)

// NewCredentials binds the credential source to the sealer, the grant store and the OAuth client; a nil store or client leaves grant links needing a reconnect.
func NewCredentials(sl sealer.Sealer, grants GrantStore, rp Refresher, log *slog.Logger) *Credentials {
	c := &Credentials{keys: keySource{sealer: sl}, log: log.With("module", "opensheetsync")}
	if grants != nil {
		c.grants = &grantSource{store: grants, sealer: sl, rp: rp, now: time.Now}
	}
	return c
}

// Bearer returns the token l presents to opensheet: *KeyUnreadableError for a key that no longer opens, *ReconnectNeededError for a grant that needs a person, any other error is worth a retry.
func (c *Credentials) Bearer(ctx context.Context, l *Link) (string, error) {
	return c.bearer(ctx, l, "")
}

func (c *Credentials) bearer(ctx context.Context, l *Link, stale string) (string, error) {
	if l.Credential != CredentialGrant {
		return c.keys.Bearer(ctx, l)
	}
	if c.grants == nil {
		return "", &ReconnectNeededError{Cause: fmt.Errorf("no grant store")}
	}
	return c.grants.bearer(ctx, l, stale)
}

// PrepareRevoke reads the project's grant and returns what revokes its refresh token at the upstream IdP, or nil when there is nothing to revoke.
func (c *Credentials) PrepareRevoke(ctx context.Context, orgID, projectID uuid.UUID) (func(ctx context.Context) error, error) {
	if c.grants == nil {
		return nil, nil
	}
	gr, err := c.grants.store.GrantByProject(ctx, orgID, projectID)
	if IsGrantNotFoundError(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("opensheetsync.PrepareRevoke: %w", err)
	}
	return c.grants.prepareRevoke(gr, c.log), nil
}

// RevokeMember deletes the org's grants userID connected and revokes each one best-effort, so an upstream IdP outage never blocks removing a member.
func (c *Credentials) RevokeMember(ctx context.Context, orgID, userID uuid.UUID) error {
	if c.grants == nil {
		return nil
	}
	grants, err := c.grants.store.GrantsConnectedBy(ctx, orgID, userID)
	if err != nil {
		return fmt.Errorf("opensheetsync.RevokeMember: list: %w", err)
	}
	for _, gr := range grants {
		revoke := c.grants.prepareRevoke(gr, c.log)
		if err := c.grants.store.DeleteGrant(ctx, orgID, gr.ProjectID); err != nil && !IsGrantNotFoundError(err) {
			return fmt.Errorf("opensheetsync.RevokeMember: delete: %w", err)
		}
		c.revoke(ctx, revoke, gr.ProjectID)
	}
	return nil
}

func (c *Credentials) revoke(ctx context.Context, revoke func(context.Context) error, projectID uuid.UUID) {
	if revoke == nil {
		return
	}
	if err := revoke(ctx); err != nil {
		c.log.WarnContext(ctx, "opensheetsync: revoking the grant at the upstream IdP failed; the grant is deleted here",
			slog.String("project_id", projectID.String()), slog.String("err", err.Error()))
	}
}
