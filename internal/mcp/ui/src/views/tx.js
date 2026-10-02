class TxListView extends YasakuView {
  render() {
    const m = this.model;
    if (!m || m.empty) return html`<p class="app-muted">No transactions yet.</p>`;
    return html`<div class="app-stack">
      ${m.totals ? kpiCard(m.kpis) : nothing}
      <div class="app-card">${m.rows.map(txRow)}</div>
      ${m.more ? html`<div>${this.button(m.more, "Load more")}</div>` : nothing}
    </div>`;
  }
}

customElements.define("yasaku-tx-list", TxListView);

registerView("list_recent_tx", TX_LIST_MODELS.list_recent_tx, (m) => html`<yasaku-tx-list .model=${m}></yasaku-tx-list>`);
registerView("search_tx", TX_LIST_MODELS.search_tx, (m) => html`<yasaku-tx-list .model=${m}></yasaku-tx-list>`);
