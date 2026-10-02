package ui

import (
	"strings"
	"testing"

	"github.com/dop251/goja"
)

func bootVM(t *testing.T, resolve bool) *goja.Runtime {
	t.Helper()
	return bootVMSettling(t, settleFor(resolve))
}

func settleFor(resolve bool) string {
	if !resolve {
		return "reject(new Error('denied'))"
	}
	return "resolve({structuredContent:{posts:[{id:'p1',title:'Shipped',status:'published'}]}})"
}

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
		globalThis.recorded = {calls: []};
		globalThis.__extApps = null;
		globalThis.fakeLoader = function () {
			return {
				App: function (info, caps) {
					globalThis.recorded.caps = caps;
					globalThis.__app = this;
					this.connect = function () { return Promise.resolve(); };
					this.getHostContext = function () { return { toolInfo: { tool: { name: "blog_list" } } }; };
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
	// SECURITY: the served bundle exports no dispatch seam; the test installs one after evaluation.
	if _, err := vm.RunString(`
		globalThis.__dispatchAction = dispatchAction;
		globalThis.__toolInput = function (p, tool) { hostHandlers.onToolInput(p, tool); };
	`); err != nil {
		t.Fatalf("install seam: %v", err)
	}
	return vm
}

func hostNamed(t *testing.T, vm *goja.Runtime, name string) {
	t.Helper()
	if _, err := vm.RunString(`__app.getHostContext = function () { return { toolInfo: { tool: { name: ` + jsQuote(name) + ` } } }; };`); err != nil {
		t.Fatalf("set host context: %v", err)
	}
}

func lastArgs(t *testing.T, vm *goja.Runtime) string {
	t.Helper()
	return jsString(t, vm, `JSON.stringify(recorded.calls[recorded.calls.length - 1].arguments)`)
}

func TestBootMountsTheAppElement(t *testing.T) {
	vm := bootVM(t, true)
	if got := jsString(t, vm, `__root.children[0].tagName`); got != "yasaku-app" {
		t.Errorf("root holds %q, want yasaku-app", got)
	}
	if got := jsString(t, vm, `typeof __appEl.onaction`); got != "function" {
		t.Errorf("boot.js did not wire the app element's action channel, got %q", got)
	}
}

func TestBootMergesToolInputAndFormOverDeclaredArgs(t *testing.T) {
	vm := bootVM(t, true)
	if _, err := vm.RunString(`
		__toolInput({ arguments: { target: { org: "acme" }, projectId: "prj_1", status: "draft" } }, "blog_list");
		current = { actions: { refresh: { tool: "blog_list", args: { status: "published" } } } };
		const form = { children: [ { attrs: { name: "projectId" }, value: "prj_2", getAttribute(k){ return this.attrs[k] || null; } } ],
		               querySelectorAll(){ return this.children; } };
		__dispatchAction("refresh", __mkEl({}), form);
	`); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	got := jsString(t, vm, `JSON.stringify(recorded.calls[0])`)
	const want = `{"name":"blog_list","arguments":{"target":{"org":"acme"},"projectId":"prj_2","status":"published"}}`
	if got != want {
		t.Errorf("callServerTool got\n %s\nwant\n %s", got, want)
	}
}

// TestBootFormFieldCannotOverrideADeclaredArgument: a.args merges last, so a field named like a declared argument cannot repoint the write at another row.
func TestBootFormFieldCannotOverrideADeclaredArgument(t *testing.T) {
	vm := bootVM(t, true)
	if _, err := vm.RunString(`
		current = { actions: { publish: { tool: "blog_publish", args: { postId: "declared" } } } };
		__dispatchAction(
			"publish",
			__mkEl({}),
			{
				querySelectorAll: function (sel) {
					if (sel !== "[name]") return [];
					return [__mkEl({ name: "postId" })].map(function (el) { el.value = "someone-elses-post"; return el; });
				},
			}
		);
	`); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if got := lastArgs(t, vm); !strings.Contains(got, `"postId":"declared"`) {
		t.Errorf("a form field overrode the declared argument: %s", got)
	}
}

func TestBootIgnoresToolInputFromADifferentTool(t *testing.T) {
	vm := bootVM(t, true)
	if _, err := vm.RunString(`
		__toolInput({ arguments: { postId: "from-another-tool", status: "stale" } }, "blog_list");
		current = { actions: { publish: { tool: "blog_publish", args: {} } } };
		__dispatchAction("publish", __mkEl({}), null);
	`); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if got := lastArgs(t, vm); strings.Contains(got, "stale") {
		t.Errorf("args = %s; another tool's captured input must not leak into a write", got)
	}
}

// TestBootSubjectReadsTheViewsOwnAnswersNotTheHostAnnouncement: the tool-input notification carries no tool name, so a late host announcement must not become the view's own state.
func TestBootSubjectReadsTheViewsOwnAnswersNotTheHostAnnouncement(t *testing.T) {
	vm := bootVM(t, true)
	hostNamed(t, vm, "blog_publish")
	if _, err := vm.RunString(`
		answers = { tool: "blog_publish", args: { postId: "the-view-s-own" } };
		__app.ontoolinput({ arguments: { postId: "from-the-host" } });
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if got := jsString(t, vm, `JSON.stringify(capturedArgs())`); strings.Contains(got, "from-the-host") {
		t.Errorf("capturedArgs surfaced the host announcement instead of the view's answers: %s", got)
	}
	if _, err := vm.RunString(`
		current = { actions: { publish: { tool: "blog_publish", args: {} } } };
		__dispatchAction("publish", __mkEl({}), null);
	`); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if got := lastArgs(t, vm); !strings.Contains(got, `"postId":"the-view-s-own"`) {
		t.Errorf("the write used the host announcement instead of the view's own answers: %s", got)
	}
}

// TestBootWriteCannotFireTwiceWhileACallIsInflight re-declares the action between dispatches so the inflight latch is the only thing left to block the second write.
func TestBootWriteCannotFireTwiceWhileACallIsInflight(t *testing.T) {
	vm := bootVM(t, true)
	if _, err := vm.RunString(`
		const decl = { tool: "blog_publish", args: { postId: "p1" } };
		current = { actions: { publish: decl } };
		const btn = __mkEl({});
		__dispatchAction("publish", btn, null);
		current.actions.publish = decl;
		__dispatchAction("publish", btn, null);
	`); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if got := jsString(t, vm, `String(recorded.calls.length)`); got != "1" {
		t.Errorf("callServerTool fired %s times, want 1 — a second click while a call is inflight must not write again", got)
	}
}

// TestBootWriteCannotFireTwiceOnceTheActionIsConsumed clears the inflight latch between dispatches so consuming the declared action is the only thing left to block the second write.
func TestBootWriteCannotFireTwiceOnceTheActionIsConsumed(t *testing.T) {
	vm := bootVM(t, true)
	if _, err := vm.RunString(`
		current = { actions: { publish: { tool: "blog_publish", args: { postId: "p1" } } } };
		const btn = __mkEl({});
		__dispatchAction("publish", btn, null);
		inflight = null;
		__dispatchAction("publish", btn, null);
	`); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if got := jsString(t, vm, `String(recorded.calls.length)`); got != "1" {
		t.Errorf("callServerTool fired %s times, want 1 — a consumed action must not write again", got)
	}
}

// TestBootFormFieldCannotPoisonALaterCall: a field name is attacker-controlled whenever a view renders one from tool output, so a dotted path must never reach Object.prototype.
func TestBootFormFieldCannotPoisonALaterCall(t *testing.T) {
	for _, tc := range []struct{ name, field, value string }{
		{"proto segment", "__proto__.postId", `"attacker-owned"`},
		{"constructor prototype segment", "constructor.prototype.postId", `"attacker-owned"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vm := bootVM(t, true)
			if _, err := vm.RunString(`
				current = { actions: { forge: { tool: "blog_list", args: {} } } };
				__dispatchAction("forge", __mkEl({}), {
					querySelectorAll: function (sel) {
						if (sel !== "[name]") return [];
						const el = __mkEl({ name: ` + jsQuote(tc.field) + ` });
						el.value = ` + tc.value + `;
						return [el];
					},
				});
				inflight = null;
				current = { actions: { publish: { tool: "blog_publish", args: {} } } };
				__dispatchAction("publish", __mkEl({}), null);
			`); err != nil {
				t.Fatalf("dispatch: %v", err)
			}
			if got := jsString(t, vm, `String(recorded.calls.length)`); got != "2" {
				t.Fatalf("recorded %s calls, want 2", got)
			}
			if got := jsString(t, vm, `JSON.stringify(recorded.calls)`); strings.Contains(got, "attacker-owned") || strings.Contains(got, "postId") {
				t.Errorf("a forged form field reached a tool call's arguments: %s", got)
			}
			if got := jsString(t, vm, `String(({}).postId)`); got != "undefined" {
				t.Errorf("a forged form field wrote %q onto Object.prototype", got)
			}
		})
	}
}

