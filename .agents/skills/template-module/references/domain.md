# Domain layer: aggregate, Store, service

## File set

```
internal/<name>/
  <name>.go   aggregate root, New() with invariants, value types, ListOpts
  store.go    Store interface (the driven port)
  errors.go   typed errors
  service.go  Service + NewService
  factory.go  NewStore driver dispatch
  postgres.go / sqlite.go
  <name>_test.go service_test.go sqlite_test.go postgres_integration_test.go
```

`<name>.go`, `store.go` and `errors.go` are under the `domain-purity` depguard rule. Allowed
imports: stdlib (`$gostd`), `github.com/google/uuid`, `platform/tenant`, `apperror`, the apperror
proto, `reqid`, `grpc/codes`. Anything else — a markdown renderer, a driver — goes in its own
file in the same package, which is not covered by the rule.

Note the rule permits all of stdlib. `MODULE_TEMPLATE.md` says "no `net/http`, no `database/sql`";
that overstates it. The real boundary is third-party and cross-package imports.

## Aggregate

Exported fields. `uuid.UUID` ids, `time.Time` UTC timestamps, no strings where a type exists, no
JSON tags (wire shapes live in `api/` and `web/`).

`New(...)` enforces creation invariants and returns typed domain errors — never a bare
`fmt.Errorf`. Mutations are methods on the aggregate, not logic in the service.

Lifecycle transitions belong here. Prefer idempotent methods that return nothing over ones that
error on a repeat, because POST-and-redirect means a double submit is normal:

```go
func (p *Post) Publish()   // sets FirstPublishedAt only when nil
func (p *Post) Unpublish() // returns to draft, retains FirstPublishedAt
```

Name a field for what it means across every state. `FirstPublishedAt` survives an unpublish, so
after one the state is `FirstPublishedAt != nil && Status == draft` — `PublishedAt` would invite
a consumer to read non-nil as "currently published". Keep one field as the source of truth for
the question people actually ask.

A setter that takes a collection should normalise it — de-duplicate, preserve order — so the
store never has to defend against bad input.

## Store

```go
type Store interface {
	Save(ctx context.Context, p *Post) error
	ByID(ctx context.Context, id uuid.UUID) (*Post, error)
	List(ctx context.Context, orgID, projectID uuid.UUID, opts ListOpts) ([]*Post, error)
	Delete(ctx context.Context, id uuid.UUID) error
}
```

Verbs only. `context.Context` first. Domain-shaped results, typed errors. No `*sql.Tx` in any
signature — atomicity comes from the unit-of-work helpers, not from passing transactions around.

Batch lookups return a map keyed by id (`ByIDs(ctx, orgID, projectID, ids) (map[uuid.UUID]*T,
error)`): an unresolvable id is simply absent, ordering is the caller's business, and it avoids
N+1 in list views. Take `orgID, projectID` explicitly — without the project the store can only
scope by org, which leaks across projects inside one org.

`ListOpts` is a struct of optional filters whose zero value means "everything in scope".

## Service

```go
type Service struct {
	store      Store
	log        *slog.Logger
	unexpected apperror.UnexpectedFunc
	// module-specific deps after the canonical three
}
```

Every method opens a span — `ctx, span := tracer.Start(ctx, "<name>.<Method>"); defer span.End()`
with a package-level `tracer`.

**A method taking a bare id must verify scope after loading**, because the store filters by org
but not project:

```go
if c.OrgID != tc.OrgID || c.ProjectID != tc.ProjectID {
	return nil, &NotFoundError{ID: id.String()}
}
```

Return `NotFoundError`, not a distinct "wrong project" error — the caller should learn nothing
about rows outside its scope. Route `Delete` and `Rename` through the same check rather than
copying it; a `Delete` that never loads the row has no scope check at all.

Keep the service pure. It does not take another module's `Store`. Resolving a related aggregate's
display name is composition, and composition happens at the edges — the web handler and the RPC
service, which already hold every service they need.

A method needing three or more dependencies beyond `store/log/unexpected` becomes its own struct
in its own file (see `internal/invite/send.go`, `internal/user/onboard.go`).

## Subdomains

Split into `internal/<name>/<sub>/` only when the subdomain has its own aggregate root, its own
`Store`, **and** its own table. Otherwise add files — more files in one package is not a problem.

Cross-reference by UUID only. `Post.CategoryID uuid.UUID`, never `*category.Category`.

Duplicating a small helper (a slug function) across sibling packages is preferred over a shared
import here, because the purity allow-list would reject the import and because the repo already
duplicates it in `org` and `project`.
