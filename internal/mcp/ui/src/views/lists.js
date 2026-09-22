// views/lists.js
function periodRow(p) {
  return html`<div class="ya-row">
    <span>${p.name || "—"}</span>
    <span class="ya-muted">${dateRange(p.startDate, p.endDate)}</span>
    <span class="ya-muted">${p.status || "open"}</span>
  </div>`;
}

function snapshotKpis(s) {
  const snap = s || {};
  return html`<div class="ya-card ya-kpis">
    ${raw(kpi("Income", money(snap.income)))}
    ${raw(kpi("Expense", money(snap.expense)))}
    ${raw(kpi("Net", money(snap.net)))}
    ${raw(kpi("Transactions", num(snap.txCount || 0)))}
  </div>`;
}

function listCard(label, rows, empty) {
  if (!rows.length) {
    return html`<div class="ya-root"><p class="ya-muted">${empty}</p></div>`;
  }
  return html`<div class="ya-root">
    <div class="ya-card">
      <div class="ya-kpi-label">${label}</div>
      ${raw(rows.join(""))}
    </div>
  </div>`;
}

function renderCategoryList(d) {
  const rows = (d.categories || []).map(function (c) {
    return html`<div class="ya-row">
      <span class="ya-swatch" style="background:${swatchColour(c.color)}"></span>
      <span>${c.name || "—"}</span>
      <span class="ya-muted">${c.kind || ""}</span>
    </div>`;
  });
  return listCard("Categories", rows, "No categories yet.");
}

function renderPeriodList(d) {
  return listCard("Periods", (d.periods || []).map(periodRow), "No periods yet.");
}

function renderProjectList(d) {
  const rows = (d.projects || []).map(function (p) {
    return html`<div class="ya-row">
      <span>${p.projectName || p.project || "—"}</span>
      <span class="ya-muted">${p.orgName || p.org || ""}</span>
    </div>`;
  });
  return listCard("Projects", rows, "No projects reachable.");
}

function renderCurrentPeriod(d) {
  const p = d.period || {};
  return html`<div class="ya-root">
    <div>
      <div class="ya-kpi-value">${p.name || "Current period"}</div>
      <div class="ya-muted">${dateRange(p.startDate, p.endDate)} · ${p.status || "open"}</div>
    </div>
    ${raw(snapshotKpis(d.running))}
  </div>`;
}

function renderNow(d) {
  const p = d.currentPeriod || {};
  return html`<div class="ya-root">
    <div class="ya-card ya-kpis">
      ${raw(kpi("Today", d.today || "—"))}
      ${raw(kpi("Timezone", d.timezone || "—"))}
    </div>
    ${raw(p.name ? html`<div class="ya-card">${raw(periodRow(p))}</div>` : "")}
  </div>`;
}

registerView("list_categories", renderCategoryList);
registerView("list_periods", renderPeriodList);
registerView("list_projects", renderProjectList);
registerView("current_period", renderCurrentPeriod);
registerView("now", renderNow);