// TestBootToolInputCannotPoisonALaterCall: JSON.parse mints a real own "__proto__" key, so a deep merge that followed it would write onto Object.prototype.
func TestBootToolInputCannotPoisonALaterCall(t *testing.T) {
	vm := bootVM(t, true)
	if _, err := vm.RunString(`
		__toolInput({ arguments: JSON.parse('{"__proto__":{"postId":"attacker-owned"}}') }, "blog_list");
		current = { actions: { list: { tool: "blog_list", args: {} } } };
		__dispatchAction("list", __mkEl({}), null);
		inflight = null;
		current = { actions: { publish: { tool: "blog_publish", args: {} } } };
		__dispatchAction("publish", __mkEl({}), null);
	`); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if got := jsString(t, vm, `String(recorded.calls.length)`); got != "2" {
		t.Fatalf("recorded %s calls, want 2", got)
	}
	if got := jsString(t, vm, `JSON.stringify(recorded.calls)`); strings.Contains(got, "attacker-owned") || strings.Contains(got, "postId") {
		t.Errorf("a forged tool-input key reached a tool call's arguments: %s", got)
	}
	if got := jsString(t, vm, `String(({}).postId)`); got != "undefined" {
		t.Errorf("a forged tool-input key wrote %q onto Object.prototype", got)
	}
}

