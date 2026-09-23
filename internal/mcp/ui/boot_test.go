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
	settle := "resolve({structuredContent:{result:{id:'written'}}})"
	if !resolve {
		settle = "reject(new Error('denied'))"
	}
	return bootVMSettling(t, settle)
}

// bootVMSettling is bootVM with the callServerTool promise settled by the given JS statement.
func bootVMSettling(t *testing.T, settle string) *goja.Runtime {
	t.Helper()
	vm := newJSVM(t)
	stub, err := fixtures.ReadFile("testdata/dom_stub.js")
	if err != nil {
		t.Fatalf("read dom stub: %v", err)
	}
	if _, err := vm.RunString(string(stub)); err != nil {
		t.Fatalf("eval dom stub: %v", err)
	}
	if _, err := vm.RunString(`
		globalThis.recorded = {calls: [], settlers: []};
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
		globalThis.__toolInput = function (p, tool) { hostHandlers.onToolInput(p, tool); };
		globalThis.__paints = [];
		const __realPaint = paint;
		paint = function (name, data) { globalThis.__paints.push(name); return __realPaint(name, data); };
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

const deferredSettle = `globalThis.recorded.settlers.push({resolve: resolve, reject: reject})`

func TestBootPaintsACancelledStateAndUnlocksTheView(t *testing.T) {
	vm := bootVMSettling(t, deferredSettle)
	if _, err := vm.RunString(`
		__toolInput({ arguments: {} }, "get_wallet");
		current = { actions: { go: { tool: "get_wallet", args: {} } } };
		__dispatchAction(__mkEl({ "data-action": "go" }), null);
		__app.ontoolcancelled({ reason: "<img src=x onerror=alert(1)>" });
	`); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	got := jsString(t, vm, `__root.innerHTML`)
	if strings.Contains(got, "Loading") {
		t.Errorf("the panel is still on Loading after ui/notifications/tool-cancelled:\n%s", got)
	}
	if !strings.Contains(strings.ToLower(got), "cancel") {
		t.Errorf("a cancelled call must paint a cancelled state, got:\n%s", got)
	}
	if strings.Contains(got, "<img src=x") {
		t.Errorf("the host-supplied cancellation reason reached the DOM as markup:\n%s", got)
	}
	if !strings.Contains(got, "&lt;img src=x") {
		t.Errorf("the cancellation reason was not surfaced (escaped), got:\n%s", got)
	}
	if _, err := vm.RunString(`
		current = { actions: { go: { tool: "get_wallet", args: {} } } };
		__dispatchAction(__mkEl({ "data-action": "go" }), null);
	`); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if got := jsString(t, vm, `String(recorded.calls.length)`); got != "2" {
		t.Errorf("calls after retry = %s, want 2 — a cancelled call must release the in-flight lock", got)
	}
}

func TestBootResolvesAHostNotificationToTheToolItActuallyCalled(t *testing.T) {
	t.Run("notification after the callServerTool promise", func(t *testing.T) {
		vm := bootVM(t, true)
		if _, err := vm.RunString(`
			__toolInput({ arguments: {} }, "get_wallet");
			current = { actions: { go: { tool: "get_wallet", args: {} } } };
			__dispatchAction(__mkEl({ "data-action": "go" }), null);
		`); err != nil {
			t.Fatalf("dispatch: %v", err)
		}
		if _, err := vm.RunString(`__app.ontoolresult({ structuredContent: { result: { id: 'written' } } });`); err != nil {
			t.Fatalf("host result: %v", err)
		}
		if got := jsString(t, vm, `JSON.stringify(__paints)`); got != `["get_wallet"]` {
			t.Errorf("paints = %s, want [\"get_wallet\"] — hostContext still names the instantiating tool, so a later host notification must not repaint through list_wallets", got)
		}
	})

	t.Run("notification before the callServerTool promise", func(t *testing.T) {
		vm := bootVMSettling(t, deferredSettle)
		if _, err := vm.RunString(`
			__toolInput({ arguments: {} }, "get_wallet");
			current = { actions: { go: { tool: "get_wallet", args: {} } } };
			__dispatchAction(__mkEl({ "data-action": "go" }), null);
			__app.ontoolresult({ structuredContent: { result: { id: 'written' } } });
			recorded.settlers[0].resolve({ structuredContent: { result: { id: 'written' } } });
		`); err != nil {
			t.Fatalf("dispatch: %v", err)
		}
		if got := jsString(t, vm, `JSON.stringify(__paints)`); got != `["get_wallet"]` {
			t.Errorf("paints = %s, want [\"get_wallet\"] once — the host notification and the promise describe ONE call and must not double-paint", got)
		}
	})

	t.Run("the first host-initiated result still uses host context", func(t *testing.T) {
		vm := bootVM(t, true)
		if _, err := vm.RunString(`__app.ontoolresult({ structuredContent: { wallets: [] } });`); err != nil {
			t.Fatalf("host result: %v", err)
		}
		if got := jsString(t, vm, `JSON.stringify(__paints)`); got != `["list_wallets"]` {
			t.Errorf("paints = %s, want [\"list_wallets\"] — with no view-initiated call the instantiating tool is the only name there is", got)
		}
	})
}

func TestBootCapturesTheCalledToolOnAHostToolInput(t *testing.T) {
	vm := bootVMSettling(t, deferredSettle)
	if _, err := vm.RunString(`
		__toolInput({ arguments: { target: { org: "acme" } } }, "create_wallet");
		current = { actions: { edit: { tool: "create_wallet", args: { confirm: false } } } };
		const n = __mkEl({ name: "name" }, [], "input"); n.value = "Dompet";
		__dispatchAction(__mkEl({ "data-action": "edit" }), __mkEl({}, [n], "form"));
		__app.ontoolinput({ arguments: { target: { org: "acme" }, name: "Dompet" } });
		recorded.settlers[0].resolve({ structuredContent: { needs: { needs: [{ field: "kind", candidates: ["cash"] }] } } });
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
	if !strings.Contains(got, `"name":"Dompet"`) {
		t.Errorf("second round = %s; the host re-sent tool-input for the view-initiated call, and mislabelling it breaks the captured.tool merge guard", got)
	}
}

// hostNamed points the fake host context at name, as bridge.js reads it for every tool-input.
func hostNamed(t *testing.T, vm *goja.Runtime, name string) {
	t.Helper()
	if _, err := vm.RunString(`__app.getHostContext = function () { return { toolInfo: { tool: { name: ` + jsQuote(name) + ` } } }; };`); err != nil {
		t.Fatalf("set host context: %v", err)
	}
}

func commit(t *testing.T, vm *goja.Runtime, tool string) {
	t.Helper()
	if _, err := vm.RunString(`current = { actions: { commit: { tool: ` + jsQuote(tool) + `, args: { confirm: true } } } };
		__dispatchAction(__mkEl({ "data-action": "commit" }), null);`); err != nil {
		t.Fatalf("commit %s: %v", tool, err)
	}
}

func lastArgs(t *testing.T, vm *goja.Runtime) string {
	t.Helper()
	v, err := vm.RunString(`JSON.stringify(recorded.calls[recorded.calls.length - 1].arguments)`)
	if err != nil {
		t.Fatalf("read args: %v", err)
	}
	return v.String()
}

func TestBootNeverMergesAHostToolInputIntoAnotherToolsWrite(t *testing.T) {
	foreign := `__app.ontoolinput({ arguments: { wallet: "from-another-tool", note: "stale" } });`

	t.Run("after the view call settled, host context naming the same tool", func(t *testing.T) {
		vm := bootVM(t, true)
		hostNamed(t, vm, "create_wallet")
		commit(t, vm, "create_wallet")
		if _, err := vm.RunString(foreign); err != nil {
			t.Fatalf("host input: %v", err)
		}
		commit(t, vm, "create_wallet")
		if got := lastArgs(t, vm); strings.Contains(got, "from-another-tool") {
			t.Errorf("another tool's arguments reached a confirm:true write: %s", got)
		}
	})

	t.Run("while the view call is still in flight", func(t *testing.T) {
		vm := bootVMSettling(t, "globalThis.recorded.settlers.push({resolve: resolve, reject: reject})")
		hostNamed(t, vm, "create_wallet")
		commit(t, vm, "create_wallet")
		if _, err := vm.RunString(foreign); err != nil {
			t.Fatalf("host input: %v", err)
		}
		if _, err := vm.RunString(`recorded.settlers[0].resolve({structuredContent:{}});`); err != nil {
			t.Fatalf("settle: %v", err)
		}
		commit(t, vm, "create_wallet")
		if got := lastArgs(t, vm); strings.Contains(got, "from-another-tool") {
			t.Errorf("an in-flight window let another tool's arguments into a confirm:true write: %s", got)
		}
	})
}

func TestBootKeepsEarlierNeedsAnswersAcrossATrailingToolInput(t *testing.T) {
	vm := bootVM(t, true)
	hostNamed(t, vm, "create_wallet")
	if _, err := vm.RunString(`
		current = { actions: { a1: { tool: "create_wallet", args: { name: "Dompet" } } } };
		__dispatchAction(__mkEl({ "data-action": "a1" }), null);
	`); err != nil {
		t.Fatalf("round 1: %v", err)
	}
	if _, err := vm.RunString(`__app.ontoolinput({ arguments: { name: "Dompet" } });`); err != nil {
		t.Fatalf("trailing tool-input: %v", err)
	}
	if _, err := vm.RunString(`
		current = { actions: { a2: { tool: "create_wallet", args: { kind: "cash" } } } };
		__dispatchAction(__mkEl({ "data-action": "a2" }), null);
	`); err != nil {
		t.Fatalf("round 2: %v", err)
	}
	if got := lastArgs(t, vm); !strings.Contains(got, "Dompet") {
		t.Errorf("round 1's answer was lost, so the server re-asks forever: %s", got)
	}
}

func TestBootPaintsAHostResultThatFollowsASettledViewCall(t *testing.T) {
	vm := bootVM(t, true)
	hostNamed(t, vm, "create_wallet")
	commit(t, vm, "create_wallet")
	if _, err := vm.RunString(`
		__app.getHostContext = function () { return { toolInfo: { tool: { name: "list_wallets" } } }; };
		__app.ontoolresult({ structuredContent: { result: {} } });
		__app.ontoolresult({ structuredContent: { wallets: [] } });
	`); err != nil {
		t.Fatalf("host result: %v", err)
	}
	paints, err := vm.RunString(`JSON.stringify(__paints)`)
	if err != nil {
		t.Fatalf("read paints: %v", err)
	}
	if !strings.Contains(paints.String(), "list_wallets") {
		t.Errorf("a host result after a settled call was dropped, leaving the panel stale: %s", paints.String())
	}
}

func TestBootDoesNotRepointAnInFlightCallOnAContextRefresh(t *testing.T) {
	vm := bootVMSettling(t, "globalThis.recorded.settlers.push({resolve: resolve, reject: reject})")
	hostNamed(t, vm, "list_wallets")
	commit(t, vm, "create_wallet")
	if _, err := vm.RunString(`
		hostHandlers.onHostContext({ theme: "dark", toolInfo: { tool: { name: "list_wallets" } } });
		__app.ontoolresult({ structuredContent: { result: {} } });
	`); err != nil {
		t.Fatalf("context refresh: %v", err)
	}
	paints, err := vm.RunString(`JSON.stringify(__paints)`)
	if err != nil {
		t.Fatalf("read paints: %v", err)
	}
	if strings.Contains(paints.String(), "list_wallets") {
		t.Errorf("an unchanged toolInfo repointed an in-flight call: %s", paints.String())
	}
}

func TestBootLateCancellationDoesNotWipeAPaintedResult(t *testing.T) {
	vm := bootVM(t, true)
	if _, err := vm.RunString(`
		current = { actions: { commit: { tool: "create_wallet", args: { confirm: true } } } };
		__dispatchAction(__mkEl({ "data-action": "commit" }), null);
	`); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if _, err := vm.RunString(`hostHandlers.onToolCancelled({ reason: "timeout" });`); err != nil {
		t.Fatalf("cancel after settle: %v", err)
	}
	dom, err := vm.RunString(`document.getElementById("root").innerHTML`)
	if err != nil {
		t.Fatalf("read dom: %v", err)
	}
	if strings.Contains(dom.String(), "cancelled") {
		t.Errorf("a late cancellation wiped an already-painted result:\n%s", dom.String())
	}
}

func TestBootReleasesTheInFlightToolWhenTheHostRepointsTheView(t *testing.T) {
	vm := bootVM(t, true)
	if _, err := vm.RunString(`
		current = { actions: { commit: { tool: "get_wallet", args: {} } } };
		__dispatchAction(__mkEl({ "data-action": "commit" }), null);
		__app.getHostContext = function () { return { toolInfo: { tool: { name: "list_recent_tx" } } }; };
		hostHandlers.onHostContext({ toolInfo: { tool: { name: "list_recent_tx" } } });
		hostHandlers.onToolResult({ structuredContent: { transactions: [] } });
	`); err != nil {
		t.Fatalf("repoint: %v", err)
	}
	paints, err := vm.RunString(`JSON.stringify(__paints)`)
	if err != nil {
		t.Fatalf("read paints: %v", err)
	}
	if !strings.Contains(paints.String(), "list_recent_tx") {
		t.Errorf("a host-repointed view must paint the new tool, got %s", paints.String())
	}
}

func TestBootLocksOutASecondWriteWhileOneIsInFlight(t *testing.T) {
	vm := bootVMSettling(t, "globalThis.recorded.settlers.push({resolve: resolve, reject: reject})")
	hostNamed(t, vm, "list_wallets")
	commit(t, vm, "close_period")
	if _, err := vm.RunString(`
		__app.ontoolcancelled({ reason: "stopped" });
		hostHandlers.onHostContext({ toolInfo: { tool: { name: "adjust_balance" } } });
	`); err != nil {
		t.Fatalf("cancel and repoint: %v", err)
	}
	commit(t, vm, "adjust_balance")
	if _, err := vm.RunString(`recorded.settlers[0].reject(new Error("aborted"));`); err != nil {
		t.Fatalf("stale reject: %v", err)
	}
	commit(t, vm, "adjust_balance")
	n, err := vm.RunString(`recorded.calls.length`)
	if err != nil {
		t.Fatalf("read calls: %v", err)
	}
	if n.ToInteger() != 2 {
		names, _ := vm.RunString(`JSON.stringify(recorded.calls.map(function (c) { return c.name; }))`)
		t.Errorf("a stale call's settle unlocked a newer in-flight write: %s", names.String())
	}
}

func TestBootCancellationSurvivesTheCallSettlingAfterwards(t *testing.T) {
	vm := bootVMSettling(t, "globalThis.recorded.settlers.push({resolve: resolve, reject: reject})")
	commit(t, vm, "create_wallet")
	if _, err := vm.RunString(`__app.ontoolcancelled({ reason: "stopped" });`); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if _, err := vm.RunString(`recorded.settlers[0].resolve({ structuredContent: { result: {} } });`); err != nil {
		t.Fatalf("late resolve: %v", err)
	}
	dom, err := vm.RunString(`document.getElementById("root").innerHTML`)
	if err != nil {
		t.Fatalf("read dom: %v", err)
	}
	if !strings.Contains(dom.String(), "cancelled") {
		t.Errorf("a cancelled call painted its result anyway:\n%s", dom.String())
	}
}

func TestBootCancellationSurvivesTheCallRejectingAfterwards(t *testing.T) {
	vm := bootVMSettling(t, "globalThis.recorded.settlers.push({resolve: resolve, reject: reject})")
	commit(t, vm, "create_wallet")
	if _, err := vm.RunString(`__app.ontoolcancelled({ reason: "stopped" });`); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if _, err := vm.RunString(`recorded.settlers[0].reject(new Error("aborted"));`); err != nil {
		t.Fatalf("late reject: %v", err)
	}
	dom, err := vm.RunString(`document.getElementById("root").innerHTML`)
	if err != nil {
		t.Fatalf("read dom: %v", err)
	}
	if strings.Contains(dom.String(), "not completed") {
		t.Errorf("a cancelled call's rejection replaced the cancellation notice:\n%s", dom.String())
	}
}

func TestBootKeepsTheInFlightCallOnAContextChangeCarryingNoToolInfo(t *testing.T) {
	vm := bootVMSettling(t, "globalThis.recorded.settlers.push({resolve: resolve, reject: reject})")
	hostNamed(t, vm, "list_wallets")
	commit(t, vm, "close_period")
	if _, err := vm.RunString(`
		hostHandlers.onHostContext({ theme: "dark" });
		__app.ontoolresult({ structuredContent: { result: {} } });
	`); err != nil {
		t.Fatalf("theme change: %v", err)
	}
	paints, err := vm.RunString(`JSON.stringify(__paints)`)
	if err != nil {
		t.Fatalf("read paints: %v", err)
	}
	if !strings.Contains(paints.String(), "close_period") {
		t.Errorf("a theme-only context change repointed the in-flight call: %s", paints.String())
	}
}

func TestBootSubjectReadsTheViewsOwnAnswersNotTheHostAnnouncement(t *testing.T) {
	vm := bootVM(t, true)
	hostNamed(t, vm, "close_period")
	if _, err := vm.RunString(`
		__app.ontoolinput({ arguments: { period: "from-the-host" } });
		answers = { tool: "close_period", args: { period: "per_9" } };
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	got, err := vm.RunString(`JSON.stringify(capturedArgs())`)
	if err != nil {
		t.Fatalf("read capturedArgs: %v", err)
	}
	if strings.Contains(got.String(), "from-the-host") {
		t.Errorf("capturedArgs surfaced the host announcement instead of the view's answers: %s", got.String())
	}
}

func TestBootFormFieldCannotOverrideADeclaredConfirm(t *testing.T) {
	vm := bootVM(t, true)
	if _, err := vm.RunString(`
		current = { actions: { preview: { tool: "close_period", args: { confirm: false } } } };
		__dispatchAction(
			__mkEl({ "data-action": "preview" }),
			{
				querySelectorAll: function (sel) {
					if (sel !== "[name]") return [];
					return [__mkEl({ name: "confirm" })].map(function (el) {
						el.value = "true";
						return el;
					});
				},
			}
		);
	`); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if got := lastArgs(t, vm); !strings.Contains(got, `"confirm":false`) {
		t.Errorf("a form field overrode the declared confirm: %s", got)
	}
}

func TestBootStaleResolveDoesNotDisownANewerCall(t *testing.T) {
	vm := bootVMSettling(t, "globalThis.recorded.settlers.push({resolve: resolve, reject: reject})")
	hostNamed(t, vm, "list_wallets")
	commit(t, vm, "close_period")
	if _, err := vm.RunString(`
		__app.ontoolcancelled({ reason: "stopped" });
		hostHandlers.onHostContext({ toolInfo: { tool: { name: "adjust_balance" } } });
	`); err != nil {
		t.Fatalf("cancel and repoint: %v", err)
	}
	commit(t, vm, "adjust_balance")
	if _, err := vm.RunString(`
		recorded.settlers[0].resolve({ structuredContent: { result: {} } });
	`); err != nil {
		t.Fatalf("stale resolve: %v", err)
	}
	if _, err := vm.RunString(`__app.ontoolresult({ structuredContent: { result: {} } });`); err != nil {
		t.Fatalf("result for the newer call: %v", err)
	}
	paints, err := vm.RunString(`JSON.stringify(__paints)`)
	if err != nil {
		t.Fatalf("read paints: %v", err)
	}
	if !strings.Contains(paints.String(), "adjust_balance") {
		t.Errorf("a stale call's resolve disowned the newer in-flight call: %s", paints.String())
	}
}

func TestBootRepointDropsTheEarlierCallsArguments(t *testing.T) {
	vm := bootVM(t, true)
	hostNamed(t, vm, "adjust_balance")
	if _, err := vm.RunString(`
		current = { actions: { commit: { tool: "adjust_balance", args: { targetBalance: "999000" } } } };
		__dispatchAction(__mkEl({ "data-action": "commit" }), null);
	`); err != nil {
		t.Fatalf("first adjust: %v", err)
	}
	if _, err := vm.RunString(`hostHandlers.onHostContext({ toolInfo: { id: 42, tool: { name: "adjust_balance" } } });`); err != nil {
		t.Fatalf("repoint: %v", err)
	}
	commit(t, vm, "adjust_balance")
	if got := lastArgs(t, vm); strings.Contains(got, "999000") {
		t.Errorf("a repointed view carried the previous call's arguments into a new write: %s", got)
	}
}

func TestBootCancellationAfterARepointStillUnlocksTheView(t *testing.T) {
	vm := bootVMSettling(t, "globalThis.recorded.settlers.push({resolve: resolve, reject: reject})")
	hostNamed(t, vm, "list_wallets")
	commit(t, vm, "close_period")
	if _, err := vm.RunString(`
		hostHandlers.onHostContext({ toolInfo: { id: 7, tool: { name: "adjust_balance" } } });
		__app.ontoolcancelled({ reason: "stopped" });
	`); err != nil {
		t.Fatalf("repoint then cancel: %v", err)
	}
	commit(t, vm, "adjust_balance")
	n, err := vm.RunString(`recorded.calls.length`)
	if err != nil {
		t.Fatalf("read calls: %v", err)
	}
	if n.ToInteger() != 2 {
		t.Errorf("a cancellation after a repoint left the view input-locked, calls = %d", n.ToInteger())
	}
}
