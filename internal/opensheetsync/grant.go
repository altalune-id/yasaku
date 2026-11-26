package opensheetsync

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/altalune-id/oidcrp"
	"github.com/google/uuid"

	"altalune.id/yasaku/internal/platform/sealer"
)

// GrantStatus is whether a grant still refreshes.
type GrantStatus string

// Statuses a grant can hold.
const (
	GrantOK              GrantStatus = "ok"
	GrantReconnectNeeded GrantStatus = "reconnect_needed"
)

// Grant is a project's OAuth grant to opensheet: the sealed refresh token, the last access token and who connected it.
type Grant struct {
	OrgID           uuid.UUID
	ProjectID       uuid.UUID
	ConnectedBy     uuid.UUID
	RefreshSealed   []byte
	AccessSealed    []byte
	AccessExpiresAt *time.Time
	Scopes          string
	Status          GrantStatus
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// GrantStore is the driven port for a project's OAuth grant.
type GrantStore interface {
	// SaveGrant upserts the project's grant at connect; a grant row held by another org is *GrantNotFoundError.
	SaveGrant(ctx context.Context, g *Grant) error
	// UpdateGrantTokens writes g's tokens, scopes and status onto the grant g was read as, never inserting; a grant deleted or connected again since is *GrantNotFoundError.
	UpdateGrantTokens(ctx context.Context, g *Grant) error
	// GrantByProject returns the project's grant, or *GrantNotFoundError.
	GrantByProject(ctx context.Context, orgID, projectID uuid.UUID) (*Grant, error)
	// GrantsConnectedBy returns the org's grants userID connected.
	GrantsConnectedBy(ctx context.Context, orgID, userID uuid.UUID) ([]*Grant, error)
	// DeleteGrant removes the project's grant; no grant is *GrantNotFoundError.
	DeleteGrant(ctx context.Context, orgID, projectID uuid.UUID) error
	// LockGrant runs fn with the project's grant held against other refreshers and commits what fn saved even when fn returns an error, so a rotated refresh token outlives a failed refresh; no grant is *GrantNotFoundError.
	LockGrant(ctx context.Context, orgID, projectID uuid.UUID, fn func(ctx context.Context, g *Grant) error) error
}

// NewGrant seals set's tokens into the project's grant; set must carry a refresh token.
func NewGrant(sl sealer.Sealer, orgID, projectID, connectedBy uuid.UUID, set oidcrp.TokenSet, at time.Time) (*Grant, error) {
	at = at.UTC()
	g := &Grant{OrgID: orgID, ProjectID: projectID, ConnectedBy: connectedBy, Status: GrantOK, CreatedAt: at, UpdatedAt: at}
	if err := g.apply(sl, set, at); err != nil {
		return nil, err
	}
	return g, nil
}

// RefreshToken opens the grant's refresh token.
func (g *Grant) RefreshToken(sl sealer.Sealer) (string, error) {
	return openGrantToken(sl, g.RefreshSealed, grantAAD("refresh", g.OrgID, g.ProjectID))
}

func (g *Grant) accessToken(sl sealer.Sealer) (string, error) {
	if len(g.AccessSealed) == 0 {
		return "", nil
	}
	return openGrantToken(sl, g.AccessSealed, grantAAD("access", g.OrgID, g.ProjectID))
}

// NOTE: a set without an access token, such as the one a failed refresh hands back, keeps only its rotated refresh token and drops the old access token.
func (g *Grant) apply(sl sealer.Sealer, set oidcrp.TokenSet, at time.Time) error {
	if set.RefreshToken == "" {
		return &ConnectFailedError{Cause: fmt.Errorf("no refresh token")}
	}
	refresh, err := sl.Seal([]byte(set.RefreshToken), grantAAD("refresh", g.OrgID, g.ProjectID))
	if err != nil {
		return fmt.Errorf("opensheetsync.Grant: seal refresh token: %w", err)
	}
	g.RefreshSealed, g.AccessSealed, g.AccessExpiresAt = refresh, nil, nil
	if set.AccessToken != "" {
		if g.AccessSealed, err = sl.Seal([]byte(set.AccessToken), grantAAD("access", g.OrgID, g.ProjectID)); err != nil {
			return fmt.Errorf("opensheetsync.Grant: seal access token: %w", err)
		}
		if !set.ExpiresAt.IsZero() {
			exp := set.ExpiresAt.UTC()
			g.AccessExpiresAt = &exp
		}
	}
	if len(set.Scopes) > 0 {
		g.Scopes = strings.Join(set.Scopes, " ")
	}
	g.UpdatedAt = at.UTC()
	return nil
}

// SECURITY: the additional data binds a sealed token to its kind, org and project, so a row copied into another project, or an access token read as a refresh token, never opens.
func grantAAD(kind string, orgID, projectID uuid.UUID) []byte {
	return []byte("opensheetsync.grant." + kind + "|" + orgID.String() + "|" + projectID.String())
}

func openGrantToken(sl sealer.Sealer, sealed, aad []byte) (string, error) {
	plain, err := sl.Open(sealed, aad)
	if err != nil {
		return "", fmt.Errorf("opensheetsync.openGrantToken: %w", err)
	}
	return string(plain), nil
}
