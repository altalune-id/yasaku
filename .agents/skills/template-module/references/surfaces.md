# Surfaces: web, RPC, CLI

A module needs only the surfaces it is actually used through. Adding all three by default is
how an example module becomes three times the size it needs to be.

## Web (templ + HTMX)

`internal/web/handlers/<name>.go` with a `Register(mux *http.ServeMux)` method, modelled on
`internal/web/handlers/todo.go` — `requireProject`, a `projectScope` helper, fragment rendering.

Project-scoped routes follow `/orgs/{org}/projects/{project}/<plural>`.

**Register routes as literal calls.** `internal/boot/route_scope_test.go` scrapes handler sources
with the regex `mux\.HandleFunc\("([A-Z]+) ([^"]+)"`. A loop or a helper is invisible to it and
the route becomes silently unprobed — worse than a failing test. Add every route to
`probeRoutes()` in the same change.

**Handlers are wired in `internal/boot/http.go`**, not `internal/web/server.go`. Add parameters
to `buildWebHandler`, append to its `AppHandlers` slice, and update the call site in
`internal/boot/server.go`.

Every probe runs twice — as a user with no org, and as an org member — asserting `< 500` and no
tenant-scope error in the log. So a nonexistent `{id}` must 404 and a caller with no org must 303. A refused delete renders the list with an error banner, never a 500.

Know what the probes do _not_ cover: they stop at project resolution because the probe fixture
has no project row, so they never enter handler bodies. They prove a route is registered, not
that it works. Write a handler test that boots a server, seeds a project, and exercises the real
flow.

**HTMX fragments need the full layout context.** Rendering a fragment with a bare base leaves
`ActiveOrg` nil, so `d.ProjectPath` collapses to `/orgs` and every action URL in the swapped
markup breaks. Type checking and route probes both stay green; it only shows up in a browser.

Rendered HTML reaches a page through `@templ.Raw(...)`. A plain templ expression escapes it and
looks like a rendering bug.

## i18n

Every `d.Tr` key must exist in all five locale files or the pre-commit hook fails.

**Do not run `go tool i18n-lint --fix`.** It yaml-marshals the whole map, re-sorting every
existing key and dropping header comments — a wholesale rewrite of the translated files. Append
new keys by hand: real `en-US` values, empty stubs elsewhere, every plural form present for a
`TrN` key. An empty value satisfies `-check`; a missing key does not.

## RPC (Connect)

`api/<name>/v1/<name>.proto`, then `make generate`. The service is
`internal/api/<name>_service.go` with `New<Name>Service(...)`, registered in
`internal/api/server.go` and added to `internal/api/client.go`.

`api.New(...)` is a positional constructor with call sites in `internal/boot/http.go` and two
test helpers. Adding services grows it; it is past the point where it should be an options
struct.

Shape decisions worth copying:

- **Set by id, return nested.** Requests carry `category_id` and `repeated string tag_ids`;
  responses carry the nested messages. A consumer rendering the record should not need a second
  call.
- **Do not offer set-by-name** for a related aggregate — it lets one module's write silently
  create another module's vocabulary.
- **Lifecycle transitions get their own RPCs** (`PublishPost`, `UnpublishPost`) rather than a
  `status` field on update, so the transition stays visible on the wire.
- **Update is full replacement**, and say so. An absent `repeated` field clears the set — proto3
  has no field presence for repeated fields, so that is the only implementable reading.
- **Render server-side.** If the record has markdown, return the rendered HTML alongside the
  source so a consumer cannot reintroduce the escaping bug the renderer closes.

CI runs `buf lint` and a proto-drift check, so generated output must be committed.

## CLI

`internal/cli/<name>.go`, following `docs/CLI_CONTRACT.md`. Commands are built by factories, not
package-level vars; business logic lives outside `cmd/` and knows nothing about Cobra or Viper.
Use `RunE`, validate positional args with `cobra.Args`, and print through `cmd.OutOrStdout()`.
