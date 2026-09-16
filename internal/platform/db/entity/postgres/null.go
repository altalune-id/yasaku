package postgres

import "github.com/go-jet/jet/v2/postgres"

// NOTE: jet's postgres.NULL is a package-level singleton and StringExp/DateExp/TimestampzExp
// call setRoot on their argument, so wrapping it directly is a data race across goroutines.
// CAST builds a fresh expression around NULL and leaves the singleton untouched.

// NullText returns a fresh SQL NULL typed as text.
func NullText() postgres.StringExpression {
	return postgres.CAST(postgres.NULL).AS_TEXT()
}

// NullDate returns a fresh SQL NULL typed as date.
func NullDate() postgres.DateExpression {
	return postgres.CAST(postgres.NULL).AS_DATE()
}

// NullTimestampz returns a fresh SQL NULL typed as timestamptz.
func NullTimestampz() postgres.TimestampzExpression {
	return postgres.CAST(postgres.NULL).AS_TIMESTAMPZ()
}

// NullJSONB returns a fresh SQL NULL typed as jsonb.
func NullJSONB() postgres.StringExpression {
	return postgres.StringExp(postgres.CAST(postgres.NULL).AS("jsonb"))
}
