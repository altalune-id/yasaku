package postgres_test

import (
	"strings"
	"sync"
	"testing"

	jetpg "github.com/go-jet/jet/v2/postgres"

	pgent "altalune.id/yasaku/internal/platform/db/entity/postgres"
)

func TestNullHelpersCarryTheColumnType(t *testing.T) {
	t.Parallel()

	// NOTE: Postgres has no assignment cast, so a helper whose cast does not match the column's
	// declared type fails the statement at analyze time, even on a plain insert.
	cases := []struct {
		name string
		expr jetpg.Expression
		want string
	}{
		{"NullText", pgent.NullText(), `SELECT NULL::text AS "v";`},
		{"NullDate", pgent.NullDate(), `SELECT NULL::date AS "v";`},
		{"NullTimestampz", pgent.NullTimestampz(), `SELECT NULL::timestamp with time zone AS "v";`},
		{"NullUUID", pgent.NullUUID(), `SELECT NULL::uuid AS "v";`},
		{"NullJSONB", pgent.NullJSONB(), `SELECT NULL::jsonb AS "v";`},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			query, _ := jetpg.SELECT(tt.expr.AS("v")).Sql()
			if strings.TrimSpace(query) != tt.want {
				t.Fatalf("%s rendered %q, want %q", tt.name, strings.TrimSpace(query), tt.want)
			}
		})
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
		func() jetpg.Expression { return pgent.NullUUID() },
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
