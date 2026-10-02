function listCard(label, m, empty, row) {
  if (!m || m.empty) return html`<p class="app-muted">${empty}</p>`;
  return html`<div class="app-card">
    <div class="app-kpi-label">${label}</div>
    ${m.rows.map(row)}
  </div>`;
}

class CategoryListView extends YasakuView {
  render() {
    return listCard("Categories", this.model, "No categories yet.", (r) => html`<div class="app-row">
      <span class="app-swatch" style="background:${r.swatch}"></span>
      <span>${r.name}</span>
      <span class="app-muted">${r.kind}</span>
    </div>`);
  }
}

class PeriodListView extends YasakuView {
  render() {
    return listCard("Periods", this.model, "No periods yet.", periodRow);
  }
}

class ProjectListView extends YasakuView {
  render() {
    return listCard("Projects", this.model, "No projects reachable.", (r) => html`<div class="app-row">
      <span>${r.name}</span>
      <span class="app-muted">${r.org}</span>
    </div>`);
  }
}

class CurrentPeriodView extends YasakuView {
  render() {
    const m = this.model;
    if (!m) return nothing;
    return html`<div class="app-stack">
      ${heading(m.name, m.sub)}
      ${kpiCard(m.kpis)}
    </div>`;
  }
}

class NowView extends YasakuView {
  render() {
    const m = this.model;
    if (!m) return nothing;
    return html`<div class="app-stack">
      ${kpiCard(m.kpis)}
      ${m.hasPeriod ? html`<div class="app-card">${periodRow(m.period)}</div>` : nothing}
    </div>`;
  }
}

customElements.define("yasaku-category-list", CategoryListView);
customElements.define("yasaku-period-list", PeriodListView);
customElements.define("yasaku-project-list", ProjectListView);
customElements.define("yasaku-current-period", CurrentPeriodView);
customElements.define("yasaku-now", NowView);

registerView("list_categories", categoryListModel, (m) => html`<yasaku-category-list .model=${m}></yasaku-category-list>`);
registerView("list_periods", periodListModel, (m) => html`<yasaku-period-list .model=${m}></yasaku-period-list>`);
registerView("list_projects", projectListModel, (m) => html`<yasaku-project-list .model=${m}></yasaku-project-list>`);
registerView("current_period", currentPeriodModel, (m) => html`<yasaku-current-period .model=${m}></yasaku-current-period>`);
registerView("now", nowModel, (m) => html`<yasaku-now .model=${m}></yasaku-now>`);
