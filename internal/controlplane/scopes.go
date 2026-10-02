package controlplane

import (
	apikeyv1connect "altalune.id/yasaku/gen/go/apikey/v1/apikeyv1connect"
	authv1connect "altalune.id/yasaku/gen/go/auth/v1/authv1connect"
	orgv1connect "altalune.id/yasaku/gen/go/org/v1/orgv1connect"
	projectv1connect "altalune.id/yasaku/gen/go/project/v1/projectv1connect"
	"altalune.id/yasaku/internal/platform/authn"
)

// ScopeTable declares the scope a key principal must hold for every mounted procedure. SECURITY: a procedure missing from this map is denied to key principals, not admitted.
func ScopeTable() authn.ScopeTable {
	t := authn.ScopeTable{
		apikeyv1connect.APIKeyServiceListProcedure:           authn.ScopeAPIKeysRead,
		apikeyv1connect.APIKeyServiceCreateProcedure:         authn.ScopeAPIKeysWrite,
		apikeyv1connect.APIKeyServiceRevokeProcedure:         authn.ScopeAPIKeysWrite,
		projectv1connect.ProjectServiceListProjectsProcedure: authn.ScopeYasakuRead,
		orgv1connect.MemberServiceListMembersProcedure:       authn.ScopeMembersRead,
		authv1connect.AuthServiceWhoamiProcedure:             authn.ScopeAPIKeysRead,
	}
	for k, v := range yasakuProcedureScopes() {
		t[k] = v
	}
	return t
}
