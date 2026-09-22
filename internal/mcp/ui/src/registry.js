// registry.js
const VIEWS = {};

function registerView(name, fn) {
  VIEWS[name] = fn;
}

function renderTool(name, data) {
  const view = VIEWS[name];
  if (!view) {
    return { html: html`<div class="ya-root"><p class="ya-muted">No view for "${name}".</p></div>`, actions: {} };
  }
  const actions = {};
  const body = view(data || {}, function action(id, tool, args) {
    actions[id] = { tool: tool, args: args || {} };
    return id;
  });
  return { html: body, actions: actions };
}
