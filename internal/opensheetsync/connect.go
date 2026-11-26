package opensheetsync

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/altalune-id/oidcrp"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"

	"altalune.id/yasaku/internal/platform/audit"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/opensheet"
)

// Auditor records an audit event inside the unit of work on ctx.
type Auditor interface {
	Record(ctx context.Context, e audit.Event) error
}

// Access is how a project's grant stands next to its link.
type Access string

// Access states the page shows; AccessNone covers no grant and an API-key link.
const (
	AccessNone       Access = ""
	AccessPending    Access = "pending"
	AccessOK         Access = "ok"
	AccessReconnect  Access = "reconnect"
	AccessMemberLeft Access = "member_left"
)

// Pick is the opensheet project and Google credential a person chose for the Sheet, and the yasaku project's name for its title.
type Pick struct {
	OSProjectID  string
	CredentialID string
	ProjectName  string
}

// Target is an opensheet project paired with one of its usable Google credentials.
type Target struct {
	Project    opensheet.Project
	Credential opensheet.Credential
}

// Setup names what the grant's person still has to do in opensheet before a Sheet can be created.
type Setup int

// Setup steps: ready to pick, no opensheet project, or no project with a working Google account.
const (
	SetupReady Setup = iota
	SetupNoAccount
	SetupNoGoogle
)

// Choices are the opensheet projects the grant's person reaches and the usable project and credential pairs; empty when opensheet does not know them.
type Choices struct {
	Projects []opensheet.Project
	Targets  []Target
}

// Setup reports the step missing before a pick, or SetupReady.
func (c Choices) Setup() Setup {
	switch {
	case len(c.Targets) > 0:
		return SetupReady
	case len(c.Projects) == 0:
		return SetupNoAccount
	}
	return SetupNoGoogle
}

func usable(c opensheet.Credential) bool { return c.Kind == "google_oauth" && c.Status == "active" }

// GrantScopes are the scopes a connect asks opensheet for; a grant missing any of them is refused.
func GrantScopes() []string {
	return []string{"sheets:read", "sheets:write", "spreadsheets:read", "spreadsheets:write", "sheets:admin", "credentials:read", "offline_access"}
}

// ProvisionSlugs names the sheets Provision creates for a yasaku project; the project id's last 12 hex, its random bits, keep them unique across yasaku orgs sharing one opensheet project.
func ProvisionSlugs(projectID uuid.UUID) SheetSlugs {
	short := "yasaku-" + strings.ReplaceAll(projectID.String(), "-", "")[20:] + "-"
	return SheetSlugs{Transactions: short + "transactions", Wallets: short + "wallets", Categories: short + "categories"}
}

func provisionTitle(e Entity) string {
	switch e {
	case EntityTransaction:
		return "Transactions"
	case EntityWallet:
		return "Wallets"
	}
	return "Categories"
}

// Authorize reports whether the caller may connect the project to opensheet: a manager, on a project with no API-key link.
func (s *Service) Authorize(ctx context.Context) error {
	_, _, err := s.connectable(ctx)
	return err
}

func (s *Service) connectable(ctx context.Context) (tenant.Context, *Link, error) {
	tc, err := s.manager(ctx)
	if err != nil {
		return tenant.Context{}, nil, err
	}
	l, err := s.store.LinkByProject(ctx, tc.OrgID, tc.ProjectID)
	if IsLinkNotFoundError(err) {
		return tc, nil, nil
	}
	if err != nil {
		return tenant.Context{}, nil, s.unexpected(ctx, "opensheetsync: link", err, "project_id", tc.ProjectID)
	}
	if l.Credential != CredentialGrant {
		return tenant.Context{}, nil, &InvalidSettingError{Field: "credential", Reason: "this project syncs with an API key; disconnect it first"}
	}
	return tc, l, nil
}

