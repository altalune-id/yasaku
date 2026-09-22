// Package gen turns yasaku's MCP-annotated RPCs into mcp.Registry bindings over their Connect handlers.
package gen

import (
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/pluginpb"

	yasakumcpv1 "altalune.id/yasaku/gen/go/yasaku/mcp/v1"
)

const (
	contextPackage   = protogen.GoImportPath("context")
	jsonPackage      = protogen.GoImportPath("encoding/json")
	connectPackage   = protogen.GoImportPath("connectrpc.com/connect")
	protojsonPackage = protogen.GoImportPath("google.golang.org/protobuf/encoding/protojson")
	runtimePackage   = protogen.GoImportPath("altalune.id/yasaku/mcp")
)

type tool struct {
	name        string
	description string
	scope       string
	mutation    bool
	destructive bool
	method      *protogen.Method
	schema      json.RawMessage
	uiResource  string
}

// Generate writes one MCP binding file per service that carries at least one annotated method.
func Generate(p *protogen.Plugin, opts Options) error {
	p.SupportedFeatures = uint64(pluginpb.CodeGeneratorResponse_FEATURE_PROTO3_OPTIONAL)

	taken := map[string]string{}
	for _, file := range p.Files {
		if !file.Generate {
			continue
		}
		for _, svc := range file.Services {
			tools, err := serviceTools(svc, taken, opts)
			if err != nil {
				return err
			}
			if len(tools) == 0 {
				continue
			}
			writeService(p, file, svc, tools)
		}
	}
	return nil
}

func serviceTools(svc *protogen.Service, taken map[string]string, opts Options) ([]tool, error) {
	var tools []tool
	for _, method := range svc.Methods {
		spec, annotated := toolAnnotation(method)
		if !annotated {
			continue
		}
		built, err := newTool(method, spec, opts)
		if err != nil {
			return nil, err
		}
		if owner, clash := taken[built.name]; clash {
			return nil, fmt.Errorf("%s: tool name %q is already used by %s", method.Desc.FullName(), built.name, owner)
		}
		taken[built.name] = string(method.Desc.FullName())
		tools = append(tools, built)
	}
	return tools, nil
}

func toolAnnotation(method *protogen.Method) (*yasakumcpv1.Tool, bool) {
	opts := method.Desc.Options()
	if opts == nil || !proto.HasExtension(opts, yasakumcpv1.E_Tool) {
		return nil, false
	}
	spec, ok := proto.GetExtension(opts, yasakumcpv1.E_Tool).(*yasakumcpv1.Tool)
	if !ok || spec == nil {
		return nil, false
	}
	return spec, true
}

//nolint:gochecknoglobals // compiled once; a package-level regexp is the idiom.
var uiNameRe = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

func uiResourceURI(ui, prefix, method string) (string, error) {
	if ui == "" {
		return "", nil
	}
	if !uiNameRe.MatchString(ui) {
		return "", fmt.Errorf("%s: ui %q must match %s", method, ui, uiNameRe)
	}
	if prefix == "" {
		return "", fmt.Errorf("%s: ui %q set but ui_prefix plugin parameter is absent", method, ui)
	}
	return prefix + "/" + ui, nil
}

func newTool(method *protogen.Method, spec *yasakumcpv1.Tool, opts Options) (tool, error) {
	full := method.Desc.FullName()
	if method.Desc.IsStreamingClient() || method.Desc.IsStreamingServer() {
		return tool{}, fmt.Errorf("%s: an MCP tool must be a unary RPC", full)
	}
	uiURI, err := uiResourceURI(spec.GetUi(), opts.UIPrefix, string(full))
	if err != nil {
		return tool{}, err
	}

	name := spec.GetName()
	if name == "" {
		name = snakeCase(method.GoName)
	}
	if !toolNamePattern.MatchString(name) {
		return tool{}, fmt.Errorf("%s: tool name %q must match %s", full, name, toolNamePattern.String())
	}

	var scope string
	switch spec.GetAccess() {
	case yasakumcpv1.Access_ACCESS_READ:
		scope = "ScopeRead"
	case yasakumcpv1.Access_ACCESS_WRITE:
		scope = "ScopeWrite"
	default:
		return tool{}, fmt.Errorf("%s: tool %q must set access to ACCESS_READ or ACCESS_WRITE", full, name)
	}

	if spec.GetMutation() && !hasConfirmField(method.Input) {
		return tool{}, fmt.Errorf("%s: tool %q is a mutation, so %s must carry a bool confirm field",
			full, name, method.Input.Desc.FullName())
	}

	description := spec.GetDescription()
	if description == "" {
		description = comment(method.Comments.Leading)
	}

	schema, err := inputSchema(method.Input)
	if err != nil {
		return tool{}, fmt.Errorf("%s: building the input schema: %w", full, err)
	}

	return tool{
		name:        name,
		description: description,
		scope:       scope,
		mutation:    spec.GetMutation(),
		destructive: spec.GetDestructive(),
		method:      method,
		schema:      schema,
		uiResource:  uiURI,
	}, nil
}

