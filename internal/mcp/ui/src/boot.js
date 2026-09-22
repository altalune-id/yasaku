// boot.js
const root = document.getElementById("root");
let current = { actions: {} };
let lastInput = {};

function currentToolName(b) {
  const ctx = b.hostContext();
  return ctx && ctx.toolInfo && ctx.toolInfo.tool ? ctx.toolInfo.tool.name : "";
}

function paint(name, data) {
  if (!name) {
    current = { actions: {} };
    root.innerHTML = html`<div class="ya-root"><p class="ya-muted">The host sent a result with no tool name.</p></div>`;
    return;
  }
  current = renderTool(name, data || {});
  root.innerHTML = current.html;
}

function paintResult(name, result) {
  if (result && result.isError) {
    current = { actions: {} };
    root.innerHTML = html`<div class="ya-root"><p class="ya-error">That tool call failed.</p></div>`;
    return;
  }
  paint(name, (result || {}).structuredContent || {});
}

function formValues(form) {
  const out = {};
  if (!form) return out;
  const fields = form.querySelectorAll("[name]");
  for (let i = 0; i < fields.length; i++) {
    const name = fields[i].getAttribute("name");
    if (name) out[name] = fields[i].value;
  }
  return out;
}

const bridge = createBridge({
  onToolInput: function (params) { lastInput = (params && params.arguments) || {}; },
  onToolResult: function (result) { paintResult(currentToolName(bridge), result); },
  onHostContext: function () {},
}, function () { return globalThis.__extApps; });

// NOTE: a preview is not round-trippable into its own request — close_period's is a
// bare Snapshot, adjust_balance's carries the delta not the target. The commit is built
// from the original tool input, never from the response.
function dispatchAction(el, form) {
  const id = el.getAttribute("data-action");
  const a = current.actions[id];
  if (!a) return;
  delete current.actions[id];
  const args = Object.assign({}, lastInput, a.args, formValues(form));
  el.disabled = true;
  bridge.callTool(a.tool, args)
    .then(function (res) { paintResult(a.tool, res); })
    .catch(function (e) {
      current = { actions: {} };
      root.innerHTML = html`<div class="ya-root"><p class="ya-error">That request was not completed.</p></div>`;
      console.error(e);
    });
}

root.addEventListener("click", function (ev) {
  const el = ev.target.closest("[data-action]");
  if (!el) return;
  dispatchAction(el, ev.target.closest("form"));
});

root.addEventListener("submit", function (ev) {
  if (ev.preventDefault) ev.preventDefault();
  const el = ev.target.querySelector ? ev.target.querySelector("[data-action]") : null;
  if (el) dispatchAction(el, ev.target);
});

globalThis.__toolInput = function (p) { lastInput = (p && p.arguments) || {}; };
globalThis.__dispatchAction = dispatchAction;

root.innerHTML = html`<div class="ya-root"><p class="ya-muted">Loading…</p></div>`;
bridge.connect().catch(function (e) {
  root.innerHTML = html`<div class="ya-root"><p class="ya-error">Could not reach the host.</p></div>`;
  console.error(e);
});
