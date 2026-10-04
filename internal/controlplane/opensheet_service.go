package controlplane

import (
	"context"
	"time"

	"connectrpc.com/connect"

	yasakuv1 "altalune.id/yasaku/gen/go/yasaku/v1"
	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/opensheetsync"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/project"
)

// OpensheetService implements yasaku.v1.OpensheetService; boot mounts it only with the Opensheet module.
type OpensheetService struct {
	orgs     *org.Service
	projects *project.Service
	links    *opensheetsync.Service
}

// NewOpensheetService binds the handler to its collaborators.
func NewOpensheetService(orgs *org.Service, projects *project.Service, links *opensheetsync.Service) *OpensheetService {
	return &OpensheetService{orgs: orgs, projects: projects, links: links}
}

// GetOpensheetLink returns the project's link and backlog, without the key.
func (s *OpensheetService) GetOpensheetLink(ctx context.Context, req *connect.Request[yasakuv1.GetOpensheetLinkRequest]) (*connect.Response[yasakuv1.GetOpensheetLinkResponse], error) {
	sc, err := s.scope(ctx, req.Msg.GetTarget())
	if err != nil {
		return nil, err
	}
	link, err := s.current(sc.ctx)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&yasakuv1.GetOpensheetLinkResponse{Link: link}), nil
}

// TestOpensheetLink checks the settings against opensheet; failing tabs come back as checks, not as an error.
func (s *OpensheetService) TestOpensheetLink(ctx context.Context, req *connect.Request[yasakuv1.TestOpensheetLinkRequest]) (*connect.Response[yasakuv1.TestOpensheetLinkResponse], error) {
	sc, err := s.scope(ctx, req.Msg.GetTarget())
	if err != nil {
		return nil, err
	}
	cl, err := s.links.Test(sc.ctx, fromProtoOpensheetSettings(req.Msg.GetSettings()))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&yasakuv1.TestOpensheetLinkResponse{Checks: toProtoTabChecks(cl), Ok: cl.OK()}), nil
}

// SaveOpensheetLink saves the settings only when the server-side Test passes; otherwise it fails with the first failing tab's OSL code.
func (s *OpensheetService) SaveOpensheetLink(ctx context.Context, req *connect.Request[yasakuv1.SaveOpensheetLinkRequest]) (*connect.Response[yasakuv1.SaveOpensheetLinkResponse], error) {
	sc, err := s.scope(ctx, req.Msg.GetTarget())
	if err != nil {
		return nil, err
	}
	if _, _, err := s.links.Save(sc.ctx, fromProtoOpensheetSettings(req.Msg.GetSettings())); err != nil {
		return nil, err
	}
	link, err := s.current(sc.ctx)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&yasakuv1.SaveOpensheetLinkResponse{Link: link}), nil
}

// SetOpensheetLinkEnabled turns the mirror on, with a backfill, or off.
func (s *OpensheetService) SetOpensheetLinkEnabled(ctx context.Context, req *connect.Request[yasakuv1.SetOpensheetLinkEnabledRequest]) (*connect.Response[yasakuv1.SetOpensheetLinkEnabledResponse], error) {
	sc, err := s.scope(ctx, req.Msg.GetTarget())
	if err != nil {
		return nil, err
	}
	if _, err := s.links.SetEnabled(sc.ctx, req.Msg.GetEnabled()); err != nil {
		return nil, err
	}
	link, err := s.current(sc.ctx)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&yasakuv1.SetOpensheetLinkEnabledResponse{Link: link}), nil
}

// SyncOpensheetNow marks every row of the project for the mirror.
func (s *OpensheetService) SyncOpensheetNow(ctx context.Context, req *connect.Request[yasakuv1.SyncOpensheetNowRequest]) (*connect.Response[yasakuv1.SyncOpensheetNowResponse], error) {
	sc, err := s.scope(ctx, req.Msg.GetTarget())
	if err != nil {
		return nil, err
	}
	n, err := s.links.SyncNow(sc.ctx)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&yasakuv1.SyncOpensheetNowResponse{Marked: n}), nil
}

// DeleteOpensheetLink removes the link and its sync state; the sheet is left alone.
func (s *OpensheetService) DeleteOpensheetLink(ctx context.Context, req *connect.Request[yasakuv1.DeleteOpensheetLinkRequest]) (*connect.Response[yasakuv1.DeleteOpensheetLinkResponse], error) {
	sc, err := s.scope(ctx, req.Msg.GetTarget())
	if err != nil {
		return nil, err
	}
	if err := s.links.Delete(sc.ctx); err != nil {
		return nil, err
	}
	return connect.NewResponse(&yasakuv1.DeleteOpensheetLinkResponse{}), nil
}

func (s *OpensheetService) current(ctx context.Context) (*yasakuv1.OpensheetLink, error) {
	st, err := s.links.Status(ctx)
	if err != nil {
		return nil, err
	}
	return toProtoOpensheetLink(st.Link, st.Backlog), nil
}

func (s *OpensheetService) scope(ctx context.Context, t *yasakuv1.Target) (targetResult, error) {
	sc, nd, err := scopeToTarget(ctx, s.orgs, s.projects, t)
	if err != nil {
		return targetResult{}, err
	}
	if nd != nil {
		return targetResult{}, needsErr(nd)
	}
	return sc, nil
}

func fromProtoOpensheetSettings(p *yasakuv1.OpensheetSettings) opensheetsync.Settings {
	return opensheetsync.Settings{
		OSOrg: p.GetOsOrg(), OSProject: p.GetOsProject(), APIKey: p.GetApiKey(),
		Sheets: opensheetsync.SheetSlugs{Transactions: p.GetTransactionsSheet(), Wallets: p.GetWalletsSheet(), Categories: p.GetCategoriesSheet()},
	}
}

func toProtoOpensheetLink(l *opensheetsync.Link, b opensheetsync.Backlog) *yasakuv1.OpensheetLink {
	if l == nil {
		return nil
	}
	return &yasakuv1.OpensheetLink{
		OsOrg: l.OSOrg, OsProject: l.OSProject, ApiKeyHint: l.APIKeyHint,
		TransactionsSheet: l.Sheets.Transactions, WalletsSheet: l.Sheets.Wallets, CategoriesSheet: l.Sheets.Categories,
		Enabled: l.Enabled, VerifiedAt: opensheetTime(l.VerifiedAt), LastError: l.LastError, LastSyncedAt: opensheetTime(l.LastSyncedAt),
		AutoDisabled: l.AutoDisabled(), Pending: b.Pending, Failing: b.Failing, GivenUp: b.GivenUp,
		AwaitingFirstSync: l.AwaitingFirstSync(),
	}
}

func toProtoTabChecks(cl opensheetsync.Checklist) []*yasakuv1.OpensheetTabCheck {
	out := make([]*yasakuv1.OpensheetTabCheck, 0, len(cl))
	for _, c := range cl {
		code := ""
		if ae, ok := apperror.AsAppError(c.Err); ok {
			code = ae.Code()
		}
		out = append(out, &yasakuv1.OpensheetTabCheck{
			Entity: string(c.Entity), Sheet: c.Sheet, Ok: c.OK(), Reachable: c.Reachable, IdColumn: c.IDColumn,
			Writable: c.Writable, Missing: c.Missing, ContractReason: c.ContractReason, Code: code, ColumnsDeferred: c.ColumnsDeferred,
		})
	}
	return out
}

func opensheetTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
