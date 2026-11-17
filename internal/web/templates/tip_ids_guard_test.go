package templates

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"altalune.id/yasaku/internal/web"
)

var (
	tipCallRE    = regexp.MustCompile(`@Tip(?:Inline)?\(\s*\w+\s*,\s*([^)\s]+)\s*\)`)
	tipConstRE   = regexp.MustCompile(`(\w+TipID)\s*=\s*"([^"]+)"`)
	tipLiteralRE = regexp.MustCompile(`^"([^"]+)"$`)
)

func TestTipIDs_EveryTemplateTipIsRegistered(t *testing.T) {
	t.Parallel()
	registered := map[string]bool{}
	for _, tip := range web.Tips() {
		registered[tip.ID] = true
	}
	consts := map[string]string{}
	gos, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range gos {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range tipConstRE.FindAllStringSubmatch(string(src), -1) {
			consts[m[1]] = m[2]
		}
	}
	templs, err := filepath.Glob("*.templ")
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	for _, f := range templs {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range tipCallRE.FindAllStringSubmatch(string(src), -1) {
			calls++
			arg := m[1]
			id, isConst := consts[arg]
			if !isConst {
				lit := tipLiteralRE.FindStringSubmatch(arg)
				if lit == nil {
					t.Errorf("%s: tip id %s is neither a literal nor a *TipID constant", f, arg)
					continue
				}
				id = lit[1]
			}
			if !registered[id] {
				t.Errorf("%s: tip %q is not registered in web.Tips(), so it never shows", f, id)
			}
		}
	}
	if calls == 0 || len(consts) == 0 {
		t.Fatalf("found %d tip calls and %d tip id constants; the scan matches nothing", calls, len(consts))
	}
}
