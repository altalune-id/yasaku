package boot

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// TestAsSystem_OnlyInBootWrappers scans every non-test Go file's calls. SECURITY: AsSystem holds every permission, so only the background-work wrappers in system.go may call it.
func TestAsSystem_OnlyInBootWrappers(t *testing.T) {
	fset := token.NewFileSet()
	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		if path == filepath.Join("..", "boot", "system.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		if callsPkgFunc(f, importName(f, authzPkg), "AsSystem") {
			t.Errorf("%s calls authz.AsSystem; only boot's background-work wrappers may", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
