package controlplane

import (
	"fmt"
	"strings"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	_ "altalune.id/yasaku/gen/go/yasaku/v1"
	"altalune.id/yasaku/internal/platform/authn"
)

// NOTE: walks every registered yasaku.v1 file, so a new proto file fails the scope tests until it is tabulated.
func yasakuProtoProcedures() []string {
	var out []string
	protoregistry.GlobalFiles.RangeFilesByPackage("yasaku.v1", func(f protoreflect.FileDescriptor) bool {
		svcs := f.Services()
		for i := range svcs.Len() {
			svc := svcs.Get(i)
			methods := svc.Methods()
			for j := range methods.Len() {
				out = append(out, fmt.Sprintf("/%s/%s", svc.FullName(), methods.Get(j).Name()))
			}
		}
		return true
	})
	return out
}

func TestEveryYasakuProcedureHasAnEnforcedAndAFineScope(t *testing.T) {
	enforced := yasakuProcedureScopes()
	fine := YasakuFineProcedureScopes()
	procs := yasakuProtoProcedures()
	if len(procs) == 0 {
		t.Fatal("no yasaku procedure found in the descriptors")
	}
	for _, p := range procs {
		if enforced[p] == "" {
			t.Errorf("%s: no enforced scope", p)
		}
		if fine[p] == "" {
			t.Errorf("%s: no fine scope declared", p)
		}
	}
	if len(enforced) != len(procs) || len(fine) != len(procs) {
		t.Errorf("tables carry %d enforced and %d fine rows, the protos declare %d procedures", len(enforced), len(fine), len(procs))
	}
	for p, s := range enforced {
		if s != authn.ScopeYasakuRead && s != authn.ScopeYasakuWrite {
			t.Errorf("%s: enforced scope %q, want yasaku:read or yasaku:write", p, s)
		}
	}
}

func TestYasakuMutationsNeedTheWriteScope(t *testing.T) {
	reads := []string{"/List", "/Get", "/Now", "/WalletTotals", "/Search", "/Suggest", "/Preview", "Report"}
	for p, s := range yasakuProcedureScopes() {
		read := false
		for _, r := range reads {
			if strings.Contains(p, r) {
				read = true
			}
		}
		if read && s != authn.ScopeYasakuRead {
			t.Errorf("%s is a read, enforced as %q", p, s)
		}
		if !read && s != authn.ScopeYasakuWrite {
			t.Errorf("%s is a mutation, enforced as %q", p, s)
		}
	}
}

func TestFineProcedureScopesAreNotMintable(t *testing.T) {
	for p, s := range YasakuFineProcedureScopes() {
		if authn.Valid(s) {
			t.Errorf("%s: %s is in the catalogue; fine scopes stay TODO until the cutover", p, s)
		}
	}
}

func TestScopeTableEnforcesTheYasakuRows(t *testing.T) {
	table := ScopeTable()
	for p, s := range yasakuProcedureScopes() {
		if table[p] != s {
			t.Errorf("%s: ScopeTable has %q, want %q", p, table[p], s)
		}
	}
}
