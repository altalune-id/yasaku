package dataplane

// NOTE: yasaku removed posts:* from the authn catalog and does not mount this surface (internal/boot/surfaces_yasaku.go); the names stay here so the reference code compiles.
const (
	ScopePostsRead  = "posts:read"
	ScopePostsWrite = "posts:write"
	ScopePostsAdmin = "posts:admin"
)
