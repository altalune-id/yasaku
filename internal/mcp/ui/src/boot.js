// boot.js
const root = document.getElementById("root");
let current = { actions: {} };
let captured = { tool: "", args: {} };
let pending = false;

function capturedArgs() { return captured.args; }

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

// setIn expands a dotted field name into the nested object the request expects;
// a flat "target.org" is an unknown field and protojson discards it silently.
function setIn(obj, path, value) {
  const parts = String(path).split(".");
  let node = obj;
  for (let i = 0; i < parts.length - 1; i++) {
    const k = parts[i];
    if (!node[k] || typeof node[k] !== "object") node[k] = {};
    node = node[k];
  }
  node[parts[parts.length - 1]] = value;
}

function formValues(form) {
  const out = {};
  if (!form) return out;
  const fields = form.querySelectorAll("[name]");
  for (let i = 0; i < fields.length; i++) {
    const name = fields[i].getAttribute("name");
    if (name) setIn(out, name, fields[i].value);
  }
  return out;
}

function mergeDeep(target, source) {
  for (const k in source) {
    const v = source[k];
    if (v && typeof v === "object" && !Array.isArray(v)) {
      if (!target[k] || typeof target[k] !== "object") target[k] = {};
      mergeDeep(target[k], v);
      continue;
    }
    target[k] = v;
  }
  return target;
}

const bridge = createBridge({
  onToolInput: function (params, tool) {
    captured = { tool: tool || "", args: (params && params.arguments) || {} };
  },
  onToolResult: function (result) { paintResult(currentToolName(bridge), result); },
  onHostContext: function () {},
}, function () { return globalThis.__extApps; });

// NOTE: a preview is not round-trippable into its own request — close_period's is a bare
// Snapshot, adjust_balance's carries the delta. Commits build on the captured tool input.
// SECURITY: a.args is merged LAST so a form field can never override a declared confirm.
function dispatchAction(el, form) {
  if (pending) return;
  const id = el.getAttribute("data-action");
  const a = current.actions[id];
  if (!a) return;
  delete current.actions[id];

  const args = {};
  if (captured.tool === a.tool) mergeDeep(args, captured.args);
  mergeDeep(args, formValues(form));
  mergeDeep(args, a.args);
  captured = { tool: a.tool, args: args };

  pending = true;
  el.disabled = true;
  bridge.callTool(a.tool, args)
    .then(function (res) { paintResult(a.tool, res); })
    .catch(function (e) {
      current = { actions: {} };
      root.innerHTML = html`<div class="ya-root"><p class="ya-error">That request was not completed.</p></div>`;
      console.error(e);
    })
    .finally(function () { pending = false; });
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

root.innerHTML = html`<div class="ya-root"><p class="ya-muted">Loading…</p></div>`;
bridge.connect().catch(function (e) {
  root.innerHTML = html`<div class="ya-root"><p class="ya-error">Could not reach the host.</p></div>`;
  console.error(e);
});
