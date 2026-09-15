package org

import (
	"testing"

	"github.com/go-jet/jet/v2/postgres"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	pdb "altalune.id/yasaku/internal/platform/db"
)

func TestPostgresStore_WrapperStatementsBindValuesNotInterpolate(t *testing.T) {
	s := newPostgresStore(pdb.Pool{}, nil, "public", "yasaku_")
	injected := `acme'; DROP TABLE yasaku_orgs; --`
	userID := uuid.New()

	slugSQL, slugArgs := postgres.RawStatement(s.resolveBySlugStmt, postgres.RawArgs{"#slug": injected}).Sql()
	require.Contains(t, slugSQL, "public.yasaku_resolve_org_by_slug($1)")
	require.NotContains(t, slugSQL, "DROP TABLE", "the slug must be bound, never interpolated")
	require.Equal(t, []any{injected}, slugArgs)

	listSQL, listArgs := postgres.RawStatement(s.listForUserStmt, postgres.RawArgs{"#userID": userID}).Sql()
	require.Contains(t, listSQL, "public.yasaku_list_orgs_for_user($1) AS o")
	require.Contains(t, listSQL, "ORDER BY o.created_at ASC, o.id ASC",
		"the outer statement must re-order: Postgres may inline the wrapper and drop its internal ORDER BY")
	require.NotContains(t, listSQL, userID.String(), "the user id must be bound, never interpolated")
	require.Equal(t, []any{userID}, listArgs)
}

func TestPostgresStore_WrapperStatementsProjectEveryOrgColumn(t *testing.T) {
	s := newPostgresStore(pdb.Pool{}, nil, "", "yasaku_")

	for name, stmt := range map[string]string{
		"resolveBySlug": s.resolveBySlugStmt,
		"listForUser":   s.listForUserStmt,
	} {
		for _, col := range []string{"orgs.id", "orgs.slug", "orgs.name", "orgs.created_by", "orgs.created_at", "orgs.system"} {
			require.Contains(t, stmt, `AS "`+col+`"`, "%s must project %s or qrm drops it", name, col)
		}
		require.Contains(t, stmt, "public.yasaku_", "%s must qualify the wrapper with the configured schema", name)
	}
	require.NotContains(t, s.resolveBySlugStmt, "ORDER BY",
		"resolve_org_by_slug is a LIMIT 1 lookup — ordering it would be noise")
}
