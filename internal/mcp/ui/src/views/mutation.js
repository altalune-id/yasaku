// views/mutation.js
// NOTE: an "org"/"project" need must submit as target.org/target.project — the request
// nests them under Target, and the server ignores a bare org (internal/api/scope.go).
const NEED_FIELD = Object.assign(Object.create(null), {
  org: "target.org",
  project: "target.project",
});

function needField(n) {
  const field = n.field || "";
  const name = NEED_FIELD[field] || field;
  const candidates = n.candidates || [];
  const control = candidates.length
    ? html`<select name="${name}">${raw(candidates.map(function (c) {
        return html`<option value="${c}">${c}</option>`;
      }).join(""))}</select>`
    : html`<input name="${name}" value="" />`;
  return html`<div class="ya-row">
    <span>${field}</span>
    ${raw(control)}
    <span class="ya-muted">${n.reason || ""}</span>
  </div>`;
}

// subjectOf names what a preview acts on. close_period's preview is a bare Snapshot
// carrying no period id, so the subject comes from the captured tool input.
function subjectOf(d) {
  const p = d.preview || {};
  const name = p.name || (p.wallet || {}).name || (p.category || {}).name;
  if (name) return ": " + name;
  const from = typeof capturedArgs === "function" ? capturedArgs() : {};
  return from.period ? ": " + from.period : "";
}

function needsForm(d, action, tool, title) {
  const rows = needsList(d).map(needField);
  const edit = action("edit", tool, { confirm: false });
  return html`<div class="ya-root">
    <div class="ya-kpi-value">${title}</div>
    <form class="ya-card">
      ${raw(rows.join(""))}
      <div><button class="ya-action" type="button" data-action="${edit}">Continue</button></div>
    </form>
  </div>`;
}

function walletEntity(w) {
  const e = w || {};
  return html`<div class="ya-card ya-kpis">
    ${raw(kpi("Wallet", e.name || "—"))}
    ${raw(kpi("Kind", e.kind || "—"))}
    ${raw(kpi("Balance", money(e.balance)))}
  </div>`;
}

function txEntity(t) {
  return html`<div class="ya-card">${raw(txRow(t || {}))}</div>`;
}

function periodEntity(p) {
  return html`<div class="ya-card">${raw(periodRow(p || {}))}</div>`;
}

function categoryEntity(c) {
  const e = c || {};
  return html`<div class="ya-card ya-kpis">
    ${raw(kpi("Category", e.name || "—"))}
    ${raw(kpi("Kind", e.kind || "—"))}
  </div>`;
}

function entityMutationView(opts) {
  return function (d, action) {
    const phase = phaseOf(d);

    if (phase === "needs") {
      return needsForm(d, action, opts.tool, opts.title);
    }

    if (phase === "preview") {
      const commit = action("commit", opts.tool, { confirm: true });
      const edit = action("edit", opts.tool, { confirm: false });
      return html`<div class="ya-root">
        <div>
          <div class="ya-kpi-value">${opts.title}${subjectOf(d)}</div>
          <div class="ya-muted">${opts.previewNote}</div>
        </div>
        ${raw(opts.preview(d.preview))}
        ${raw(d.warning ? html`<p class="ya-muted">${d.warning}</p>` : "")}
        <div>
          <button class="ya-action" type="button" data-action="${commit}">${opts.commitLabel}</button>
          <button class="ya-action" type="button" data-action="${edit}">Edit</button>
        </div>
      </div>`;
    }

    if (phase === "result") {
      return html`<div class="ya-root">
        <div class="ya-kpi-value">${opts.resultLabel}</div>
        ${raw(opts.result(d.result))}
        ${raw(d.warning ? html`<p class="ya-muted">${d.warning}</p>` : "")}
      </div>`;
    }

    return html`<div class="ya-root"><p class="ya-muted">${d.warning || "Nothing to do."}</p></div>`;
  };
}

function mutation(tool, title, previewNote, commitLabel, resultLabel, preview, result) {
  registerView(tool, entityMutationView({
    tool: tool,
    title: title,
    previewNote: previewNote,
    commitLabel: commitLabel,
    resultLabel: resultLabel,
    preview: preview,
    result: result || preview,
  }));
}

mutation("create_wallet", "New wallet", "nothing is saved yet", "Create", "Wallet created", walletEntity);
mutation("update_wallet", "Update wallet", "nothing is saved yet", "Save", "Wallet updated", walletEntity);
mutation("archive_wallet", "Archive wallet", "this wallet will be archived", "Archive", "Wallet archived", walletEntity);
mutation("adjust_balance", "Adjust balance", "nothing is saved yet", "Adjust", "Balance adjusted", txEntity);
mutation("record_expense", "Record expense", "nothing is saved yet", "Save", "Expense recorded", txEntity);
mutation("record_income", "Record income", "nothing is saved yet", "Save", "Income recorded", txEntity);
mutation("record_transfer", "Record transfer", "nothing is saved yet", "Save", "Transfer recorded", txEntity);
mutation("revise_tx", "Revise transaction", "nothing is saved yet", "Save", "Transaction revised", txEntity);
mutation("delete_tx", "Delete transaction", "this transaction will be deleted", "Delete", "Transaction deleted", txEntity);
mutation("reopen_period", "Reopen period", "nothing is saved yet", "Reopen", "Period reopened", periodEntity);
mutation("create_category", "New category", "nothing is saved yet", "Create", "Category created", categoryEntity);
// NOTE: close_period's preview is a Snapshot and its result a Period — different types, so no diff is possible.
mutation("close_period", "Close period", "recomputed at close time", "Close", "Period closed",
  function (s) { return snapshotKpis(s); },
  function (p) { return periodEntity(p); });
