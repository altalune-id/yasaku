package boot

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestKernelQueueReachesNoYasakuService(t *testing.T) {
	t.Parallel()

	// NOTE: any value's .Queue (k, kernel, svcs.Kernel, a renamed local); the cfg.Queue config block and a package's Queue type (opensheetsync.Queue) are not values.
	reQueue := regexp.MustCompile(`(\w+)\.Queue\b`)
	reImport := regexp.MustCompile(`(?m)^\s*(?:(\w+)\s+)?"(?:[^"]*/)?(\w+)"$`)
	allowed := map[string]bool{
		"server.go: kernel.Queue = q": false,
		"services.go: todos := todo.NewService(todoStore, log, reporter.Unexpected, k.Queue)": false,
		"services.go: jobs := newJobSubmitter(k.Queue, k.Tracer, log)":                        false,
	}

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob boot: %v", err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, rErr := os.ReadFile(f) //nolint:gosec // G304: path comes from a package-local glob
		if rErr != nil {
			t.Fatalf("read %s: %v", f, rErr)
		}
		notValue := map[string]bool{"cfg": true}
		for _, m := range reImport.FindAllStringSubmatch(string(b), -1) {
			notValue[m[2]] = true
			if m[1] != "" {
				notValue[m[1]] = true
			}
		}
		for line := range strings.Lines(string(b)) {
			if !slices.ContainsFunc(reQueue.FindAllStringSubmatch(line, -1), func(m []string) bool { return !notValue[m[1]] }) {
				continue
			}
			site := f + ": " + strings.TrimSpace(line)
			if _, ok := allowed[site]; !ok {
				t.Errorf("%s: a yasaku service takes svcs.jobs, never Kernel.Queue — a disabled queue drops a Submit (template gap T32)", site)
				continue
			}
			allowed[site] = true
		}
	}
	for site, seen := range allowed {
		if !seen {
			t.Errorf("%s is gone — the guard went stale; update the allowlist", site)
		}
	}
}
