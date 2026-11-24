package boot_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/gen/go/yasaku/v1/yasakuv1connect"
	"altalune.id/yasaku/gen/go/yasaku/v1/yasakuv1mcp"
	"altalune.id/yasaku/internal/cli"
	"altalune.id/yasaku/internal/limits"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/testutil/fakes"
	"altalune.id/yasaku/internal/user"
)

// TestReachedError_SameMessageOnConsoleRPCMCPCLI pins one refusal text across surfaces: the console renders limits.Message, the procedure and the MCP tool relay it, the CLI prints it with its code.
func TestReachedError_SameMessageOnConsoleRPCMCPCLI(t *testing.T) {
	f := newMCPFixture(t, mcpOpts{enabled: true})
	f.seedOwnWallet(t)
	_, err := f.srv.Platform.Pool.W.ExecContext(t.Context(),
		"INSERT INTO yasaku_limit_overrides (subject_kind, subject_id, key, value, updated_at) VALUES ('project', ?, 'project.wallets', 1, ?)",
		f.projectID, time.Now().UTC().Format(time.RFC3339Nano))
	require.NoError(t, err)
	want := limits.Message("en-US", config.ModeCloud, &limits.ReachedError{Key: limits.ProjectWallets, Max: 1}, "")

	token := f.issuer.mint(t, controlAudience, readWrite())
	target := map[string]any{"org": "mcp-org", "project": "mcp-project"}
	rec := f.connectRPC(t, token, "/api"+yasakuv1connect.WalletServiceCreateWalletProcedure,
		map[string]any{"target": target, "name": "second", "kind": "cash", "confirm": true})
	var rpc struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &rpc), "body=%s", rec.Body.String())
	require.Equal(t, "failed_precondition", rpc.Code)
	require.Equal(t, want, rpc.Message, "procedure")

	mcpRec := f.call(t, f.issuer.mint(t, mcpAudience, readWrite()), callToolBody(yasakuv1mcp.CreateWalletToolName, map[string]any{
		"target": target, "name": "third", "kind": "cash", "confirm": true,
	}))
	payload := toolPayload(t, mcpRec)
	require.Equal(t, "LIM001", payload.Code)
	require.Equal(t, want, payload.Message, "MCP tool")

	o, err := f.srv.Orgs.BySlug(t.Context(), "mcp-org")
	require.NoError(t, err)
	_, err = f.srv.Platform.Pool.W.ExecContext(t.Context(),
		"INSERT INTO yasaku_limit_overrides (subject_kind, subject_id, key, value, updated_at) VALUES ('org', ?, 'org.projects', 1, ?)",
		o.ID.String(), time.Now().UTC().Format(time.RFC3339Nano))
	require.NoError(t, err)
	owner, err := f.srv.Users.EnsureFromOIDC(t.Context(), user.Claims{Issuer: f.issuer.url, Subject: tokenSubject, Email: tokenEmail, EmailVerified: true, Name: "MCP Agent"})
	require.NoError(t, err)
	_, err = f.srv.Projects.Create(fakes.Owner(t.Context(), tenant.Context{OrgID: o.ID, UserID: owner.ID}), o.ID, "second", "Second")
	wantCLI := limits.Message("en-US", config.ModeCloud, &limits.ReachedError{Key: limits.OrgProjects, Max: 1, Name: "MCP Org"}, "")
	require.Equal(t, "LIM001: "+wantCLI, cli.ErrorLine(err), "CLI")
}
