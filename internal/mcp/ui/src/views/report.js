// views/report.js
function kpi(label, value) {
  return html`<div><div class="ya-kpi-label">${label}</div><div class="ya-kpi-value ya-num">${value}</div></div>`;
}

function categoryRows(slices) {
  const rows = (slices || []).map(function (s) {
    const cat = s.category || {};
    const colour = swatchColour(cat.color);
    return html`<div class="ya-row">
      <span class="ya-swatch" style="background:${colour}"></span>
      <span>${cat.name || "Uncategorized"}</span>
      <span class="ya-bar"><span style="width:${pct(s.share)};background:${colour}"></span></span>
      <span class="ya-num">${money(s.amount)}</span>
      <span class="ya-muted ya-num">${pct(s.share)}</span>
    </div>`;
  });
  return raw(rows.join(""));
}

registerView("period_report", function (d, action) {
  const p = d.period || {};
  const spend = d.spendByCategory || [];
  const refresh = action("refresh", "period_report", { period: p.id });

  return html`<div class="ya-root">
    <div>
      <div class="ya-kpi-value">${p.name || "Period"}</div>
      <div class="ya-muted">${dateRange(p.startDate, p.endDate)} · ${p.status || "open"}</div>
    </div>
    <div class="ya-card ya-kpis">
      ${raw(kpi("Income", money(d.income)))}
      ${raw(kpi("Expense", money(d.expense)))}
      ${raw(kpi("Net", money(d.net)))}
      ${raw(kpi("Transactions", num(d.txCount || 0)))}
    </div>
    ${raw(spend.length ? html`<div class="ya-card">
      <div class="ya-kpi-label">Spend by category</div>
      ${raw(donutSVG(spend))}
      ${categoryRows(spend)}
    </div>` : html`<p class="ya-muted">No spending recorded in this period.</p>`)}
    <div><button class="ya-action" data-action="${refresh}">Refresh</button></div>
  </div>`;
});

registerView("cashflow_report", function (d) {
  const points = d.points || [];
  if (!points.length) {
    return html`<div class="ya-root"><p class="ya-muted">No periods to plot yet.</p></div>`;
  }
  const rows = points.map(function (p) {
    return html`<div class="ya-row">
      <span>${(p.period || {}).name || "—"}</span>
      <span class="ya-muted ya-num">in ${money(p.income)}</span>
      <span class="ya-muted ya-num">out ${money(p.expense)}</span>
      <span class="ya-num">${money(p.net)}</span>
    </div>`;
  });
  return html`<div class="ya-root">
    <div class="ya-card">
      <div class="ya-kpi-label">Cashflow, last ${num(points.length)} periods</div>
      ${raw(cashflowSVG(points))}
    </div>
    <div class="ya-card">${raw(rows.join(""))}</div>
  </div>`;
});

registerView("preview_close", function (d, action) {
  const p = d.period || {};
  const s = d.snapshot || {};
  const wallets = (s.wallets || []).map(function (w) {
    return html`<div class="ya-row"><span>${(w.wallet || {}).name || "—"}</span><span class="ya-num">${money(w.closing)}</span></div>`;
  });
  const close = action("close-period", "close_period", { period: p.id, confirm: false });

  return html`<div class="ya-root">
    <div>
      <div class="ya-kpi-value">Close ${p.name || "period"}?</div>
      <div class="ya-muted">${dateRange(p.startDate, p.endDate)} · nothing is saved yet</div>
    </div>
    <div class="ya-card ya-kpis">
      ${raw(kpi("Income", money(s.income)))}
      ${raw(kpi("Expense", money(s.expense)))}
      ${raw(kpi("Net", money(s.net)))}
      ${raw(kpi("Transactions", num(s.txCount || 0)))}
    </div>
    ${raw(wallets.length ? html`<div class="ya-card"><div class="ya-kpi-label">Closing balances</div>${raw(wallets.join(""))}</div>` : "")}
    <div><button class="ya-action" data-action="${close}">Preview close</button></div>
  </div>`;
});
