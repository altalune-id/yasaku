package sqlite

import "github.com/go-jet/jet/v2/sqlite"

// NOTE: jet's sqlite.NULL is a package-level singleton and StringExp calls setRoot on its
// argument, so wrapping it directly is a data race across goroutines. CAST builds a fresh
// expression around NULL and leaves the singleton untouched.

// NullText returns a fresh SQL NULL typed as TEXT.
func NullText() sqlite.StringExpression {
	return sqlite.CAST(sqlite.NULL).AS_TEXT()
}
