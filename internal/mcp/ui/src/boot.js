// boot.js
const root = document.getElementById("root");
let current = { actions: {} };

function currentToolName(bridge) {
  const ctx = bridge.hostContext();
  return ctx && ctx.toolInfo && ctx.toolInfo.tool ? ctx.toolInfo.tool.name : "";
}

function paint(name, data) {
  current = renderTool(name, data);
  root.innerHTML = current.html;
}

const bridge = createBridge({
  onToolInput: function () {},
  onToolResult: function (result) {
    if (result && result.isError) {
      current = { actions: {} };
      root.innerHTML = html`<div class="ya-root"><p class="ya-error">That tool call failed.</p></div>`;
      return;
    }
    const name = currentToolName(bridge);
    if (!name) {
      root.innerHTML = html`<div class="ya-root"><p class="ya-muted">The host sent a result with no tool name.</p></div>`;
      return;
    }
    paint(name, (result || {}).structuredContent || {});
  },
  onHostContext: function () {},
}, function () { return globalThis.__extApps; });

root.addEventListener("click", function (ev) {
  const el = ev.target.closest("[data-action]");
  if (!el) return;
  const a = current.actions[el.getAttribute("data-action")];
  if (!a) return;
  el.disabled = true;
  bridge.callTool(a.tool, a.args)
    .catch(function (e) { console.error(e); })
    .finally(function () { el.disabled = false; });
});

root.innerHTML = html`<div class="ya-root"><p class="ya-muted">Loading…</p></div>`;
bridge.connect().catch(function (e) {
  console.error(e);
  root.innerHTML = html`<div class="ya-root"><p class="ya-error">Could not reach the host.</p></div>`;
});
