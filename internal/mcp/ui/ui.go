// Package ui assembles and serves yasaku's MCP Apps bundle.
package ui

import (
	"embed"
	"regexp"
	"strings"
	"sync"
	"text/template"
)

// ResourceURI is the ui:// URI the bundle is published at; it must match buf.gen.yaml's ui_prefix plus the proto's ui name.
const ResourceURI = "ui://yasaku/app"

//go:embed shell.html app.css assets/ext-apps-2.0.0.js
//go:embed src/html.js src/format.js src/phase.js src/charts.js src/registry.js src/views/report.js src/views/tx.js src/views/wallets.js src/views/lists.js src/views/mutation.js src/views/bulk.js src/bridge.js src/boot.js
var files embed.FS

const vendorPart = "assets/ext-apps-2.0.0.js"

// NOTE: registry.js must precede every views/ entry — const VIEWS is in the TDZ until it runs, and views call registerView at load. Pinned by TestRegistryLoadsBeforeAnyView.
//
//nolint:gochecknoglobals // an ordered embed manifest has to be package level.
var scriptParts = []string{
	"src/html.js",
	"src/format.js",
	"src/phase.js",
	"src/charts.js",
	"src/registry.js",
	"src/views/report.js",
	"src/views/tx.js",
	"src/views/wallets.js",
	"src/views/lists.js",
	"src/views/mutation.js",
	"src/views/bulk.js",
	"src/bridge.js",
	"src/boot.js",
}

//nolint:gochecknoglobals // sync.OnceValue memoises the assembled document.
var document = sync.OnceValue(build)

// Document returns the assembled single-file HTML bundle.
func Document() string { return document() }

func build() string {
	// NOTE: text/template, never html/template — the latter escapes inside
	// <script> and <style> and would mangle the embedded JS and CSS.
	tmpl := template.Must(template.New("shell").Parse(read("shell.html")))

	var scripts strings.Builder
	for _, p := range scriptParts {
		scripts.WriteString(read(p))
		scripts.WriteString("\n")
	}

	var out strings.Builder
	if err := tmpl.Execute(&out, struct{ Style, Vendor, Scripts string }{
		Style:   read("app.css"),
		Vendor:  exposeExports(read(vendorPart)),
		Scripts: scripts.String(),
	}); err != nil {
		panic("ui: assembling the bundle: " + err.Error())
	}
	return out.String()
}

//nolint:gochecknoglobals // compiled once; a package-level regexp is the idiom.
var exportStmt = regexp.MustCompile(`export\{([^}]*)\};?\s*$`)

// exposeExports appends an assignment binding the bundle's exported names onto globalThis, since an inlined module's exports are unreachable.
func exposeExports(src string) string {
	m := exportStmt.FindStringSubmatch(src)
	if m == nil {
		panic("ui: vendored ext-apps has no trailing export statement")
	}
	pairs := make([]string, 0, 64)
	for _, part := range strings.Split(m[1], ",") {
		local, external, found := strings.Cut(part, " as ")
		if !found {
			external = part
		}
		pairs = append(pairs, strings.TrimSpace(external)+":"+strings.TrimSpace(local))
	}
	return src + "\nglobalThis.__extApps={" + strings.Join(pairs, ",") + "};\n"
}

func read(name string) string {
	b, err := files.ReadFile(name)
	if err != nil {
		panic("ui: embedded file missing: " + name)
	}
	return string(b)
}
