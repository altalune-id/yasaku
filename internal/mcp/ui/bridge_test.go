package ui

import (
	"strings"
	"testing"
)

func TestBridgeDeclaresInlineDisplayModeAndForwardsCalls(t *testing.T) {
	vm := newJSVM(t)

	// A fake ext-apps that records exactly what bridge.js hands the App class.
	if _, err := vm.RunString(`
		globalThis.recorded = {};
		globalThis.fakeExtApps = function () {
			return {
				App: function (appInfo, capabilities, options) {
					recorded.appInfo = appInfo;
					recorded.capabilities = capabilities;
					recorded.options = options;
					this.connect = function () { return Promise.resolve(); };
					this.getHostContext = function () { return { theme: null }; };
					this.callServerTool = function (req) { recorded.call = req; return Promise.resolve({}); };
				},
				applyDocumentTheme: function () {},
				applyHostStyleVariables: function () {},
				applyHostFonts: function () {},
			};
		};
	`); err != nil {
		t.Fatalf("install fake: %v", err)
	}
	if _, err := vm.RunString(mustRead(t, "src/bridge.js")); err != nil {
		t.Fatalf("eval bridge.js: %v", err)
	}
	// connect() is what constructs App, so the test must await it before calling
	// a tool. goja drains the microtask queue at the end of RunString, so the Go
	// assertions below observe the settled result.
	if _, err := vm.RunString(`
		const b = createBridge({ onToolInput(){}, onToolResult(){}, onHostContext(){} }, fakeExtApps);
		b.connect().then(function () { b.callTool("period_report", { period: "per_1" }); });
	`); err != nil {
		t.Fatalf("createBridge: %v", err)
	}

	modes := jsString(t, vm, `JSON.stringify(recorded.capabilities.availableDisplayModes)`)
	if modes != `["inline"]` {
		t.Errorf(`availableDisplayModes = %s, want ["inline"] — it MUST be the SECOND positional argument to App`, modes)
	}
	if got := jsString(t, vm, `String(recorded.options)`); got != "undefined" {
		t.Errorf("options = %s, want undefined so autoResize stays on and size-changed is sent for free", got)
	}
	if got := jsString(t, vm, `JSON.stringify(recorded.call)`); got != `{"name":"period_report","arguments":{"period":"per_1"}}` {
		t.Errorf("callServerTool received %s", got)
	}
}

func TestVendorExportsEveryNameTheBridgeUses(t *testing.T) {
	src := mustRead(t, vendorPart)
	for _, name := range []string{"App", "applyDocumentTheme", "applyHostStyleVariables", "applyHostFonts"} {
		if !strings.Contains(src, " as "+name+",") && !strings.Contains(src, " as "+name+"}") {
			t.Errorf("ext-apps no longer exports %q — the bridge will throw at runtime", name)
		}
	}
}