// TestBootIgnoresAnActionNamedLikeAnInheritedMember: every action map the dispatcher reads is null-prototype, so "toString" resolves to nothing rather than to an inherited member.
func TestBootIgnoresAnActionNamedLikeAnInheritedMember(t *testing.T) {
	for _, tc := range []struct {
		name    string
		resolve bool
		seed    string
	}{
		{"a rendered view", true, `current = renderTool("blog_list", { posts: [] });`},
		{"a result with no tool name", true, `paint("", {});`},
		{"a failed result", true, `paintResult("blog_list", { isError: true });`},
		{"a cancelled call", true, `paintCancelled("nope");`},
		{"a rejected call", false, `current = { actions: { go: { tool: "blog_list", args: {} } } }; __dispatchAction("go", __mkEl({}), null);`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vm := bootVMSettling(t, settleFor(tc.resolve))
			if _, err := vm.RunString(tc.seed); err != nil {
				t.Fatalf("seed: %v", err)
			}
			before := jsString(t, vm, `String(recorded.calls.length)`)
			for _, id := range []string{"toString", "valueOf", "constructor", "hasOwnProperty"} {
				if _, err := vm.RunString(`inflight = null; __dispatchAction(` + jsQuote(id) + `, __mkEl({}), null);`); err != nil {
					t.Fatalf("dispatch %s: %v", id, err)
				}
			}
			if got := jsString(t, vm, `String(recorded.calls.length)`); got != before {
				t.Errorf("an action named like an inherited member fired a call: %s calls, want %s", got, before)
			}
		})
	}
}

func TestBootPaintsFromTheCallToolPromise(t *testing.T) {
	vm := bootVM(t, true)
	if _, err := vm.RunString(`
		current = { actions: { go: { tool: "blog_list", args: {} } } };
		__dispatchAction("go", __mkEl({}), null);
	`); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if got := jsString(t, vm, `__appEl.status`); got != "view" {
		t.Errorf("app status = %q, want view", got)
	}
	if got := jsString(t, vm, `JSON.stringify(__appEl.view.model)`); !strings.Contains(got, "Shipped") {
		t.Errorf("panel did not repaint from the callTool promise:\n%s", got)
	}
}

func TestBootShowsARejectedState(t *testing.T) {
	vm := bootVM(t, false)
	if _, err := vm.RunString(`
		current = { actions: { go: { tool: "blog_list", args: {} } } };
		__dispatchAction("go", __mkEl({}), null);
	`); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if got := jsString(t, vm, `__appEl.status`); got != "error" {
		t.Errorf("a denied or failed call must show a visible error state, got %q", got)
	}
}

func TestBootIgnoresAnActionItNeverDeclared(t *testing.T) {
	vm := bootVM(t, true)
	if _, err := vm.RunString(`
		current = { actions: { publish: { tool: "blog_publish", args: { postId: "p1" } } } };
		__dispatchAction("forged", __mkEl({}), null);
	`); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if got := jsString(t, vm, `String(recorded.calls.length)`); got != "0" {
		t.Errorf("an undeclared action id fired %s calls, want 0", got)
	}
}

// TestBootSetInRejectsAWholeNameProto: "__proto__" as a whole field name swaps the form object's prototype, which no later own-key enumeration would surface.
func TestBootSetInRejectsAWholeNameProto(t *testing.T) {
	vm := bootVM(t, true)
	if _, err := vm.RunString(`
		globalThis.__out = {};
		setIn(__out, "__proto__", { postId: "attacker-owned" });
	`); err != nil {
		t.Fatalf("setIn: %v", err)
	}
	if got := jsString(t, vm, `String(Object.getPrototypeOf(__out) === Object.prototype)`); got != "true" {
		t.Error("setIn replaced the target's prototype")
	}
	if got := jsString(t, vm, `String(__out.postId)`); got != "undefined" {
		t.Errorf("setIn leaked %q onto the target through its prototype", got)
	}
}

