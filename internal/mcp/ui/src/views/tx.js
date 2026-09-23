// views/tx.js
// NOTE: null-prototype, so a kind like "constructor" cannot resolve to an inherited value.
const TX_SIGN = Object.assign(Object.create(null), {
  expense: "−", income: "+", transfer: "⇄", opening: "•", adjustment_in: "+", adjustment_out: "−",
});

// NOTE: prefer the civil date; occurredAt is normalised to UTC by protojson and
// reads a day early for any project east of Greenwich.
function txDate(t) {
  if (t && t.date) return String(t.date);
  const ts = t && t.occurredAt;
  if (!ts) return "";
  return String(ts).slice(0, 10);
}

function txRow(t) {
  const wallet = (t.wallet || {}).name || "";
  const to = (t.toWallet || {}).name || "";
  const cat = (t.category || {}).name || "";
  const where = to ? wallet + " → " + to : wallet;
  const label = t.note || cat || t.kind || "";
  const sub = t.note && cat ? cat + " · " + where : where;
  return html`<div class="ya-row">
    <span class="ya-muted">${TX_SIGN[t.kind] || "•"}</span>
    <span>${label}</span>
    <span class="ya-muted">${sub}</span>
    <span class="ya-muted">${txDate(t)}</span>
    <span class="ya-num">${money(t.amount)}</span>
  </div>`;
}

function txListView(d, action) {
  const rows = d.transactions || [];
  if (!rows.length) {
    return html`<div class="ya-root"><p class="ya-muted">No transactions yet.</p></div>`;
  }
  const totals = d.totalIn || d.totalOut
    ? html`<div class="ya-card ya-kpis">
        ${raw(kpi("In", money(d.totalIn)))}
        ${raw(kpi("Out", money(d.totalOut)))}
        ${raw(kpi("Rows", num(rows.length)))}
      </div>`
    : "";
  const more = d.nextCursor
    ? html`<div><button class="ya-action" type="button" data-action="${action("more", "list_recent_tx", { cursor: d.nextCursor })}">Load more</button></div>`
    : "";
  return html`<div class="ya-root">
    ${raw(totals)}
    <div class="ya-card">${raw(rows.map(txRow).join(""))}</div>
    ${raw(more)}
  </div>`;
}

registerView("list_recent_tx", txListView);
registerView("search_tx", txListView);
