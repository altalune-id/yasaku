# opensheet

A Go client for the [opensheet](https://github.com/altalune-id/opensheet) HTTP data plane
(see `skills/opensheet-api` in that repo for the wire contract this client implements).

This package has no consumer in `altalune-yasaku` yet. It exists so MVP 2 can wire a
dual-write path against opensheet without a client to write from scratch first.

## Config

```go
c, err := opensheet.New(opensheet.Config{
    BaseURL: "https://opensheet.example.com",
    Org:     "acme",
    Project: "reservations",
    Token:   token,
})
```

`AllowPrivateHosts` must be set to talk to a loopback or private-network `BaseURL` (tests
do this); it is refused otherwise, mirroring the SSRF guard in `httpclient`. `Timeout`,
`Retry`, and `UserAgent` all have sane defaults when left zero.

## Reading rows

`Pages` follows the server's `Link: <...>; rel="next"` header until it runs out; `Rows`
flattens it into individual rows:

```go
for page, err := range c.Pages(ctx, "reservations", opensheet.Query{
    Where: []opensheet.Filter{{Column: "status", Op: "eq", Value: "confirmed"}},
    Limit: 200,
}) {
    if err != nil {
        return err
    }
    if page.Stale {
        // the server served this page from a cache that lagged the sheet
    }
    for _, row := range page.Rows {
        _ = row["id"]
    }
}
```

## Writes, idempotency, and optimistic concurrency

`CreateRow` accepts `WithIdempotencyKey` so a retried POST replays the original result
instead of creating a duplicate row; only that call path retries POST requests.
`WithNumericColumns` marks which row values should be sent as JSON numbers rather than
strings. `ReplaceRow`, `PatchRow`, and `DeleteRow` accept `WithIfMatch(etag)` so the write
fails with `PreconditionFailedError` if the row changed since the caller last read it.

## The 404 mask

`NotFoundError` is the server's single answer for five distinct causes: an unknown slug,
a missing or invalid credential, a key from another project, a key without the needed
scope, or a sheet not granted to the key. The client cannot and does not distinguish
between them.

## Errors

| Status  | Code     | Error                     |
| ------- | -------- | ------------------------- |
| 404     | any      | `NotFoundError`           |
| 412     | any      | `PreconditionFailedError` |
| 409     | `SHT036` | `StaleCursorError`        |
| 429     | any      | `RateLimitedError`        |
| 413     | any      | `PayloadTooLargeError`    |
| 400/422 | any      | `ValidationError`         |
| other   | any      | `APIError`                |

Each has an `Is<Type>Error(err) bool` helper built on `errors.As`.

## Out of scope (MVP 2)

`Append`, `PurgeCache`, `Tabs`, and `CreateTab` are not implemented here.
