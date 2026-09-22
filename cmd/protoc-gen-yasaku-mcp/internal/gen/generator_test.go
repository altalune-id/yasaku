package gen

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"

	_ "altalune.id/yasaku/gen/go/yasaku/mcp/v1"
)

//nolint:gochecknoglobals // a -update flag for golden files has to be package level.
var update = flag.Bool("update", false, "rewrite the golden files instead of comparing against them")

func TestGenerateWritesOneFilePerAnnotatedService(t *testing.T) {
	p := newPlugin(t, "yasakutest/v1/fixture.proto")
	if err := Generate(p, Options{UIPrefix: "ui://yasakutest"}); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	resp := p.Response()
	if resp.Error != nil {
		t.Fatalf("response carries error %q", resp.GetError())
	}
	if len(resp.File) != 1 {
		t.Fatalf("got %d generated files, want 1", len(resp.File))
	}
	if got, want := resp.File[0].GetName(), "yasakutest/v1/yasakutestv1mcp/fixture_service.mcp.go"; got != want {
		t.Fatalf("generated file name = %q, want %q", got, want)
	}
	checkGolden(t, "fixture.mcp.go.golden", resp.File[0].GetContent())
}

func TestGenerateSkipsServicesWithoutAnnotatedMethods(t *testing.T) {
	p := newPlugin(t, "yasakutest/v1/unannotated.proto")
	if err := Generate(p, Options{UIPrefix: "ui://yasakutest"}); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if got := len(p.Response().File); got != 0 {
		t.Fatalf("got %d generated files, want 0", got)
	}
}

func TestInputSchemas(t *testing.T) {
	p := newPlugin(t, "yasakutest/v1/fixture.proto")

	schemas := map[string]json.RawMessage{}
	for _, file := range p.Files {
		if !file.Generate {
			continue
		}
		for _, svc := range file.Services {
			tools, err := serviceTools(svc, map[string]string{}, Options{UIPrefix: "ui://yasakutest"})
			if err != nil {
				t.Fatalf("serviceTools(%s) error = %v", svc.Desc.FullName(), err)
			}
			for _, tl := range tools {
				schemas[tl.name] = tl.schema
			}
		}
	}

	got, err := json.MarshalIndent(schemas, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent() error = %v", err)
	}
	checkGolden(t, "schemas.json.golden", string(got)+"\n")
}

func TestGenerateRefusals(t *testing.T) {
	tests := []struct {
		name  string
		file  string
		wants []string
	}{
		{
			name:  "mutation without a bool confirm field",
			file:  "yasakutest/v1/bad_confirm.proto",
			wants: []string{"MutateWithoutConfirm", "confirm"},
		},
		{
			name:  "tool name that fails the regex",
			file:  "yasakutest/v1/bad_name.proto",
			wants: []string{"ShoutLoudly", "Shout-Loudly"},
		},
		{
			name:  "duplicate tool name",
			file:  "yasakutest/v1/bad_dup.proto",
			wants: []string{"SecondTwin", "twin_tool"},
		},
		{
			name:  "unspecified access",
			file:  "yasakutest/v1/bad_access.proto",
			wants: []string{"NoAccess", "access"},
		},
		{
			name:  "streaming rpc",
			file:  "yasakutest/v1/bad_stream.proto",
			wants: []string{"WatchForever", "unary"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newPlugin(t, tt.file)
			err := Generate(p, Options{UIPrefix: "ui://yasakutest"})
			if err == nil {
				t.Fatalf("Generate() error = nil, want a refusal")
			}
			for _, want := range tt.wants {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not mention %q", err, want)
				}
			}
		})
	}
}

func TestDuplicateToolNamesAcrossFiles(t *testing.T) {
	p := newPlugin(t, "yasakutest/v1/fixture.proto", "yasakutest/v1/bad_echo.proto")
	err := Generate(p, Options{UIPrefix: "ui://yasakutest"})
	if err == nil {
		t.Fatal("Generate() error = nil, want a refusal on the cross-file duplicate")
	}
	if !strings.Contains(err.Error(), "poke_target") {
		t.Errorf("error %q does not mention the duplicated tool name", err)
	}
}

