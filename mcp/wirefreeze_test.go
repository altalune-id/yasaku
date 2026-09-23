package mcp

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

//nolint:gochecknoglobals // a -update flag for golden files has to be package level.
var updateGolden = flag.Bool("update", false, "rewrite golden files")

func checkWireGolden(t *testing.T, path string, v any) {
	t.Helper()
	got, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got = append(got, '\n')

	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatalf("mkdir golden dir: %v", err)
		}
		if err := os.WriteFile(path, got, 0o600); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(path) //nolint:gosec // fixed test path
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("wire changed for %s.\ngot:\n%s\nwant:\n%s", path, got, want)
	}
}

func wireFreezeServer() *Server {
	s := NewServer("yasaku", "test", staticScopes(string(ScopeRead), string(ScopeWrite)))
	s.Register(readSpec(), echoHandler)
	s.Register(writeSpec(), echoHandler)
	return s
}

func TestToolsListWireIsFrozen(t *testing.T) {
	cs := connect(t, wireFreezeServer())
	res, err := cs.ListTools(t.Context(), &sdk.ListToolsParams{})
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	checkWireGolden(t, "testdata/golden/tools_list.json", res.Tools)
}

func TestInitializeCapabilitiesAreFrozen(t *testing.T) {
	cs := connect(t, wireFreezeServer())
	checkWireGolden(t, "testdata/golden/initialize.json", cs.InitializeResult().Capabilities)
}

func wireFreezeUIServer(t *testing.T) *Server {
	t.Helper()
	s := NewServer("yasaku", "test", staticScopes(string(ScopeRead), string(ScopeWrite)), WithUI(true))
	s.Register(uiSpec(), echoHandler)
	s.AddUIResource(UIResource{
		URI:           "ui://yasaku/app",
		Name:          "yasaku",
		Body:          "<html></html>",
		PrefersBorder: new(bool),
	})
	return s
}

func TestToolsListWireIsFrozenWithUI(t *testing.T) {
	cs := connect(t, wireFreezeUIServer(t))
	res, err := cs.ListTools(t.Context(), &sdk.ListToolsParams{})
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	checkWireGolden(t, "testdata/golden/tools_list_ui.json", res.Tools)
}

func TestResourcesListWireIsFrozen(t *testing.T) {
	cs := connect(t, wireFreezeUIServer(t))
	res, err := cs.ListResources(t.Context(), &sdk.ListResourcesParams{})
	if err != nil {
		t.Fatalf("list resources: %v", err)
	}
	checkWireGolden(t, "testdata/golden/resources_list.json", res.Resources)
}

func TestResourceReadWireIsFrozen(t *testing.T) {
	cs := connect(t, wireFreezeUIServer(t))
	res, err := cs.ReadResource(t.Context(), &sdk.ReadResourceParams{URI: "ui://yasaku/app"})
	if err != nil {
		t.Fatalf("read resource: %v", err)
	}
	checkWireGolden(t, "testdata/golden/resource_read.json", res.Contents)
}

func TestAddUIResourceWithoutMetaEmitsNoMetaKey(t *testing.T) {
	s := NewServer("yasaku", "test")
	s.AddUIResource(UIResource{URI: "ui://yasaku/app", Name: "yasaku", Body: "<html></html>"})

	res, err := connect(t, s).ListResources(t.Context(), &sdk.ListResourcesParams{})
	if err != nil {
		t.Fatalf("list resources: %v", err)
	}
	if res.Resources[0].Meta != nil {
		t.Errorf("the fork default must publish no _meta, got %v", res.Resources[0].Meta)
	}
}
