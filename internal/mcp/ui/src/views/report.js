class PeriodReportView extends YasakuView {
  render() {
    const m = this.model;
    if (!m) return nothing;
    return html`<div class="app-stack">
      ${heading(m.name, m.sub)}
      ${kpiCard(m.kpis)}
      ${m.spendEmpty
        ? html`<p class="app-muted">No spending recorded in this period.</p>`
        : html`<div class="app-card">
            <div class="app-kpi-label">Spend by category</div>
            ${donutChart(m.donut)}
            ${m.categories.map((c) => html`<div class="app-row">
              <span class="app-swatch" style="background:${c.swatch}"></span>
              <span>${c.name}</span>
              <span class="app-bar"><span style="width:${c.width};background:${c.swatch}"></span></span>
              <span class="app-num">${c.amount}</span>
              <span class="app-muted app-num">${c.share}</span>
            </div>`)}
          </div>`}
      <div>${this.button(m.refresh, "Refresh")}</div>
    </div>`;
  }
}

class CashflowReportView extends YasakuView {
  render() {
    const m = this.model;
    if (!m || m.empty) return html`<p class="app-muted">No periods to plot yet.</p>`;
    return html`<div class="app-stack">
      <div class="app-card">
        <div class="app-kpi-label">${m.label}</div>
        ${cashflowChart(m.chart)}
      </div>
      <div class="app-card">${m.rows.map((r) => html`<div class="app-row">
        <span>${r.name}</span>
        <span class="app-muted app-num">${r.income}</span>
        <span class="app-muted app-num">${r.expense}</span>
        <span class="app-num">${r.net}</span>
      </div>`)}</div>
    </div>`;
  }
}

class PreviewCloseView extends YasakuView {
  render() {
    const m = this.model;
    if (!m) return nothing;
    return html`<div class="app-stack">
      ${heading(m.title, m.sub)}
      ${kpiCard(m.kpis)}
      ${m.wallets.length
        ? html`<div class="app-card">
            <div class="app-kpi-label">Closing balances</div>
            ${m.wallets.map((w) => html`<div class="app-row"><span>${w.name}</span><span class="app-num">${w.closing}</span></div>`)}
          </div>`
        : nothing}
      <div>${this.button(m.close, "Preview close")}</div>
    </div>`;
  }
}

customElements.define("yasaku-period-report", PeriodReportView);
customElements.define("yasaku-cashflow-report", CashflowReportView);
customElements.define("yasaku-preview-close", PreviewCloseView);

registerView("period_report", periodReportModel, (m) => html`<yasaku-period-report .model=${m}></yasaku-period-report>`);
registerView("cashflow_report", cashflowReportModel, (m) => html`<yasaku-cashflow-report .model=${m}></yasaku-cashflow-report>`);
registerView("preview_close", previewCloseModel, (m) => html`<yasaku-preview-close .model=${m}></yasaku-preview-close>`);