// TestBootInheritedKeysNeverReachToolArguments: mergeDeep enumerates own keys only, so a prototype polluted through any other path cannot add arguments to a later tool call.
func TestBootInheritedKeysNeverReachToolArguments(t *testing.T) {
	vm := bootVM(t, true)
	if _, err := vm.RunString(`
		Object.defineProperty(Object.prototype, "postId", {
			value: "inherited", enumerable: true, configurable: true, writable: true,
		});
		try {
			__toolInput({ arguments: {} }, "blog_publish");
			current = { actions: { publish: { tool: "blog_publish", args: {} } } };
			__dispatchAction("publish", __mkEl({}), null);
		} finally {
			delete Object.prototype.postId;
		}
	`); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if got := lastArgs(t, vm); strings.Contains(got, "inherited") || strings.Contains(got, "postId") {
		t.Errorf("an inherited key reached a tool call's arguments: %s", got)
	}
}

const (
	writtenSettle  = "resolve({structuredContent:{result:{id:'written'}}})"
	deferredSettle = "globalThis.recorded.settlers.push({resolve: resolve, reject: reject})"
)

func yasakuBootVM(t *testing.T, settle string) *goja.Runtime {
	t.Helper()
	vm := bootVMSettling(t, settle)
	if _, err := vm.RunString(`
		globalThis.recorded.settlers = [];
		globalThis.__paints = [];
		const __realPaint = paint;
		paint = function (name, data) { globalThis.__paints.push(name); return __realPaint(name, data); };
	`); err != nil {
		t.Fatalf("install paint log: %v", err)
	}
	return vm
}

func commitTool(t *testing.T, vm *goja.Runtime, tool string) {
	t.Helper()
	if _, err := vm.RunString(`current = { actions: { commit: { tool: ` + jsQuote(tool) + `, args: { confirm: true } } } };
		__dispatchAction("commit", __mkEl({}), null);`); err != nil {
		t.Fatalf("commit %s: %v", tool, err)
	}
}

func paints(t *testing.T, vm *goja.Runtime) string {
	t.Helper()
	return jsString(t, vm, `JSON.stringify(__paints)`)
}

func TestBootMergesToolInputAndFormIntoAClosePeriodCommit(t *testing.T) {
	vm := yasakuBootVM(t, writtenSettle)
	if _, err := vm.RunString(`
		__toolInput({ arguments: { target: { org: "acme" }, period: "per_9", endDate: "2026-09-30" } }, "close_period");
		current = { actions: { commit: { tool: "close_period", args: { confirm: true } } } };
		const end = __mkEl({ name: "endDate" }, [], "input"); end.value = "2026-09-29";
		__dispatchAction("commit", __mkEl({}), __mkEl({}, [end], "form"));
	`); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	got := jsString(t, vm, `JSON.stringify(recorded.calls[0])`)
	const want = `{"name":"close_period","arguments":{"target":{"org":"acme"},"period":"per_9","endDate":"2026-09-29","confirm":true}}`
	if got != want {
		t.Errorf("callServerTool got\n %s\nwant\n %s", got, want)
	}
}

func TestBootExpandsDottedFieldNamesIntoNestedArgs(t *testing.T) {
	vm := yasakuBootVM(t, writtenSettle)
	if _, err := vm.RunString(`
		__toolInput({ arguments: {} }, "create_wallet");
		current = { actions: { edit: { tool: "create_wallet", args: { confirm: false } } } };
		const sel = __mkEl({ name: "target.org" }, [], "select");
		sel.value = "acme";
		__dispatchAction("edit", __mkEl({}), __mkEl({}, [sel], "form"));
	`); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if got := jsString(t, vm, `JSON.stringify(recorded.calls[0].arguments.target)`); got != `{"org":"acme"}` {
		t.Errorf("target = %s, want {\"org\":\"acme\"} — a flat \"target.org\" key is dropped by DiscardUnknown and the need returns forever", got)
	}
	if got := jsString(t, vm, `String("target.org" in recorded.calls[0].arguments)`); got != "false" {
		t.Error("the flat dotted key must not also be sent")
	}
}

func TestBootRemembersEarlierNeedsAnswers(t *testing.T) {
	vm := yasakuBootVM(t, writtenSettle)
	if _, err := vm.RunString(`
		__toolInput({ arguments: { target: { org: "acme" } } }, "create_wallet");
		current = { actions: { edit: { tool: "create_wallet", args: { confirm: false } } } };
		const n = __mkEl({ name: "name" }, [], "input"); n.value = "Dompet";
		__dispatchAction("edit", __mkEl({}), __mkEl({}, [n], "form"));
	`); err != nil {
		t.Fatalf("round 1: %v", err)
	}
	if _, err := vm.RunString(`
		current = { actions: { edit: { tool: "create_wallet", args: { confirm: false } } } };
		const k = __mkEl({ name: "kind" }, [], "select"); k.value = "cash";
		__dispatchAction("edit", __mkEl({}), __mkEl({}, [k], "form"));
	`); err != nil {
		t.Fatalf("round 2: %v", err)
	}
	got := jsString(t, vm, `JSON.stringify(recorded.calls[1].arguments)`)
	if !strings.Contains(got, `"name":"Dompet"`) || !strings.Contains(got, `"kind":"cash"`) || !strings.Contains(got, `"org":"acme"`) {
		t.Errorf("second round = %s; the server resolves needs one at a time, so earlier answers must persist or the form loops forever", got)
	}
}

