function batchCard(b) {
  return html`<div class="app-card">
    <div class="app-kpi-label">${b.label}</div>
    ${b.rows.map((r) => r.failed
      ? html`<div class="app-row">
          <span class="app-muted">${r.row}</span>
          <span class="app-error">${r.code}</span>
          <span class="app-error">${r.error}</span>
        </div>`
      : html`<div class="app-row">
          <span class="app-muted">${r.row}</span>
          ${txRow(r.tx)}
        </div>`)}
  </div>`;
}

class RecordBatchView extends YasakuView {
  render() {
    const m = this.model;
    if (!m) return nothing;
    if (m.phase === "needs") return this.needsForm(m);
    if (m.phase === "result") {
      return html`<div class="app-stack"><div class="app-kpi-value">${m.title}</div>${batchCard(m.batch)}</div>`;
    }
    if (m.phase === "preview") {
      return html`<div class="app-stack">
        ${heading(m.title, m.note)}
        ${batchCard(m.batch)}
        <div>${this.button(m.commit, "Save batch")}</div>
      </div>`;
    }
    return html`<p class="app-muted">${m.message}</p>`;
  }
}

class SeedCategoriesView extends YasakuView {
  render() {
    const m = this.model;
    if (!m) return nothing;
    if (m.phase === "needs") return this.needsForm(m);
    if (m.phase === "result") return heading(m.title, m.note);
    if (m.phase === "preview") {
      return html`<div class="app-stack">
        ${heading(m.title, m.note)}
        <div>${this.button(m.commit, "Add them")}</div>
      </div>`;
    }
    return html`<p class="app-muted">${m.message}</p>`;
  }
}

customElements.define("yasaku-record-batch", RecordBatchView);
customElements.define("yasaku-seed-categories", SeedCategoriesView);

registerView("record_batch", recordBatchModel, (m) => html`<yasaku-record-batch .model=${m}></yasaku-record-batch>`);
registerView("seed_default_categories", seedCategoriesModel, (m) => html`<yasaku-seed-categories .model=${m}></yasaku-seed-categories>`);
