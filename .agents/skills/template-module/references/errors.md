# Typed errors and error codes

## The error type

One struct per failure mode in `errors.go`. No `Kind int` enum, no sentinel `var Err...`.

```go
// NotFoundError reports that no post matched the lookup.
type NotFoundError struct{ ID string }

func (e *NotFoundError) Error() string {
	return "blog: not found: id=" + e.ID
}

// ToAppError converts the typed error into the wire envelope.
func (*NotFoundError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodePostNotFound,
		"Post not found",
		codes.NotFound,
		&apperrorv1.ErrorDetail{Code: apperror.CodePostNotFound},
	)
}

// IsNotFoundError reports whether err's tree contains a *NotFoundError.
func IsNotFoundError(err error) bool {
	_, ok := errors.AsType[*NotFoundError](err)
	return ok
}
```

The helper is `Is<FullTypeName>` — `InvalidTitleError` → `IsInvalidTitleError`. Never `IsErr*`,
never a shortened form. This is checked in review because a mismatched helper name is invisible
to the compiler and callers silently stop matching.

`Error()` format is `"<module>: <situation>: <cause>"`, e.g. `"todo: title: over 200 characters"`.

Wire code calls `apperror.AsAppError` and never switches on concrete error types.

## Error codes

Codes live in `internal/apperror/codes.go` as `<DOM><NNN>` — a three-letter domain mnemonic plus
a per-domain sequence. They are **append-only**: users quote them off an error page, so a code is
never renumbered and a retired code is never reused. `900`-`999` is reserved for unexpected
failures.

Every code needs a matching row in `docs/ERROR_CODES.md`. `internal/apperror/codes_test.go`
parses both files and fails in **both** directions — a code with no doc row, and a doc row with
no code. Add them in the same change.

Pick a mnemonic that names what the user was doing, not the package topology. A module with
subdomains is usually clearer with one mnemonic per subdomain (`BLG`, `CAT`, `TAG`) than one
shared block, because the code appears on the user's screen.

After editing the markdown, run `pnpm exec prettier --write docs/ERROR_CODES.md` — lint-staged
checks markdown formatting and hand-aligned tables will not match.

## Which failures get a type

Expected failures — anything a caller might reasonably branch on, or a user might see. Not
found, already exists, invalid input, in use, forbidden transition.

Unexpected failures go through `s.unexpected(ctx, "<name>.<Method>: <situation>", err, k, v...)`
and surface as the generic unexpected code. Do not invent a typed error for a driver failure.

## Constraint violations become typed errors

Adapters translate driver errors; they never leak `*pgconn.PgError` or a SQLite error. See
`references/persistence.md` for the exact codes per driver — including the non-obvious one, where
`ON DELETE RESTRICT` reports a different SQLite errcode than an insert-side FK violation.