// Connect seals set into the project's grant; it reports true when a grant link already exists, which then resumes with a full backfill, and false when the picker comes next.
func (s *Service) Connect(ctx context.Context, set oidcrp.TokenSet) (bool, error) {
	ctx, span := tracer.Start(ctx, "opensheetsync.Connect")
	defer span.End()

	tc, l, err := s.connectable(ctx)
	if err != nil {
		return false, err
	}
	if s.creds.grants == nil {
		return false, &ConnectFailedError{Cause: fmt.Errorf("no grant store")}
	}
	if missing := slices.DeleteFunc(GrantScopes(), func(sc string) bool { return slices.Contains(set.Scopes, sc) }); len(missing) > 0 {
		s.revokeIssued(ctx, set.RefreshToken, tc)
		return false, &ConnectFailedError{Cause: fmt.Errorf("scopes not granted: %s", strings.Join(missing, " "))}
	}
	at := s.now()
	g, err := NewGrant(s.sealer, tc.OrgID, tc.ProjectID, tc.UserID, set, at)
	if err != nil {
		return false, err
	}
	resume := l != nil && l.Credential == CredentialGrant
	// SECURITY: SaveGrant overwrites the old refresh token in place, so its family is revoked at the upstream IdP after the new grant commits.
	revoke, err := s.creds.PrepareRevoke(ctx, tc.OrgID, tc.ProjectID)
	if err != nil {
		return false, s.unexpected(ctx, "opensheetsync.Connect: prior grant", err, "project_id", tc.ProjectID)
	}
	err = s.uow(ctx, func(ctx context.Context) error {
		if err := s.creds.grants.store.SaveGrant(ctx, g); err != nil {
			return err
		}
		if !resume {
			return nil
		}
		stamp := s.stamp(l)
		if err := l.Enable(stamp); err != nil {
			return err
		}
		if err := s.store.SaveLink(ctx, l); err != nil {
			return err
		}
		if _, err := s.store.MarkAll(ctx, tc.OrgID, tc.ProjectID, stamp); err != nil {
			return err
		}
		return s.record(ctx, "opensheet.connected", tc)
	})
	if err != nil {
		return false, s.passOrReport(ctx, "opensheetsync.Connect: save", err, tc)
	}
	s.creds.revoke(ctx, revoke, tc.ProjectID)
	if resume {
		s.mirror.KickDirty(ctx)
	}
	return resume, nil
}

// Choices lists what the grant's person can pick; an opensheet that refuses the person gives empty Choices, not an error.
func (s *Service) Choices(ctx context.Context) (Choices, error) {
	ctx, span := tracer.Start(ctx, "opensheetsync.Choices")
	defer span.End()

	tc, err := s.manager(ctx)
	if err != nil {
		return Choices{}, err
	}
	c, err := s.control(ctx, tc)
	if err != nil {
		return Choices{}, err
	}
	projects, err := c.ListProjects(ctx)
	if refused(err) {
		return Choices{}, nil
	}
	if err != nil {
		return Choices{}, s.controlErr(ctx, "opensheetsync.Choices: projects", err, tc)
	}
	var out Choices
	for _, p := range projects {
		creds, err := c.ListCredentials(ctx, p.ID)
		if refused(err) {
			continue
		}
		if err != nil {
			return Choices{}, s.controlErr(ctx, "opensheetsync.Choices: credentials", err, tc)
		}
		out.Projects = append(out.Projects, p)
		for _, cr := range creds {
			if usable(cr) && cr.ProjectID == p.ID {
				out.Targets = append(out.Targets, Target{Project: p, Credential: cr})
			}
		}
	}
	return out, nil
}