func TestSnakeCase(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Ping", "ping"},
		{"AdjustBalance", "adjust_balance"},
		{"WalletService", "wallet_service"},
		{"ListV2Items", "list_v2_items"},
		{"HTTPServer", "http_server"},
		{"SeedDefaultCategories", "seed_default_categories"},
	}
	for _, tt := range tests {
		if got := snakeCase(tt.in); got != tt.want {
			t.Errorf("snakeCase(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestToolNamePattern(t *testing.T) {
	valid := []string{"ping", "_ping", "list_wallets", "a", "wallet2"}
	invalid := []string{"", "Ping", "2ping", "list-wallets", "list wallets", strings.Repeat("a", 65)}
	for _, s := range valid {
		if !toolNamePattern.MatchString(s) {
			t.Errorf("toolNamePattern rejected valid name %q", s)
		}
	}
	for _, s := range invalid {
		if toolNamePattern.MatchString(s) {
			t.Errorf("toolNamePattern accepted invalid name %q", s)
		}
	}
}

func TestFixtureDescriptorSetIsFresh(t *testing.T) {
	buf := bufCommand(t)

	out := filepath.Join(t.TempDir(), "fixture.binpb")
	cmd := exec.CommandContext(t.Context(), buf[0], append(append([]string{}, buf[1:]...), "build", filepath.Join("testdata", "proto"), "-o", out)...)
	if combined, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("rebuilding the fixture descriptor set: %v\n%s", err, combined)
	}

	rebuilt, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("reading the rebuilt descriptor set: %v", err)
	}
	committed, err := os.ReadFile(filepath.Join("testdata", "fixture.binpb"))
	if err != nil {
		t.Fatalf("reading the committed descriptor set: %v", err)
	}
	if !bytes.Equal(rebuilt, committed) {
		t.Errorf("testdata/fixture.binpb is stale: it no longer matches testdata/proto. Run `make gen-plugin-fixture`, then `go test ./cmd/protoc-gen-yasaku-mcp/... -update`.")
	}
}

func bufCommand(t *testing.T) []string {
	t.Helper()

	if path, err := exec.LookPath("buf"); err == nil {
		return []string{path}
	}
	if path, err := exec.LookPath("pnpm"); err == nil {
		return []string{path, "exec", "buf"}
	}
	t.Skip("neither buf nor pnpm is on PATH — cannot verify that testdata/fixture.binpb is fresh")
	return nil
}

func newPlugin(t *testing.T, generate ...string) *protogen.Plugin {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("testdata", "fixture.binpb"))
	if err != nil {
		t.Fatalf("reading the fixture descriptor set: %v", err)
	}
	set := &descriptorpb.FileDescriptorSet{}
	if err := proto.Unmarshal(raw, set); err != nil {
		t.Fatalf("unmarshalling the fixture descriptor set: %v", err)
	}

	p, err := protogen.Options{}.New(&pluginpb.CodeGeneratorRequest{
		FileToGenerate: generate,
		Parameter:      proto.String("paths=source_relative"),
		ProtoFile:      set.File,
	})
	if err != nil {
		t.Fatalf("building the plugin: %v", err)
	}
	return p
}

func checkGolden(t *testing.T, name, got string) {
	t.Helper()

	path := filepath.Join("testdata", "golden", name)
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("writing golden %s: %v", path, err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading golden %s (run `go test ./... -update` to create it): %v", path, err)
	}
	if !bytes.Equal([]byte(got), want) {
		t.Errorf("generated output differs from %s\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}

func TestGenerateRejectsBadUIName(t *testing.T) {
	p := newPlugin(t, "yasakutest/v1/bad_ui.proto")
	err := Generate(p, Options{UIPrefix: "ui://yasakutest"})
	if err == nil {
		t.Fatal(`Generate() = nil, want error for ui name "App_1"`)
	}
	if !strings.Contains(err.Error(), "App_1") {
		t.Errorf("error %q must name the offending value", err)
	}
}

func TestGenerateRequiresUIPrefixWhenUISet(t *testing.T) {
	p := newPlugin(t, "yasakutest/v1/fixture.proto")
	err := Generate(p, Options{})
	if err == nil {
		t.Fatal("Generate() = nil, want error when ui is set but ui_prefix is absent")
	}
	if !strings.Contains(err.Error(), "ui_prefix") {
		t.Errorf("error %q must name ui_prefix", err)
	}
}
