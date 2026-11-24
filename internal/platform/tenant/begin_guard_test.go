package tenant_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// SECURITY: a Postgres store that opens its own transaction skips BeginTenanted, so a support scope's READ ONLY and the RLS org binding never reach it.
func TestEveryPostgresStoreUsesBeginTenanted(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "..")
	reBegin := regexp.MustCompile(`\.BeginTx\(`)
	scanned := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "node_modules" || d.Name() == ".git" || d.Name() == "gen") {
			return filepath.SkipDir
		}
		if d.IsDir() || d.Name() != "postgres.go" {
			return nil
		}
		if filepath.Dir(path) == filepath.Join(root, "internal", "platform", "tenant") {
			return nil
		}
		scanned++
		b, err := os.ReadFile(path) //nolint:gosec // G304: path comes from a walk of the module tree
		if err != nil {
			return err
		}
		if reBegin.Match(b) {
			t.Errorf("%s opens its own transaction; use tenant.PgConn.BeginTenanted", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if scanned < 10 {
		t.Fatalf("scanned %d postgres.go files; the guard has gone stale", scanned)
	}
}
