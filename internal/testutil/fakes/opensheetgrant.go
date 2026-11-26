package fakes

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/altalune-id/oidcrp"
	"github.com/google/uuid"

	"altalune.id/yasaku/internal/opensheetsync"
)

var (
	_ opensheetsync.GrantStore = (*OpensheetGrants)(nil)
	_ opensheetsync.Refresher  = (*OpensheetRefresher)(nil)
)

// OpensheetGrants is an in-memory opensheetsync.GrantStore: grants upsert by project behind an org guard. LockGrant takes no lock, as SQLite's does not.
type OpensheetGrants struct {
	mu     sync.Mutex
	grants map[uuid.UUID]opensheetsync.Grant
}

// NewOpensheetGrants returns an empty store.
func NewOpensheetGrants() *OpensheetGrants {
	return &OpensheetGrants{grants: map[uuid.UUID]opensheetsync.Grant{}}
}

// SaveGrant upserts g by project; a project held by another org is *GrantNotFoundError.
func (f *OpensheetGrants) SaveGrant(_ context.Context, g *opensheetsync.Grant) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if cur, ok := f.grants[g.ProjectID]; ok {
		if cur.OrgID != g.OrgID {
			return &opensheetsync.GrantNotFoundError{ProjectID: g.ProjectID.String()}
		}
		g.CreatedAt = cur.CreatedAt
	}
	f.grants[g.ProjectID] = cloneGrant(*g)
	return nil
}

// UpdateGrantTokens overwrites the grant g was read as; a grant gone or connected again since is *GrantNotFoundError.
func (f *OpensheetGrants) UpdateGrantTokens(_ context.Context, g *opensheetsync.Grant) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cur, ok := f.grants[g.ProjectID]
	if !ok || cur.OrgID != g.OrgID || !cur.CreatedAt.Equal(g.CreatedAt) {
		return &opensheetsync.GrantNotFoundError{ProjectID: g.ProjectID.String()}
	}
	f.grants[g.ProjectID] = cloneGrant(*g)
	return nil
}

// GrantByProject returns a copy of the project's grant.
func (f *OpensheetGrants) GrantByProject(_ context.Context, orgID, projectID uuid.UUID) (*opensheetsync.Grant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	g, ok := f.grants[projectID]
	if !ok || g.OrgID != orgID {
		return nil, &opensheetsync.GrantNotFoundError{ProjectID: projectID.String()}
	}
	out := cloneGrant(g)
	return &out, nil
}

// GrantsConnectedBy returns copies of the org's grants userID connected.
func (f *OpensheetGrants) GrantsConnectedBy(_ context.Context, orgID, userID uuid.UUID) ([]*opensheetsync.Grant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*opensheetsync.Grant
	for _, g := range f.grants {
		if g.OrgID == orgID && g.ConnectedBy == userID {
			c := cloneGrant(g)
			out = append(out, &c)
		}
	}
	slices.SortFunc(out, func(a, b *opensheetsync.Grant) int { return slices.Compare(a.ProjectID[:], b.ProjectID[:]) })
	return out, nil
}

// DeleteGrant removes the project's grant.
func (f *OpensheetGrants) DeleteGrant(_ context.Context, orgID, projectID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	g, ok := f.grants[projectID]
	if !ok || g.OrgID != orgID {
		return &opensheetsync.GrantNotFoundError{ProjectID: projectID.String()}
	}
	delete(f.grants, projectID)
	return nil
}

// LockGrant reads the grant and runs fn.
func (f *OpensheetGrants) LockGrant(ctx context.Context, orgID, projectID uuid.UUID, fn func(ctx context.Context, g *opensheetsync.Grant) error) error {
	g, err := f.GrantByProject(ctx, orgID, projectID)
	if err != nil {
		return err
	}
	return fn(ctx, g)
}

func cloneGrant(g opensheetsync.Grant) opensheetsync.Grant {
	g.RefreshSealed = slices.Clone(g.RefreshSealed)
	g.AccessSealed = slices.Clone(g.AccessSealed)
	if g.AccessExpiresAt != nil {
		at := *g.AccessExpiresAt
		g.AccessExpiresAt = &at
	}
	return g
}

// OpensheetRefresher is a hand-written upstream IdP for grants: each refresh issues access-N and refresh-N, where N counts refreshes. Set Delay, Next and RevokeErr before use.
type OpensheetRefresher struct {
	Delay     time.Duration
	Next      func(ctx context.Context, prev oidcrp.TokenSet) (*oidcrp.TokenSet, error)
	RevokeErr error

	mu        sync.Mutex
	refreshes []string
	revoked   []string
}

// Refresh records prev's refresh token, waits Delay and answers Next, or a rotated set.
func (f *OpensheetRefresher) Refresh(ctx context.Context, prev oidcrp.TokenSet) (*oidcrp.TokenSet, error) {
	f.mu.Lock()
	f.refreshes = append(f.refreshes, prev.RefreshToken)
	n := len(f.refreshes)
	f.mu.Unlock()
	select {
	case <-time.After(f.Delay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if f.Next != nil {
		return f.Next(ctx, prev)
	}
	return &oidcrp.TokenSet{
		AccessToken: fmt.Sprintf("access-%d", n), RefreshToken: fmt.Sprintf("refresh-%d", n),
		ExpiresAt: time.Now().Add(time.Hour), Scopes: prev.Scopes,
	}, nil
}

// Revoke records token and answers RevokeErr.
func (f *OpensheetRefresher) Revoke(_ context.Context, token, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.revoked = append(f.revoked, token)
	return f.RevokeErr
}

// Refreshes returns the refresh tokens presented, in order.
func (f *OpensheetRefresher) Refreshes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.refreshes)
}

// Revoked returns the tokens revoked, in order.
func (f *OpensheetRefresher) Revoked() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.revoked)
}
