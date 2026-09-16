package api

import (
	"context"
	"strings"

	"connectrpc.com/connect"

	yasakuv1 "altalune.id/yasaku/gen/go/yasaku/v1"
	"altalune.id/yasaku/internal/ledger"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/project"
	"altalune.id/yasaku/money"
)

// LedgerService implements yasaku.v1.LedgerService.
type LedgerService struct {
	scope   scopeResolver
	ledgers *ledger.Service
}

// NewLedgerService binds the handler to its collaborators.
func NewLedgerService(orgs *org.Service, projects *project.Service, ledgers *ledger.Service) *LedgerService {
	return &LedgerService{scope: scopeResolver{orgs: orgs, projects: projects}, ledgers: ledgers}
}

// GetSettings returns the project's ledger settings, or the defaults when none were saved.
func (s *LedgerService) GetSettings(ctx context.Context, req *connect.Request[yasakuv1.GetSettingsRequest]) (*connect.Response[yasakuv1.GetSettingsResponse], error) {
	sc, nd, err := s.scope.resolve(ctx, req.Msg.GetTarget())
	if err != nil {
		return nil, err
	}
	if nd != nil {
		return nil, needsErr(nd)
	}
	settings, err := s.ledgers.Get(sc.ctx)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&yasakuv1.GetSettingsResponse{Settings: toProtoSettings(settings)}), nil
}

// UpdateSettings applies the named fields; an absent field is left alone.
func (s *LedgerService) UpdateSettings(ctx context.Context, req *connect.Request[yasakuv1.UpdateSettingsRequest]) (*connect.Response[yasakuv1.UpdateSettingsResponse], error) {
	sc, nd, err := s.scope.resolve(ctx, req.Msg.GetTarget())
	if err != nil {
		return nil, err
	}
	if nd != nil {
		return nil, needsErr(nd)
	}
	patch := ledger.Patch{}
	if tz := req.Msg.Timezone; tz != nil {
		v := strings.TrimSpace(*tz)
		patch.Timezone = &v
	}
	if code := req.Msg.Currency; code != nil {
		cur, pErr := money.ParseCurrency(strings.TrimSpace(*code))
		if pErr != nil {
			return nil, invalidArgCause("currency", "unknown currency "+*code, pErr)
		}
		patch.Currency = &cur
	}
	if day := req.Msg.PeriodStartDay; day != nil {
		v := int(*day)
		patch.PeriodStartDay = &v
	}
	settings, err := s.ledgers.Update(sc.ctx, patch)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&yasakuv1.UpdateSettingsResponse{Settings: toProtoSettings(settings)}), nil
}