func TestBootKeepsEarlierNeedsAnswersAcrossATrailingToolInput(t *testing.T) {
	vm := yasakuBootVM(t, writtenSettle)
	hostNamed(t, vm, "create_wallet")
	if _, err := vm.RunString(`
		current = { actions: { a1: { tool: "create_wallet", args: { name: "Dompet" } } } };
		__dispatchAction("a1", __mkEl({}), null);
	`); err != nil {
		t.Fatalf("round 1: %v", err)
	}
	if _, err := vm.RunString(`__app.ontoolinput({ arguments: { name: "Dompet" } });`); err != nil {
		t.Fatalf("trailing tool-input: %v", err)
	}
	if _, err := vm.RunString(`
		current = { actions: { a2: { tool: "create_wallet", args: { kind: "cash" } } } };
		__dispatchAction("a2", __mkEl({}), null);
	`); err != nil {
		t.Fatalf("round 2: %v", err)
	}
	if got := lastArgs(t, vm); !strings.Contains(got, "Dompet") {
		t.Errorf("round 1's answer was lost, so the server re-asks forever: %s", got)
	}
}

func TestBootCapturesTheCalledToolOnAHostToolInput(t *testing.T) {
	vm := yasakuBootVM(t, deferredSettle)
	if _, err := vm.RunString(`
		__toolInput({ arguments: { target: { org: "acme" } } }, "create_wallet");
		current = { actions: { edit: { tool: "create_wallet", args: { confirm: false } } } };
		const n = __mkEl({ name: "name" }, [], "input"); n.value = "Dompet";
		__dispatchAction("edit", __mkEl({}), __mkEl({}, [n], "form"));
		__app.ontoolinput({ arguments: { target: { org: "acme" }, name: "Dompet" } });
		recorded.settlers[0].resolve({ structuredContent: { needs: { needs: [{ field: "kind", candidates: ["cash"] }] } } });
	`); err != nil {
		t.Fatalf("round 1: %v", err)
	}
	if _, err := vm.RunString(`
		current = { actions: { edit: { tool: "create_wallet", args: { confirm: false } } } };
		const k = __mkEl({ name: "kind" }, [], "select"); k.value = "cash";
		__dispatchAction("edit", __mkEl({}), __mkEl({}, [k], "form"));
	`); err != nil {
		t.Fatalf("round 2: %v", err)
	}
	if got := jsString(t, vm, `JSON.stringify(recorded.calls[1].arguments)`); !strings.Contains(got, `"name":"Dompet"`) {
		t.Errorf("second round = %s; the host re-sent tool-input for the view-initiated call, and mislabelling it breaks the answers merge guard", got)
	}
}

func TestBootPaintsACancelledStateAndUnlocksTheView(t *testing.T) {
	vm := yasakuBootVM(t, deferredSettle)
	if _, err := vm.RunString(`
		__toolInput({ arguments: {} }, "get_wallet");
		current = { actions: { go: { tool: "get_wallet", args: {} } } };
		__dispatchAction("go", __mkEl({}), null);
		__app.ontoolcancelled({ reason: "<img src=x onerror=alert(1)>" });
	`); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if got := jsString(t, vm, `__appEl.status`); got != "notice" {
		t.Errorf("status = %q after ui/notifications/tool-cancelled, want notice", got)
	}
	if got := jsString(t, vm, `__appEl.message`); !strings.Contains(strings.ToLower(got), "cancel") {
		t.Errorf("a cancelled call must paint a cancelled state, got %q", got)
	}
	if got := jsString(t, vm, `__appEl.detail`); got != "<img src=x onerror=alert(1)>" {
		t.Errorf("the cancellation reason must reach the shell as text (Lit escapes it), got %q", got)
	}
	if _, err := vm.RunString(`
		current = { actions: { go: { tool: "get_wallet", args: {} } } };
		__dispatchAction("go", __mkEl({}), null);
	`); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if got := jsString(t, vm, `String(recorded.calls.length)`); got != "2" {
		t.Errorf("calls after retry = %s, want 2 — a cancelled call must release the in-flight lock", got)
	}
}

func TestBootLateCancellationDoesNotWipeAPaintedResult(t *testing.T) {
	vm := yasakuBootVM(t, writtenSettle)
	commitTool(t, vm, "create_wallet")
	if _, err := vm.RunString(`hostHandlers.onToolCancelled({ reason: "timeout" });`); err != nil {
		t.Fatalf("cancel after settle: %v", err)
	}
	if got := jsString(t, vm, `__appEl.status + "|" + __appEl.message`); strings.Contains(got, "cancelled") || !strings.HasPrefix(got, "view|") {
		t.Errorf("a late cancellation wiped an already-painted result: %s", got)
	}
}

