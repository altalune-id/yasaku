// boot.js
const root = document.getElementById("root");
let current = { actions: {} };
let inflight = null;
let lastCall = null;
let lastToolInfo = "";

// SECURITY: ui/notifications/tool-input carries no tool name and no call id, so hostInput is
// labelled only by hostContext and is never read when answers already names the tool.
let answers = { tool: "", args: {} };
let hostInput = { tool: "", args: {} };

function capturedArgs() { return answers.args; }

// NOTE: hostContext names the tool that instantiated the view, never the tool a view-initiated call ran.
function currentToolName(b) {
  const ctx = b.hostContext();
  return ctx && ctx.toolInfo && ctx.toolInfo.tool ? ctx.toolInfo.tool.name : "";
}

function toolInfoKey(ctx) {
  const info = ctx && ctx.toolInfo;
  if (!info || !info.tool) return "";
  return String(info.id === undefined ? "" : info.id) + "|" + info.tool.name;
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

function paintCancelled(reason) {
  if (lastCall) lastCall.done = true;
  inflight = null;
  current = { actions: {} };
  const why = reason ? html`<p class="ya-muted">${reason}</p>` : "";
  root.innerHTML = html`<div class="ya-root"><p class="ya-muted">The host cancelled that tool call.</p>${raw(why)}</div>`;
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

const hostHandlers = {
  onToolInput: function (params, tool) {
    hostInput = { tool: tool || "", args: (params && params.arguments) || {} };
  },
  onToolResult: function (result) {
    if (!lastCall) {
      paintResult(currentToolName(bridge), result);
      return;
    }
    if (lastCall.done) {
      lastCall = null;
      return;
    }
    lastCall.done = true;
    paintResult(lastCall.tool, result);
  },
  onToolCancelled: function (params) {
    if (lastCall && lastCall.done) return;
    paintCancelled(params && params.reason);
  },
  onHostContext: function (ctx) {
    const key = toolInfoKey(ctx);
    if (!key || key === lastToolInfo) return;
    lastToolInfo = key;
    lastCall = null;
    answers = { tool: "", args: {} };
    hostInput = { tool: "", args: {} };
  },
};

const bridge = createBridge(hostHandlers, function () { return globalThis.__extApps; });

// NOTE: a preview is not round-trippable into its own request — close_period's is a bare
// Snapshot, adjust_balance's carries the delta. Commits build on the captured tool input.
// SECURITY: a.args is merged LAST so a form field can never override a declared confirm.
function dispatchAction(el, form) {
  if (inflight) return;
  const id = el.getAttribute("data-action");
  const a = current.actions[id];
  if (!a) return;
  delete current.actions[id];

  const args = {};
  if (answers.tool === a.tool) mergeDeep(args, answers.args);
  else if (hostInput.tool === a.tool) mergeDeep(args, hostInput.args);
  mergeDeep(args, formValues(form));
  mergeDeep(args, a.args);
  answers = { tool: a.tool, args: args };

  const call = { tool: a.tool, done: false };
  inflight = call;
  lastCall = call;
  el.disabled = true;
  bridge.callTool(a.tool, args)
    .then(function (res) {
      if (call.done) {
        if (lastCall === call) lastCall = null;
        return;
      }
      call.done = true;
      paintResult(call.tool, res);
    })
    .catch(function (e) {
      if (call.done) return;
      call.done = true;
      current = { actions: {} };
      root.innerHTML = html`<div class="ya-root"><p class="ya-error">That request was not completed.</p></div>`;
      console.error(e);
    })
    .finally(function () {
      if (inflight === call) inflight = null;
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

root.innerHTML = html`<div class="ya-root"><p class="ya-muted">Loading…</p></div>`;
bridge.connect().then(function (ctx) {
  lastToolInfo = toolInfoKey(ctx);
}).catch(function (e) {
  root.innerHTML = html`<div class="ya-root"><p class="ya-error">Could not reach the host.</p></div>`;
  console.error(e);
});
