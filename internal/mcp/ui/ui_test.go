package ui

import (
	"os"
	"strings"
	"testing"
)

func mustRead(t *testing.T, name string) string {
	t.Helper()
	b, err := files.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

func TestDocumentAssembles(t *testing.T) {
	doc := Document()
	for _, want := range []string{"<!doctype html>", `id="root"`, "ext-apps"} {
		if !strings.Contains(doc, want) {
			t.Errorf("Document() missing %q", want)
		}
	}
	if n := strings.Count(doc, `id="root"`); n != 1 {
		t.Errorf(`id="root" appears %d times, want 1`, n)
	}
	if strings.Contains(doc, "{{.") {
		t.Error("Document() has an unexpanded template action")
	}
}

func TestDocumentIncludesEveryScriptPartOnce(t *testing.T) {
	doc := Document()
	for _, p := range scriptParts {
		src := mustRead(t, p)
		if n := strings.Count(doc, src); n != 1 {
			t.Errorf("%s appears %d times in Document(), want 1", p, n)
		}
	}
}

func TestNoHostSpecificAPIs(t *testing.T) {
	for _, p := range scriptParts {
		if strings.Contains(mustRead(t, p), "window.openai") {
			t.Errorf("%s uses window.openai; the bundle must stay portable across hosts", p)
		}
	}
}

func TestExposeExportsBindsEveryNameTheBridgeUses(t *testing.T) {
	doc := Document()
	if !strings.Contains(doc, "globalThis.__extApps={") {
		t.Fatal("Document() never exposes the bundle's exports; the bridge will find nothing")
	}
	for _, name := range []string{"App", "applyDocumentTheme", "applyHostStyleVariables", "applyHostFonts"} {
		if !strings.Contains(doc, name+":") {
			t.Errorf("__extApps does not bind %q — ext-apps renamed or dropped it", name)
		}
	}
}

func TestVendorIsSafeToInline(t *testing.T) {
	src := mustRead(t, vendorPart)
	for _, bad := range []string{"</script", "<!--"} {
		if strings.Contains(src, bad) {
			t.Errorf("vendored bundle contains %q, which would truncate the inlined document", bad)
		}
	}
}

func TestResourceURIMatchesBufPrefix(t *testing.T) {
	const want = "ui://yasaku/app"
	if ResourceURI != want {
		t.Errorf("ResourceURI = %q, want %q — it must match buf.gen.yaml's ui_prefix plus the proto's ui name", ResourceURI, want)
	}
}

func TestDumpBundle(t *testing.T) {
	path := os.Getenv("YASAKU_UI_DUMP")
	if path == "" {
		t.Skip("set YASAKU_UI_DUMP=path to write the assembled bundle")
	}
	if err := os.WriteFile(path, []byte(Document()), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Logf("wrote %s", path)
}
