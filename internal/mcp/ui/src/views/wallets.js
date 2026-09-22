// views/wallets.js
function walletRow(w) {
  const meta = [w.kind, w.provider].filter(Boolean).join(" · ");
  const excluded = w.excludeFromTotal ? html`<span class="ya-muted">excluded</span>` : "";
  const archived = w.archived ? html`<span class="ya-muted">archived</span>` : "";
  return html`<div class="ya-row">
    <span>${w.name || "—"}</span>
    <span class="ya-muted">${meta}</span>
    ${raw(excluded)}
    ${raw(archived)}
    <span class="ya-num">${money(w.balance)}</span>
  </div>`;
}

function renderWalletList(d) {
  const wallets = d.wallets || [];
  if (!wallets.length) {
    return html`<div class="ya-root"><p class="ya-muted">No wallets yet.</p></div>`;
  }
  return html`<div class="ya-root">
    <div class="ya-card">
      <div class="ya-kpi-label">Wallets</div>
      ${raw(wallets.map(walletRow).join(""))}
    </div>
  </div>`;
}

function renderWalletDetail(d) {
  const w = d.wallet || {};
  const recent = d.recent || [];
  const meta = [w.kind, w.provider, w.currency].filter(Boolean).join(" · ");
  const rows = recent.length
    ? html`<div class="ya-card">
        <div class="ya-kpi-label">Recent</div>
        ${raw(recent.map(txRow).join(""))}
      </div>`
    : html`<p class="ya-muted">No transactions in this wallet yet.</p>`;
  return html`<div class="ya-root">
    <div>
      <div class="ya-kpi-value">${w.name || "Wallet"}</div>
      <div class="ya-muted">${meta}</div>
    </div>
    <div class="ya-card ya-kpis">
      ${raw(kpi("Balance", money(w.balance)))}
      ${raw(kpi("Excluded", w.excludeFromTotal ? "yes" : "no"))}
    </div>
    ${raw(rows)}
  </div>`;
}

function renderWalletTotals(d) {
  const p = d.period || {};
  const lines = (d.wallets || []).map(function (l) {
    const excluded = l.excludeFromTotal ? html`<span class="ya-muted">excluded</span>` : "";
    return html`<div class="ya-row">
      <span>${(l.wallet || {}).name || "—"}</span>
      <span class="ya-muted">${l.kind || ""}</span>
      ${raw(excluded)}
      <span class="ya-num">${money(l.closing)}</span>
    </div>`;
  });
  const period = p.name
    ? html`<div class="ya-muted">${p.name} · ${dateRange(p.startDate, p.endDate)}</div>`
    : "";
  return html`<div class="ya-root">
    <div>
      <div class="ya-kpi-value">Wallet totals</div>
      ${raw(period)}
    </div>
    <div class="ya-card ya-kpis">
      ${raw(kpi("Spendable", money(d.spendableTotal)))}
      ${raw(kpi("Total", money(d.total)))}
    </div>
    <div class="ya-card ya-kpis">
      ${raw(kpi("Income", money(d.income)))}
      ${raw(kpi("Expense", money(d.expense)))}
      ${raw(kpi("Net", money(d.net)))}
    </div>
    ${raw(lines.length ? html`<div class="ya-card">${raw(lines.join(""))}</div>` : "")}
  </div>`;
}

registerView("list_wallets", renderWalletList);
registerView("get_wallet", renderWalletDetail);
registerView("wallet_totals", renderWalletTotals);