func hasConfirmField(msg *protogen.Message) bool {
	for _, field := range msg.Fields {
		desc := field.Desc
		if desc.Name() == "confirm" && desc.Kind() == protoreflect.BoolKind && !desc.IsList() && !desc.IsMap() {
			return true
		}
	}
	return false
}

func writeService(p *protogen.Plugin, file *protogen.File, svc *protogen.Service, tools []tool) {
	pkg := string(file.GoPackageName) + "mcp"
	importPath := protogen.GoImportPath(string(file.GoImportPath) + "/" + pkg)
	filename := path.Join(path.Dir(file.GeneratedFilenamePrefix), pkg, snakeCase(svc.GoName)+".mcp.go")
	// NOTE: mirrors how protoc-gen-connect-go derives its own output package, so the two move in
	// lockstep under managed mode; a non-default `package_suffix=` opt would break the pairing.
	connect := protogen.GoImportPath(string(file.GoImportPath) + "/" + string(file.GoPackageName) + "connect")

	g := p.NewGeneratedFile(filename, importPath)
	g.P("// Code generated by protoc-gen-yasaku-mcp. DO NOT EDIT.")
	g.P("//")
	g.P("// Source: ", file.Desc.Path())
	g.P()
	g.P("package ", pkg)
	g.P()

	rawMessage := g.QualifiedGoIdent(jsonPackage.Ident("RawMessage"))
	g.P("// Register", svc.GoName, "Tools registers every MCP-annotated ", svc.GoName, " method on reg.")
	g.P("func Register", svc.GoName, "Tools(reg ", g.QualifiedGoIdent(runtimePackage.Ident("Registry")),
		", h ", g.QualifiedGoIdent(connect.Ident(svc.GoName+"Handler")), ") {")
	for _, t := range tools {
		g.P("reg.Register(", g.QualifiedGoIdent(runtimePackage.Ident("ToolSpec")), "{")
		g.P("Name: ", strconv.Quote(t.name), ",")
		g.P("Description: ", strconv.Quote(t.description), ",")
		g.P("Scope: ", g.QualifiedGoIdent(runtimePackage.Ident(t.scope)), ",")
		g.P("Mutation: ", t.mutation, ",")
		if t.destructive {
			g.P("Destructive: true,")
		}
		g.P("InputSchema: ", rawMessage, "(", goStringLiteral(string(t.schema)), "),")
		if t.uiResource != "" {
			g.P("UIResourceURI: ", strconv.Quote(t.uiResource), ",")
		}
		g.P("}, func(ctx ", g.QualifiedGoIdent(contextPackage.Ident("Context")), ", input ", rawMessage,
			") (", rawMessage, ", error) {")
		g.P("if len(input) == 0 {")
		g.P("input = ", rawMessage, "(`{}`)")
		g.P("}")
		g.P("var req ", g.QualifiedGoIdent(t.method.Input.GoIdent))
		g.P("if err := (", g.QualifiedGoIdent(protojsonPackage.Ident("UnmarshalOptions")),
			"{DiscardUnknown: true}).Unmarshal(input, &req); err != nil {")
		g.P("return nil, err")
		g.P("}")
		g.P("resp, err := h.", t.method.GoName, "(ctx, ",
			g.QualifiedGoIdent(connectPackage.Ident("NewRequest")), "(&req))")
		g.P("if err != nil {")
		g.P("return nil, err")
		g.P("}")
		g.P("return ", g.QualifiedGoIdent(protojsonPackage.Ident("Marshal")), "(resp.Msg)")
		g.P("})")
	}
	g.P("}")
}

func goStringLiteral(s string) string {
	if strings.Contains(s, "`") || strings.Contains(s, "\n") {
		return strconv.Quote(s)
	}
	return "`" + s + "`"
}
