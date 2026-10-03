class YasakuView extends LitElement {
  static properties = { model: { attribute: false } };
  static styles = [appStyles, yasakuStyles];

  fire(id, ev) {
    const el = ev.currentTarget;
    this.dispatchEvent(new CustomEvent("yasaku-action", {
      bubbles: true,
      composed: true,
      detail: { id: id, el: el, form: el.closest("form") },
    }));
  }

  // NOTE: submit is not composed, so the shell never sees it; a form handles its own Enter key or the iframe navigates.
  submit(id, ev) {
    ev.preventDefault();
    const form = ev.currentTarget;
    const el = ev.submitter || form.querySelector("button") || form;
    this.dispatchEvent(new CustomEvent("yasaku-action", {
      bubbles: true,
      composed: true,
      detail: { id: id, el: el, form: form },
    }));
  }

  button(id, label) {
    if (!id) return nothing;
    return html`<button class="app-action" type="button" @click=${(ev) => this.fire(id, ev)}>${label}</button>`;
  }

  needsForm(m) {
    return html`<div class="app-stack">
      <div class="app-kpi-value">${m.title}</div>
      <form class="app-card" @submit=${(ev) => this.submit(m.edit, ev)}>
        ${m.fields.map((f) => html`<div class="app-row">
          <span>${f.label}</span>
          ${f.select
            ? html`<select name=${f.name}>${f.options.map((o) => html`<option value=${o}>${o}</option>`)}</select>`
            : html`<input name=${f.name} value="" />`}
          <span class="app-muted">${f.reason}</span>
        </div>`)}
        <div>${this.button(m.edit, "Continue")}</div>
      </form>
    </div>`;
  }
}

function kpiCard(kpis) {
  return html`<div class="app-card app-kpis">
    ${kpis.map((k) => html`<div>
      <div class="app-kpi-label">${k.label}</div>
      <div class="app-kpi-value app-num">${k.value}</div>
    </div>`)}
  </div>`;
}

function txRow(r) {
  return html`<div class="app-row">
    <span class="app-muted">${r.sign}</span>
    <span>${r.label}</span>
    <span class="app-muted">${r.sub}</span>
    <span class="app-muted" title=${r.title || nothing}>${r.date}</span>
    <span class="app-num">${r.amount}</span>
  </div>`;
}

function periodRow(r) {
  return html`<div class="app-row">
    <span>${r.name}</span>
    <span class="app-muted">${r.range}</span>
    <span class="app-muted">${r.closedAt ? r.status + " · " + r.closedAt : r.status}</span>
  </div>`;
}

function heading(title, sub) {
  return html`<div>
    <div class="app-kpi-value">${title}</div>
    ${sub ? html`<div class="app-muted">${sub}</div>` : nothing}
  </div>`;
}
