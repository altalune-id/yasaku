// views/bulk.js
// NOTE: BatchOutcome.index is omitted for row 0, and a refused row may carry error with no errorCode.
function batchRow(o, i) {
  const idx = (o.index === undefined ? i : o.index) + 1;
  const failed = o.error || o.errorCode;
  if (failed) {
    return html`<div class="ya-row">
      <span class="ya-muted">Row ${num(idx)}</span>
      <span class="ya-error">${o.errorCode || "refused"}</span>
      <span class="ya-error">${o.error || ""}</span>
    </div>`;
  }
  return html`<div class="ya-row">
    <span class="ya-muted">Row ${num(idx)}</span>
    ${raw(txRow(o.transaction || {}))}
  </div>`;
}

function batchCard(maybeRows) {
  const rows = Array.isArray(maybeRows) ? maybeRows : [];
  const clean = rows.filter(function (o) { return !(o.error || o.errorCode); }).length;
  return html`<div class="ya-card">
    <div class="ya-kpi-label">${num(clean)} of ${num(rows.length)} rows are writable</div>
    ${raw(rows.map(batchRow).join(""))}
  </div>`;
}

registerView("record_batch", function (d, action) {
  const phase = phaseOf(d);
  if (phase === "needs") {
    return needsForm(d, action, "record_batch", "Record batch");
  }
  if (phase === "result") {
    return html`<div class="ya-root">
      <div class="ya-kpi-value">Batch recorded</div>
      ${raw(batchCard(d.results || []))}
    </div>`;
  }
  if (phase === "preview") {
    const commit = action("commit", "record_batch", { confirm: true });
    return html`<div class="ya-root">
      <div>
        <div class="ya-kpi-value">Record batch</div>
        <div class="ya-muted">nothing is saved yet</div>
      </div>
      ${raw(batchCard(d.preview || []))}
      <div><button class="ya-action" type="button" data-action="${commit}">Save batch</button></div>
    </div>`;
  }
  return html`<div class="ya-root"><p class="ya-muted">${d.warning || "Nothing to record."}</p></div>`;
});

// NOTE: at zero, previewed and committed are byte-identical {} on the wire — they cannot be told apart.
registerView("seed_default_categories", function (d, action) {
  if (needsList(d).length > 0) {
    return needsForm(d, action, "seed_default_categories", "Seed default categories");
  }
  if (d.inserted !== undefined) {
    return html`<div class="ya-root">
      <div class="ya-kpi-value">Defaults added</div>
      <div class="ya-muted">${num(d.inserted)} categories inserted.</div>
    </div>`;
  }
  if (d.previewCount !== undefined) {
    const commit = action("commit", "seed_default_categories", { confirm: true });
    return html`<div class="ya-root">
      <div class="ya-kpi-value">Seed default categories</div>
      <div class="ya-muted">${num(d.previewCount)} would be added. Nothing is saved yet.</div>
      <div><button class="ya-action" type="button" data-action="${commit}">Add them</button></div>
    </div>`;
  }
  return html`<div class="ya-root"><p class="ya-muted">${d.warning || "Nothing to do — the project already has every default."}</p></div>`;
});
