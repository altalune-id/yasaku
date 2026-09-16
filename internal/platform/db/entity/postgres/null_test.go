package postgres_test

import (
	"strings"
	"sync"
	"testing"

	jetpg "github.com/go-jet/jet/v2/postgres"

	pgent "altalune.id/yasaku/internal/platform/db/entity/postgres"
)

func nullExprSQL(t *testing.T, name string, expr jetpg.Expression) string {
	t.Helper()

	query, _ := jetpg.SELECT(expr.AS("v")).Sql()
	if !strings.Contains(query, "NULL") {
		t.Fatalf("%s did not serialize to a NULL cast: %q", name, query)
	}
	return query
}

func TestNullHelpersSerializeAsNull(t *testing.T) {
	t.Parallel()

	cases := map[string]jetpg.Expression{
		"NullText":       pgent.NullText(),
		"NullDate":       pgent.NullDate(),
		"NullTimestampz": pgent.NullTimestampz(),
		"NullJSONB":      pgent.NullJSONB(),
	}
	for name, expr := range cases {
		nullExprSQL(t, name, expr)
	}
}

func TestNullHelpersAreConcurrencySafe(t *testing.T) {
	t.Parallel()

	const goroutines = 8
	const iterations = 200

	builders := []func() jetpg.Expression{
		func() jetpg.Expression { return pgent.NullText() },
		func() jetpg.Expression { return pgent.NullDate() },
		func() jetpg.Expression { return pgent.NullTimestampz() },
		func() jetpg.Expression { return pgent.NullJSONB() },
	}

	var wg sync.WaitGroup
	for i := range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			build := builders[i%len(builders)]
			for range iterations {
				query, _ := jetpg.SELECT(build().AS("v")).Sql()
				if !strings.Contains(query, "NULL") {
					t.Errorf("unstable serialization: %q", query)
					return
				}
			}
		}()
	}
	wg.Wait()
}
