class WalletListView extends YasakuView {
  render() {
    const m = this.model;
    if (!m || m.empty) return html`<p class="app-muted">No wallets yet.</p>`;
    return html`<div class="app-card">
      <div class="app-kpi-label">Wallets</div>
      ${m.rows.map((r) => html`<div class="app-row">
        <span>${r.name}</span>
        <span class="app-muted">${r.meta}</span>
        ${r.excluded ? html`<span class="app-muted">excluded</span>` : nothing}
        ${r.archived ? html`<span class="app-muted">archived</span>` : nothing}
        <span class="app-num">${r.balance}</span>
      </div>`)}
    </div>`;
  }
}

class WalletDetailView extends YasakuView {
  render() {
    const m = this.model;
    if (!m) return nothing;
    return html`<div class="app-stack">
      ${heading(m.name, m.meta)}
      ${kpiCard(m.kpis)}
      ${m.recent.length
        ? html`<div class="app-card"><div class="app-kpi-label">Recent</div>${m.recent.map(txRow)}</div>`
        : html`<p class="app-muted">No transactions in this wallet yet.</p>`}
    </div>`;
  }
}

class WalletTotalsView extends YasakuView {
  render() {
    const m = this.model;
    if (!m) return nothing;
    return html`<div class="app-stack">
      ${heading("Wallet totals", m.period)}
      ${kpiCard(m.totals)}
      ${kpiCard(m.flows)}
      ${m.rows.length
        ? html`<div class="app-card">${m.rows.map((r) => html`<div class="app-row">
            <span>${r.name}</span>
            <span class="app-muted">${r.kind}</span>
            ${r.excluded ? html`<span class="app-muted">excluded</span>` : nothing}
            <span class="app-num">${r.closing}</span>
          </div>`)}</div>`
        : nothing}
    </div>`;
  }
}

customElements.define("yasaku-wallet-list", WalletListView);
customElements.define("yasaku-wallet-detail", WalletDetailView);
customElements.define("yasaku-wallet-totals", WalletTotalsView);

registerView("list_wallets", walletListModel, (m) => html`<yasaku-wallet-list .model=${m}></yasaku-wallet-list>`);
registerView("get_wallet", walletDetailModel, (m) => html`<yasaku-wallet-detail .model=${m}></yasaku-wallet-detail>`);
registerView("wallet_totals", walletTotalsModel, (m) => html`<yasaku-wallet-totals .model=${m}></yasaku-wallet-totals>`);