func TestBootCancellationSurvivesTheCallSettlingAfterwards(t *testing.T) {
	vm := yasakuBootVM(t, deferredSettle)
	commitTool(t, vm, "create_wallet")
	if _, err := vm.RunString(`
		__app.ontoolcancelled({ reason: "stopped" });
		recorded.settlers[0].resolve({ structuredContent: { result: {} } });
	`); err != nil {
		t.Fatalf("cancel then resolve: %v", err)
	}
	if got := jsString(t, vm, `__appEl.status + "|" + __appEl.message`); !strings.Contains(got, "cancelled") {
		t.Errorf("a cancelled call painted its result anyway: %s", got)
	}
}

func TestBootCancellationSurvivesTheCallRejectingAfterwards(t *testing.T) {
	vm := yasakuBootVM(t, deferredSettle)
	commitTool(t, vm, "create_wallet")
	if _, err := vm.RunString(`
		__app.ontoolcancelled({ reason: "stopped" });
		recorded.settlers[0].reject(new Error("aborted"));
	`); err != nil {
		t.Fatalf("cancel then reject: %v", err)
	}
	if got := jsString(t, vm, `__appEl.status + "|" + __appEl.message`); strings.Contains(got, "not completed") || !strings.Contains(got, "cancelled") {
		t.Errorf("a cancelled call's rejection replaced the cancellation notice: %s", got)
	}
}

func TestBootCancellationAfterARepointStillUnlocksTheView(t *testing.T) {
	vm := yasakuBootVM(t, deferredSettle)
	hostNamed(t, vm, "list_wallets")
	commitTool(t, vm, "close_period")
	if _, err := vm.RunString(`
		hostHandlers.onHostContext({ toolInfo: { id: 7, tool: { name: "adjust_balance" } } });
		__app.ontoolcancelled({ reason: "stopped" });
	`); err != nil {
		t.Fatalf("repoint then cancel: %v", err)
	}
	commitTool(t, vm, "adjust_balance")
	if got := jsString(t, vm, `String(recorded.calls.length)`); got != "2" {
		t.Errorf("a cancellation after a repoint left the view input-locked, calls = %s", got)
	}
}

func TestBootRepointDropsTheEarlierCallsArguments(t *testing.T) {
	vm := yasakuBootVM(t, writtenSettle)
	hostNamed(t, vm, "adjust_balance")
	if _, err := vm.RunString(`
		current = { actions: { commit: { tool: "adjust_balance", args: { targetBalance: "999000" } } } };
		__dispatchAction("commit", __mkEl({}), null);
	`); err != nil {
		t.Fatalf("first adjust: %v", err)
	}
	if _, err := vm.RunString(`hostHandlers.onHostContext({ toolInfo: { id: 42, tool: { name: "adjust_balance" } } });`); err != nil {
		t.Fatalf("repoint: %v", err)
	}
	commitTool(t, vm, "adjust_balance")
	if got := lastArgs(t, vm); strings.Contains(got, "999000") {
		t.Errorf("a repointed view carried the previous call's arguments into a new write: %s", got)
	}
}

func TestBootReleasesTheInFlightToolWhenTheHostRepointsTheView(t *testing.T) {
	vm := yasakuBootVM(t, writtenSettle)
	if _, err := vm.RunString(`
		current = { actions: { commit: { tool: "get_wallet", args: {} } } };
		__dispatchAction("commit", __mkEl({}), null);
		__app.getHostContext = function () { return { toolInfo: { tool: { name: "list_recent_tx" } } }; };
		hostHandlers.onHostContext({ toolInfo: { tool: { name: "list_recent_tx" } } });
		hostHandlers.onToolResult({ structuredContent: { transactions: [] } });
	`); err != nil {
		t.Fatalf("repoint: %v", err)
	}
	if got := paints(t, vm); !strings.Contains(got, "list_recent_tx") {
		t.Errorf("a host-repointed view must paint the new tool, got %s", got)
	}
}

func TestBootDoesNotRepointAnInFlightCallOnAContextRefresh(t *testing.T) {
	vm := yasakuBootVM(t, deferredSettle)
	hostNamed(t, vm, "list_wallets")
	if _, err := vm.RunString(`hostHandlers.onHostContext(__app.getHostContext());`); err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	commitTool(t, vm, "create_wallet")
	if _, err := vm.RunString(`
		hostHandlers.onHostContext({ theme: "dark", toolInfo: { tool: { name: "list_wallets" } } });
		__app.ontoolresult({ structuredContent: { result: {} } });
	`); err != nil {
		t.Fatalf("context refresh: %v", err)
	}
	if got := paints(t, vm); strings.Contains(got, "list_wallets") || !strings.Contains(got, "create_wallet") {
		t.Errorf("an unchanged toolInfo repointed an in-flight call: %s", got)
	}
}

