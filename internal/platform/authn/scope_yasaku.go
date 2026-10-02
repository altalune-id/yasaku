package authn

// TODO(scopes): replace yasaku:* with the fine scopes in internal/controlplane/scopes_yasaku.go and internal/mcp/scopes_yasaku.go, then remove yasaku:*.
const (
	ScopeYasakuRead  = "yasaku:read"
	ScopeYasakuWrite = "yasaku:write"
)
