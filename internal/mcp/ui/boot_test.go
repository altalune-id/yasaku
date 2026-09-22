package ui

import (
	"strings"
	"testing"

	"github.com/dop251/goja"
)

// bootVM mounts boot.js over the DOM stub and a fake ext-apps that records every
// callServerTool and lets the test drive onToolInput and the call's outcome.
func bootVM(t *testing.T, resolve bool) *goja.Runtime {
	t.Helper()
	vm := newJSVM(t)
	stub, err := fixtures.ReadFile("testdata/dom_stub.js")
	if err != nil {
		t.Fatalf("read dom stub: %v", err)
	}
	if _, err := vm.RunString(string(stub)); err != nil {
		t.Fatalf("eval dom stub: %v", err)
	}
	settle := "resolve({structuredContent:{result:{id:'written'}}})"
	if !resolve {
		settle = "reject(new Error('denied'))"
	}
	if _, err := vm.RunString(`
		globalThis.recorded = {calls: []};
		globalThis.__extApps = null;
		globalThis.fakeLoader = function () {
			return {
				App: function (info, caps) {
					globalThis.recorded.caps = caps;
					globalThis.__app = this;
					this.connect = function () { return Promise.resolve(); };
					this.getHostContext = function () { return { toolInfo: { tool: { name: "list_wallets" } } }; };
					this.callServerTool = function (req) {
						globalThis.recorded.calls.push(req);
						return new Promise(function (resolve, reject) { ` + settle + `; });
					};
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
	src := strings.Replace(mustRead(t, "src/boot.js"),
		"function () { return globalThis.__extApps; }", "globalThis.fakeLoader", 1)
	if _, err := vm.RunString(src); err != nil {
		t.Fatalf("eval boot.js: %v", err)
	}
	return vm
}

func TestBootMergesToolInputAndFormOverDeclaredArgs(t *testing.T) {
	vm := bootVM(t, true)
	if _, err := vm.RunString(`
		__toolInput({ arguments: { target: { org: "acme" }, period: "per_9", endDate: "2026-09-30" } });
		current = { actions: { commit: { tool: "close_period", args: { confirm: true } } } };
		const form = { children: [ { attrs: { name: "endDate" }, value: "2026-09-29", getAttribute(k){ return this.attrs[k] || null; } } ],
		               querySelectorAll(){ return this.children; } };
		const btn = __mkEl({ "data-action": "commit" });
		__dispatchAction(btn, form);
	`); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	got := jsString(t, vm, `JSON.stringify(recorded.calls[0])`)
	const want = `{"name":"close_period","arguments":{"target":{"org":"acme"},"period":"per_9","endDate":"2026-09-29","confirm":true}}`
	if got != want {
		t.Errorf("callServerTool got\n %s\nwant\n %s", got, want)
	}
}

func TestBootCommitCannotFireTwice(t *testing.T) {
	vm := bootVM(t, true)
	if _, err := vm.RunString(`
		__toolInput({ arguments: {} });
		current = { actions: { commit: { tool: "record_expense", args: { confirm: true } } } };
		const btn = __mkEl({ "data-action": "commit" });
		__dispatchAction(btn, null);
		__dispatchAction(btn, null);
	`); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if got := jsString(t, vm, `String(recorded.calls.length)`); got != "1" {
		t.Errorf("callServerTool fired %s times, want 1 — a second click must not write again", got)
	}
}

func TestBootPaintsFromTheCallToolPromise(t *testing.T) {
	vm := bootVM(t, true)
	if _, err := vm.RunString(`
		__toolInput({ arguments: {} });
		current = { actions: { go: { tool: "get_wallet", args: {} } } };
		__dispatchAction(__mkEl({ "data-action": "go" }), null);
	`); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	// get_wallet's renderer must have run against the promise result, not against
	// hostContext, which the fake pins to list_wallets.
	if got := jsString(t, vm, `__root.innerHTML`); !strings.Contains(got, "Wallet") {
		t.Errorf("panel did not repaint from the callTool promise:\n%s", got)
	}
}

func TestBootShowsARejectedState(t *testing.T) {
	vm := bootVM(t, false)
	if _, err := vm.RunString(`
		__toolInput({ arguments: {} });
		current = { actions: { go: { tool: "get_wallet", args: {} } } };
		__dispatchAction(__mkEl({ "data-action": "go" }), null);
	`); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	got := jsString(t, vm, `__root.innerHTML`)
	if !strings.Contains(got, "ya-error") {
		t.Errorf("a denied or failed call must show a visible state, got:\n%s", got)
	}
}
