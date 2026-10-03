function entityBlock(e) {
  if (!e) return nothing;
  if (e.kind === "tx") return html`<div class="app-card">${txRow(e.tx)}</div>`;
  if (e.kind === "period") return html`<div class="app-card">${periodRow(e.period)}</div>`;
  return kpiCard(e.kpis);
}

class MutationView extends YasakuView {
  render() {
    const m = this.model;
    if (!m) return nothing;
    if (m.phase === "needs") return this.needsForm(m);
    if (m.phase === "preview") {
      return html`<div class="app-stack">
        ${heading(m.title, m.note)}
        ${entityBlock(m.entity)}
        ${m.warning ? html`<p class="app-muted">${m.warning}</p>` : nothing}
        <div class="app-buttons">${this.button(m.commit, m.commitLabel)} ${this.button(m.edit, "Edit")}</div>
      </div>`;
    }
    if (m.phase === "result") {
      return html`<div class="app-stack">
        <div class="app-kpi-value">${m.title}</div>
        ${entityBlock(m.entity)}
        ${m.warning ? html`<p class="app-muted">${m.warning}</p>` : nothing}
      </div>`;
    }
    return html`<p class="app-muted">${m.message}</p>`;
  }
}

customElements.define("yasaku-mutation", MutationView);

function mutationTemplate(m) {
  return html`<yasaku-mutation .model=${m}></yasaku-mutation>`;
}

registerView("create_wallet", MUTATION_MODELS.create_wallet, mutationTemplate);
registerView("update_wallet", MUTATION_MODELS.update_wallet, mutationTemplate);
registerView("archive_wallet", MUTATION_MODELS.archive_wallet, mutationTemplate);
registerView("adjust_balance", MUTATION_MODELS.adjust_balance, mutationTemplate);
registerView("record_expense", MUTATION_MODELS.record_expense, mutationTemplate);
registerView("record_income", MUTATION_MODELS.record_income, mutationTemplate);
registerView("record_transfer", MUTATION_MODELS.record_transfer, mutationTemplate);
registerView("revise_tx", MUTATION_MODELS.revise_tx, mutationTemplate);
registerView("delete_tx", MUTATION_MODELS.delete_tx, mutationTemplate);
registerView("reopen_period", MUTATION_MODELS.reopen_period, mutationTemplate);
registerView("create_category", MUTATION_MODELS.create_category, mutationTemplate);
registerView("close_period", MUTATION_MODELS.close_period, mutationTemplate);