func TestBootKeepsTheInFlightCallOnAContextChangeCarryingNoToolInfo(t *testing.T) {
	vm := yasakuBootVM(t, deferredSettle)
	hostNamed(t, vm, "list_wallets")
	commitTool(t, vm, "close_period")
	if _, err := vm.RunString(`
		hostHandlers.onHostContext({ theme: "dark" });
		__app.ontoolresult({ structuredContent: { result: {} } });
	`); err != nil {
		t.Fatalf("theme change: %v", err)
	}
	if got := paints(t, vm); !strings.Contains(got, "close_period") {
		t.Errorf("a theme-only context change repointed the in-flight call: %s", got)
	}
}

func TestBootResolvesAHostNotificationToTheToolItActuallyCalled(t *testing.T) {
	t.Run("notification after the callServerTool promise", func(t *testing.T) {
		vm := yasakuBootVM(t, writtenSettle)
		if _, err := vm.RunString(`
			__toolInput({ arguments: {} }, "get_wallet");
			current = { actions: { go: { tool: "get_wallet", args: {} } } };
			__dispatchAction("go", __mkEl({}), null);
		`); err != nil {
			t.Fatalf("dispatch: %v", err)
		}
		if _, err := vm.RunString(`__app.ontoolresult({ structuredContent: { result: { id: 'written' } } });`); err != nil {
			t.Fatalf("host result: %v", err)
		}
		if got := paints(t, vm); got != `["get_wallet"]` {
			t.Errorf("paints = %s, want [\"get_wallet\"] — hostContext still names the instantiating tool, so a later host notification must not repaint through it", got)
		}
	})

	t.Run("notification before the callServerTool promise", func(t *testing.T) {
		vm := yasakuBootVM(t, deferredSettle)
		if _, err := vm.RunString(`
			__toolInput({ arguments: {} }, "get_wallet");
			current = { actions: { go: { tool: "get_wallet", args: {} } } };
			__dispatchAction("go", __mkEl({}), null);
			__app.ontoolresult({ structuredContent: { result: { id: 'written' } } });
			recorded.settlers[0].resolve({ structuredContent: { result: { id: 'written' } } });
		`); err != nil {
			t.Fatalf("dispatch: %v", err)
		}
		if got := paints(t, vm); got != `["get_wallet"]` {
			t.Errorf("paints = %s, want [\"get_wallet\"] once — the host notification and the promise describe ONE call and must not double-paint", got)
		}
	})

	t.Run("the first host-initiated result still uses host context", func(t *testing.T) {
		vm := yasakuBootVM(t, writtenSettle)
		hostNamed(t, vm, "list_wallets")
		if _, err := vm.RunString(`__app.ontoolresult({ structuredContent: { wallets: [] } });`); err != nil {
			t.Fatalf("host result: %v", err)
		}
		if got := paints(t, vm); got != `["list_wallets"]` {
			t.Errorf("paints = %s, want [\"list_wallets\"] — with no view-initiated call the instantiating tool is the only name there is", got)
		}
	})
}

func TestBootNeverMergesAHostToolInputIntoAnotherToolsWrite(t *testing.T) {
	const foreign = `__app.ontoolinput({ arguments: { wallet: "from-another-tool", note: "stale" } });`

	t.Run("after the view call settled, host context naming the same tool", func(t *testing.T) {
		vm := yasakuBootVM(t, writtenSettle)
		hostNamed(t, vm, "create_wallet")
		commitTool(t, vm, "create_wallet")
		if _, err := vm.RunString(foreign); err != nil {
			t.Fatalf("host input: %v", err)
		}
		commitTool(t, vm, "create_wallet")
		if got := lastArgs(t, vm); strings.Contains(got, "from-another-tool") {
			t.Errorf("another tool's arguments reached a confirm:true write: %s", got)
		}
	})

	t.Run("while the view call is still in flight", func(t *testing.T) {
		vm := yasakuBootVM(t, deferredSettle)
		hostNamed(t, vm, "create_wallet")
		commitTool(t, vm, "create_wallet")
		if _, err := vm.RunString(foreign); err != nil {
			t.Fatalf("host input: %v", err)
		}
		if _, err := vm.RunString(`recorded.settlers[0].resolve({structuredContent:{}});`); err != nil {
			t.Fatalf("settle: %v", err)
		}
		commitTool(t, vm, "create_wallet")
		if got := lastArgs(t, vm); strings.Contains(got, "from-another-tool") {
			t.Errorf("an in-flight window let another tool's arguments into a confirm:true write: %s", got)
		}
	})
}

