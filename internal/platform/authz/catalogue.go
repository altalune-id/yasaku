package authz

import (
	"slices"

	"altalune.id/yasaku/internal/platform/authn"
)

// The permissions every app carries.
const (
	ProjectsWrite Permission = "projects:write"
	MembersWrite  Permission = "members:write"
	APIKeysWrite  Permission = "apikeys:write"
	RolesWrite    Permission = "roles:write"
)

func coreDefs() []Def {
	return []Def{
		{Permission: MembersWrite, LabelKey: "authz.perm.members_write", Holder: HolderConsole},
		{Permission: APIKeysWrite, LabelKey: "authz.perm.apikeys_write", Holder: HolderConsole},
		{Permission: RolesWrite, LabelKey: "authz.perm.roles_write", Holder: HolderConsole},
	}
}

// ScopeHeld reports whether held lets its holder put scope on a key: a permission the scope admits, or for a scope that admits none, the permission guarding it.
func ScopeHeld(held []Permission, scope string) bool {
	if guard, ok := scopeGuard(scope); ok {
		return slices.Contains(held, guard)
	}
	admitsAny := false
	for _, d := range catalogue() {
		if d.Scope != scope {
			continue
		}
		admitsAny = true
		if slices.Contains(held, d.Permission) {
			return true
		}
	}
	return !admitsAny
}

func scopeGuard(scope string) (Permission, bool) {
	switch scope {
	case authn.ScopeAPIKeysRead, authn.ScopeAPIKeysWrite:
		return APIKeysWrite, true
	}
	return "", false
}
