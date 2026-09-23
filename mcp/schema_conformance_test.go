package mcp

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const extAppsSchemaPath = "testdata/ext-apps-schema-2.0.0.json"

func extAppsSchema(t *testing.T, def string) *jsonschema.Resolved {
	t.Helper()
	bundleRaw, err := os.ReadFile(extAppsSchemaPath) //nolint:gosec // fixed test path
	if err != nil {
		t.Fatalf("read vendored schema: %v", err)
	}
	var bundle struct {
		Defs map[string]json.RawMessage `json:"$defs"`
	}
	if err := json.Unmarshal(bundleRaw, &bundle); err != nil {
		t.Fatalf("unmarshal bundle: %v", err)
	}
	raw, ok := bundle.Defs[def]
	if !ok {
		t.Fatalf("$defs/%s missing — ext-apps moved it; re-pin against apps.mdx", def)
	}
	var s jsonschema.Schema
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("unmarshal %s: %v", def, err)
	}
	rs, err := s.Resolve(nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	return rs
}

func uiMetaTool(t *testing.T) *sdk.Tool {
	t.Helper()
	s := NewServer("yasaku", "test", WithUI(true))
	s.AddUIResource(UIResource{URI: "ui://yasaku/app", Body: "<html></html>"})
	s.Register(uiSpec(), echoHandler)
	return toolByName(t, connect(t, s), "wallet_list")
}

func roundTrip(t *testing.T, v any) any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return out
}

func TestEmittedUIMetaMatchesOfficialSchema(t *testing.T) {
	tool := uiMetaTool(t)
	if err := extAppsSchema(t, "McpUiToolMeta").Validate(roundTrip(t, tool.Meta[metaKeyUI])); err != nil {
		t.Errorf("emitted _meta[%q] violates McpUiToolMeta: %v", metaKeyUI, err)
	}
}

// TestWholeMetaAlsoValidates guards the reason the deprecated flat sibling key is
// not emitted: McpUiToolMeta sets additionalProperties:false, so a host validating
// the whole _meta object must still accept what we send.
func TestWholeMetaAlsoValidates(t *testing.T) {
	tool := uiMetaTool(t)
	whole := roundTrip(t, map[string]any(tool.Meta))
	inner, ok := whole.(map[string]any)["ui"]
	if !ok {
		t.Fatalf("_meta has no ui key: %v", whole)
	}
	if err := extAppsSchema(t, "McpUiToolMeta").Validate(inner); err != nil {
		t.Errorf("_meta.ui violates McpUiToolMeta: %v", err)
	}
	if len(whole.(map[string]any)) != 1 {
		t.Errorf("_meta carries %d keys; a sibling of ui breaks strict hosts", len(whole.(map[string]any)))
	}
}

func uiMetaResource(t *testing.T) *sdk.Resource {
	t.Helper()
	s := NewServer("yasaku", "test", WithUI(true))
	s.AddUIResource(UIResource{
		URI:           "ui://yasaku/app",
		Name:          "yasaku",
		Body:          "<html></html>",
		PrefersBorder: new(bool),
	})
	res, err := connect(t, s).ListResources(t.Context(), &sdk.ListResourcesParams{})
	if err != nil {
		t.Fatalf("list resources: %v", err)
	}
	if len(res.Resources) != 1 {
		t.Fatalf("resources = %d, want 1", len(res.Resources))
	}
	return res.Resources[0]
}

func TestEmittedResourceMetaMatchesOfficialSchema(t *testing.T) {
	whole, ok := roundTrip(t, map[string]any(uiMetaResource(t).Meta)).(map[string]any)
	if !ok {
		t.Fatalf("resource _meta is not an object")
	}
	inner, ok := whole[metaKeyUI]
	if !ok {
		t.Fatalf("resource _meta has no %q key: %v", metaKeyUI, whole)
	}
	if err := extAppsSchema(t, "McpUiResourceMeta").Validate(inner); err != nil {
		t.Errorf("_meta.%s violates McpUiResourceMeta: %v", metaKeyUI, err)
	}
	if len(whole) != 1 {
		t.Errorf("resource _meta carries %d keys; a sibling of %q breaks strict hosts", len(whole), metaKeyUI)
	}
}