// Provision creates the Sheet in the picked opensheet project and links it with the grant, enabled with a full backfill.
func (s *Service) Provision(ctx context.Context, pick Pick) (*Link, error) {
	ctx, span := tracer.Start(ctx, "opensheetsync.Provision")
	defer span.End()

	tc, err := s.manager(ctx)
	if err != nil {
		return nil, err
	}
	choices, err := s.Choices(ctx)
	if err != nil {
		return nil, err
	}
	// SECURITY: the form names ids; only a usable pair opensheet lists for this person is provisioned into.
	ti := slices.IndexFunc(choices.Targets, func(tg Target) bool {
		return tg.Project.ID == pick.OSProjectID && tg.Credential.ID == pick.CredentialID
	})
	if pick.OSProjectID == "" || pick.CredentialID == "" || ti < 0 {
		return nil, &InvalidSettingError{Field: "target", Reason: "pick a project and Google account OpenSheet lists for you"}
	}
	osp := choices.Targets[ti].Project
	slugs := ProvisionSlugs(tc.ProjectID)
	req := opensheet.ProvisionRequest{
		ProjectID: osp.ID, CredentialID: pick.CredentialID, IdempotencyKey: "yasaku:" + tc.ProjectID.String(),
		Title: strings.TrimSpace(pick.ProjectName) + " · Yasaku records",
	}
	for _, tab := range Contract() {
		req.Tabs = append(req.Tabs, opensheet.ProvisionTab{Title: provisionTitle(tab.Entity), Headers: tab.Columns, Slug: slugs.For(tab.Entity)})
	}
	c, err := s.control(ctx, tc)
	if err != nil {
		return nil, err
	}
	ss, err := c.Provision(ctx, req)
	if err != nil {
		return nil, s.controlErr(ctx, "opensheetsync.Provision", err, tc)
	}
	existing, err := s.store.LinkByProject(ctx, tc.OrgID, tc.ProjectID)
	if IsLinkNotFoundError(err) {
		existing, err = nil, nil
	}
	if err != nil {
		return nil, s.unexpected(ctx, "opensheetsync.Provision: link", err, "project_id", tc.ProjectID)
	}
	at := s.stamp(existing)
	l := existing
	if l == nil {
		l = NewLink(tc.OrgID, tc.ProjectID, tc.UserID, uuid.Nil, at)
	}
	l.Credential, l.APIKeySealed, l.APIKeyHint = CredentialGrant, nil, ""
	l.SheetTitle, l.GoogleFileID = ss.Title, ss.GoogleFileID
	l.Configure(Settings{OSOrg: osp.OrgSlug, OSProject: osp.Slug, Sheets: slugs}, nil, "", at)
	if err := l.Enable(at); err != nil {
		return nil, err
	}
	err = s.uow(ctx, func(ctx context.Context) error {
		if err := s.store.SaveLink(ctx, l); err != nil {
			return err
		}
		if _, err := s.store.MarkAll(ctx, tc.OrgID, tc.ProjectID, at); err != nil {
			return err
		}
		return s.record(ctx, "opensheet.connected", tc)
	})
	if err != nil {
		return nil, s.passOrReport(ctx, "opensheetsync.Provision: save", err, tc)
	}
	span.SetAttributes(attribute.String("opensheetsync.os_project", osp.Slug))
	s.mirror.KickDirty(ctx)
	return l, nil
}

func (s *Service) control(ctx context.Context, tc tenant.Context) (*opensheet.Control, error) {
	token, err := s.creds.Bearer(ctx, &Link{OrgID: tc.OrgID, ProjectID: tc.ProjectID, Credential: CredentialGrant})
	if err != nil {
		return nil, s.passOrReport(ctx, "opensheetsync: grant bearer", err, tc)
	}
	c, err := s.endpoint.control(token)
	if err != nil {
		return nil, s.unexpected(ctx, "opensheetsync: control client", err, "org_id", tc.OrgID, "project_id", tc.ProjectID)
	}
	return c, nil
}

// SECURITY: a refused token response still minted a refresh token at the upstream IdP; revoke it rather than leave it live.
func (s *Service) revokeIssued(ctx context.Context, token string, tc tenant.Context) {
	if token == "" || s.creds.grants == nil || s.creds.grants.rp == nil {
		return
	}
	rp := s.creds.grants.rp
	s.creds.revoke(ctx, func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, revokeTimeout)
		defer cancel()
		return rp.Revoke(ctx, token, "refresh_token")
	}, tc.ProjectID)
}

func refused(err error) bool {
	ce, ok := opensheet.AsConnectError(err)
	return ok && ce.Refused()
}

func (s *Service) controlErr(ctx context.Context, op string, err error, tc tenant.Context) error {
	if refused(err) {
		return &InvalidSettingError{Field: "os_project_id", Reason: "OpenSheet refused it"}
	}
	if ce, ok := opensheet.AsConnectError(err); ok && ce.Status < 500 {
		return s.unexpected(ctx, op, err, "org_id", tc.OrgID, "project_id", tc.ProjectID)
	}
	return &UnavailableError{Cause: err.Error()}
}

func (s *Service) record(ctx context.Context, action string, tc tenant.Context) error {
	if s.audit == nil {
		return nil
	}
	return s.audit.Record(ctx, audit.Event{Action: action, TargetKind: "project", TargetID: tc.ProjectID.String(), ProjectID: tc.ProjectID})
}

func (s *Service) access(ctx context.Context, tc tenant.Context, l *Link) (Access, error) {
	if s.creds.grants == nil || (l != nil && l.Credential != CredentialGrant) {
		return AccessNone, nil
	}
	g, err := s.creds.grants.store.GrantByProject(ctx, tc.OrgID, tc.ProjectID)
	switch {
	case IsGrantNotFoundError(err) && l != nil:
		return AccessMemberLeft, nil
	case IsGrantNotFoundError(err):
		return AccessNone, nil
	case err != nil:
		return AccessNone, err
	case g.Status != GrantOK:
		return AccessReconnect, nil
	case l == nil:
		return AccessPending, nil
	}
	return AccessOK, nil
}
