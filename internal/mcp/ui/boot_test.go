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
	// The bundle exports no dispatch seam; the test installs one after evaluation.
	if _, err := vm.RunString(`
		globalThis.__dispatchAction = dispatchAction;
		globalThis.__toolInput = function (p, tool) { captured = { tool: tool || "", args: (p && p.arguments) || {} }; };
	`); err != nil {
		t.Fatalf("install seam: %v", err)
	}
	return vm
}

func TestBootMergesToolInputAndFormOverDeclaredArgs(t *testing.T) {
	vm := bootVM(t, true)
	if _, err := vm.RunString(`
		__toolInput({ arguments: { target: { org: "acme" }, period: "per_9", endDate: "2026-09-30" } }, "close_period");
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
		__toolInput({ arguments: {} }, "record_expense");
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
		__toolInput({ arguments: {} }, "get_wallet");
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
		__toolInput({ arguments: {} }, "get_wallet");
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

func TestBootExpandsDottedFieldNamesIntoNestedArgs(t *testing.T) {
	vm := bootVM(t, true)
	if _, err := vm.RunString(`
		__toolInput({ arguments: {} }, "create_wallet");
		current = { actions: { edit: { tool: "create_wallet", args: { confirm: false } } } };
		const sel = __mkEl({ name: "target.org" }, [], "select");
		sel.value = "acme";
		const form = __mkEl({}, [sel], "form");
		__dispatchAction(__mkEl({ "data-action": "edit" }), form);
	`); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	got := jsString(t, vm, `JSON.stringify(recorded.calls[0].arguments.target)`)
	if got != `{"org":"acme"}` {
		t.Errorf("target = %s, want {\"org\":\"acme\"} — a flat \"target.org\" key is dropped by DiscardUnknown and the need returns forever", got)
	}
	if got := jsString(t, vm, `String("target.org" in recorded.calls[0].arguments)`); got != "false" {
		t.Error("the flat dotted key must not also be sent")
	}
}

func TestBootRemembersEarlierNeedsAnswers(t *testing.T) {
	vm := bootVM(t, true)
	// Two rounds, in separate RunStrings so the first settles — the in-flight lock
	// deliberately refuses a second dispatch while one is pending.
	if _, err := vm.RunString(`
		__toolInput({ arguments: { target: { org: "acme" } } }, "create_wallet");
		current = { actions: { edit: { tool: "create_wallet", args: { confirm: false } } } };
		const n = __mkEl({ name: "name" }, [], "input"); n.value = "Dompet";
		__dispatchAction(__mkEl({ "data-action": "edit" }), __mkEl({}, [n], "form"));
	`); err != nil {
		t.Fatalf("round 1: %v", err)
	}
	if _, err := vm.RunString(`
		current = { actions: { edit: { tool: "create_wallet", args: { confirm: false } } } };
		const k = __mkEl({ name: "kind" }, [], "select"); k.value = "cash";
		__dispatchAction(__mkEl({ "data-action": "edit" }), __mkEl({}, [k], "form"));
	`); err != nil {
		t.Fatalf("round 2: %v", err)
	}
	got := jsString(t, vm, `JSON.stringify(recorded.calls[1].arguments)`)
	if !strings.Contains(got, `"name":"Dompet"`) || !strings.Contains(got, `"kind":"cash"`) {
		t.Errorf("second round = %s; the server resolves needs one at a time, so earlier answers must persist or the form loops forever", got)
	}
}

func TestBootIgnoresToolInputFromADifferentTool(t *testing.T) {
	vm := bootVM(t, true)
	if _, err := vm.RunString(`
		__toolInput({ arguments: { wallet: "from-another-tool", note: "stale" } }, "list_recent_tx");
		current = { actions: { commit: { tool: "delete_tx", args: { confirm: true } } } };
		__dispatchAction(__mkEl({ "data-action": "commit" }), null);
	`); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	got := jsString(t, vm, `JSON.stringify(recorded.calls[0].arguments)`)
	if strings.Contains(got, "stale") {
		t.Errorf("args = %s; another tool's captured input must not leak into a write", got)
	}
}

func TestBootDoesNotFireOnAStrayClickInsideAForm(t *testing.T) {
	vm := bootVM(t, true)
	if _, err := vm.RunString(`
		__toolInput({ arguments: {} }, "create_wallet");
		const out = renderTool("create_wallet", {needs:{needs:[{field:"kind",candidates:["cash"]}]}});
		current = out;
		__root.innerHTML = out.html;
	`); err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(jsString(t, vm, `__root.innerHTML`), `<form class="ya-card" data-action=`) {
		t.Error("data-action on the <form> makes any click inside it dispatch the tool call before the user has chosen")
	}
}