func TestBootPaintsAHostResultThatFollowsASettledViewCall(t *testing.T) {
	vm := yasakuBootVM(t, writtenSettle)
	hostNamed(t, vm, "create_wallet")
	commitTool(t, vm, "create_wallet")
	if _, err := vm.RunString(`
		__app.getHostContext = function () { return { toolInfo: { tool: { name: "list_wallets" } } }; };
		__app.ontoolresult({ structuredContent: { result: {} } });
		__app.ontoolresult({ structuredContent: { wallets: [] } });
	`); err != nil {
		t.Fatalf("host result: %v", err)
	}
	if got := paints(t, vm); !strings.Contains(got, "list_wallets") {
		t.Errorf("a host result after a settled call was dropped, leaving the panel stale: %s", got)
	}
}

func TestBootLocksOutASecondWriteWhileOneIsInFlight(t *testing.T) {
	vm := yasakuBootVM(t, deferredSettle)
	hostNamed(t, vm, "list_wallets")
	commitTool(t, vm, "close_period")
	if _, err := vm.RunString(`
		__app.ontoolcancelled({ reason: "stopped" });
		hostHandlers.onHostContext({ toolInfo: { tool: { name: "adjust_balance" } } });
	`); err != nil {
		t.Fatalf("cancel and repoint: %v", err)
	}
	commitTool(t, vm, "adjust_balance")
	if _, err := vm.RunString(`recorded.settlers[0].reject(new Error("aborted"));`); err != nil {
		t.Fatalf("stale reject: %v", err)
	}
	commitTool(t, vm, "adjust_balance")
	if got := jsString(t, vm, `String(recorded.calls.length)`); got != "2" {
		t.Errorf("a stale call's settle unlocked a newer in-flight write: %s", jsString(t, vm, `JSON.stringify(recorded.calls.map(function (c) { return c.name; }))`))
	}
}

func TestBootStaleResolveDoesNotDisownANewerCall(t *testing.T) {
	vm := yasakuBootVM(t, deferredSettle)
	hostNamed(t, vm, "list_wallets")
	commitTool(t, vm, "close_period")
	if _, err := vm.RunString(`
		__app.ontoolcancelled({ reason: "stopped" });
		hostHandlers.onHostContext({ toolInfo: { tool: { name: "adjust_balance" } } });
	`); err != nil {
		t.Fatalf("cancel and repoint: %v", err)
	}
	commitTool(t, vm, "adjust_balance")
	if _, err := vm.RunString(`
		recorded.settlers[0].resolve({ structuredContent: { result: {} } });
		__app.ontoolresult({ structuredContent: { result: {} } });
	`); err != nil {
		t.Fatalf("stale resolve then result: %v", err)
	}
	if got := paints(t, vm); !strings.Contains(got, "adjust_balance") {
		t.Errorf("a stale call's resolve disowned the newer in-flight call: %s", got)
	}
}

func TestBootFormFieldCannotOverrideADeclaredConfirm(t *testing.T) {
	vm := yasakuBootVM(t, writtenSettle)
	if _, err := vm.RunString(`
		current = { actions: { preview: { tool: "close_period", args: { confirm: false } } } };
		const c = __mkEl({ name: "confirm" }, [], "input"); c.value = "true";
		__dispatchAction("preview", __mkEl({}), __mkEl({}, [c], "form"));
	`); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if got := lastArgs(t, vm); !strings.Contains(got, `"confirm":false`) {
		t.Errorf("a form field overrode the declared confirm: %s", got)
	}
}

func TestBootPaintsAYasakuViewFromTheCallToolPromise(t *testing.T) {
	vm := yasakuBootVM(t, writtenSettle)
	if _, err := vm.RunString(`
		registerView("get_wallet", walletDetailModel, null);
		current = { actions: { go: { tool: "get_wallet", args: {} } } };
		__dispatchAction("go", __mkEl({}), null);
	`); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if got := jsString(t, vm, `__appEl.status + "|" + __appEl.view.tool + "|" + __appEl.view.model.name`); got != "view|get_wallet|Wallet" {
		t.Errorf("panel = %s, want get_wallet's model painted from the promise, not the host-context tool", got)
	}
}

// TestBootSearchLoadMoreKeepsTheSearchCriteria: the view declares only the cursor, so the query must come from the search_tx call that painted it.
func TestBootSearchLoadMoreKeepsTheSearchCriteria(t *testing.T) {
	vm := yasakuBootVM(t, writtenSettle)
	if _, err := vm.RunString(`
		registerView("search_tx", TX_LIST_MODELS.search_tx, null);
		__toolInput({ arguments: { query: "nasi", from: "2026-09-01", to: "2026-09-30", cursor: "old" } }, "search_tx");
		current = renderTool("search_tx", { transactions: [{ id: "a", kind: "expense" }], nextCursor: "tok" });
		__dispatchAction("more", __mkEl({}), null);
	`); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	const want = `{"name":"search_tx","arguments":{"query":"nasi","from":"2026-09-01","to":"2026-09-30","cursor":"tok"}}`
	if got := jsString(t, vm, `JSON.stringify(recorded.calls[0])`); got != want {
		t.Errorf("load more called\n %s\nwant\n %s", got, want)
	}
}
