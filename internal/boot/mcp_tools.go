package boot

import (
	"errors"
	"maps"
	"slices"
	"strings"

	"altalune.id/yasaku/gen/go/yasaku/v1/yasakuv1mcp"
	"altalune.id/yasaku/internal/controlplane"
	mcpinternal "altalune.id/yasaku/internal/mcp"
	rootmcp "altalune.id/yasaku/mcp"
)

// MCPWiringError reports MCP tool domains the manifest and the declared domain list disagree on.
type MCPWiringError struct {
	Missing []string
	Extra   []string
}

func (e *MCPWiringError) Error() string {
	var parts []string
	if len(e.Missing) > 0 {
		parts = append(parts, "no registrar for "+strings.Join(e.Missing, ", "))
	}
	if len(e.Extra) > 0 {
		parts = append(parts, "undeclared registrar for "+strings.Join(e.Extra, ", "))
	}
	return "mcp wiring: " + strings.Join(parts, "; ")
}

// IsMCPWiringError reports whether err is an MCPWiringError.
func IsMCPWiringError(err error) bool {
	_, ok := errors.AsType[*MCPWiringError](err)
	return ok
}

// MCPToolUnregisteredError reports catalog tools that survived registration unregistered.
type MCPToolUnregisteredError struct {
	Tools []string
}

func (e *MCPToolUnregisteredError) Error() string {
	return "mcp wiring: no tool registered for " + strings.Join(e.Tools, ", ")
}

// IsMCPToolUnregisteredError reports whether err is an MCPToolUnregisteredError.
func IsMCPToolUnregisteredError(err error) bool {
	_, ok := errors.AsType[*MCPToolUnregisteredError](err)
	return ok
}

type mcpToolRegistrar func(*rootmcp.Registry, *controlplane.Server)

// NOTE: add a row here when a .proto starts declaring an (mcp.v1.tool); assertMCPWiring fails boot on a slot nobody filled.
func mcpToolDomains() []string { return publishedMCPDomains() }

// SECURITY: the generated registrations carry no scope of their own — mcpinternal.ScopeFor is the one catalog a tool's scope comes from, so the runtime check cannot drift from it.
func mcpToolManifest() map[string]mcpToolRegistrar {
	return map[string]mcpToolRegistrar{
		"yasaku.v1": func(reg *rootmcp.Registry, apiSrv *controlplane.Server) {
			yasakuv1mcp.RegisterWorkspaceServiceTools(reg, apiSrv.WorkspaceSvc, mcpinternal.ScopeFor)
			yasakuv1mcp.RegisterWalletServiceTools(reg, apiSrv.WalletSvc, mcpinternal.ScopeFor)
			yasakuv1mcp.RegisterCategoryServiceTools(reg, apiSrv.CategorySvc, mcpinternal.ScopeFor)
			yasakuv1mcp.RegisterTransactionServiceTools(reg, apiSrv.TransactionSvc, mcpinternal.ScopeFor)
			yasakuv1mcp.RegisterPeriodServiceTools(reg, apiSrv.PeriodSvc, mcpinternal.ScopeFor)
			yasakuv1mcp.RegisterReportServiceTools(reg, apiSrv.ReportSvc, mcpinternal.ScopeFor)
		},
	}
}

func registerTools(reg *rootmcp.Registry, apiSrv *controlplane.Server) error {
	manifest := mcpToolManifest()
	if err := assertMCPWiring(manifest); err != nil {
		return err
	}
	for _, domain := range mcpToolDomains() {
		manifest[domain](reg, apiSrv)
	}
	return assertMCPTools(reg)
}

func assertMCPWiring(manifest map[string]mcpToolRegistrar) error {
	declared := mcpToolDomains()
	var missing []string
	for _, domain := range declared {
		if manifest[domain] == nil {
			missing = append(missing, domain)
		}
	}
	var extra []string
	for _, domain := range slices.Sorted(maps.Keys(manifest)) {
		if !slices.Contains(declared, domain) {
			extra = append(extra, domain)
		}
	}
	if len(missing) == 0 && len(extra) == 0 {
		return nil
	}
	return &MCPWiringError{Missing: missing, Extra: extra}
}

func assertMCPTools(reg *rootmcp.Registry) error {
	var unregistered []string
	for name := range mcpinternal.ScopeTable() {
		if _, ok := reg.Spec(name); !ok {
			unregistered = append(unregistered, name)
		}
	}
	if len(unregistered) == 0 {
		return nil
	}
	slices.Sort(unregistered)
	return &MCPToolUnregisteredError{Tools: unregistered}
}
