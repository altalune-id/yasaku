function categoryListModel(d) {
  const rows = (Array.isArray(d.categories) ? d.categories : []).map(function (c) {
    return { swatch: swatchColour(c.color), name: text(c.name, "—"), kind: text(c.kind, "") };
  });
  return { empty: rows.length === 0, rows: rows };
}

function periodListModel(d) {
  const rows = (Array.isArray(d.periods) ? d.periods : []).map(periodRowModel);
  return { empty: rows.length === 0, rows: rows };
}

function projectListModel(d) {
  const rows = (Array.isArray(d.projects) ? d.projects : []).map(function (p) {
    return {
      name: text(p.projectName, text(p.project, "—")),
      org: text(p.orgName, text(p.org, "")),
    };
  });
  return { empty: rows.length === 0, rows: rows };
}

function currentPeriodModel(d) {
  const p = d.period && typeof d.period === "object" ? d.period : {};
  return {
    name: text(p.name, "Current period"),
    sub: dateRange(p.startDate, p.endDate) + " · " + text(p.status, "open"),
    kpis: snapshotKpis(d.running),
  };
}

function nowModel(d) {
  const p = d.currentPeriod && typeof d.currentPeriod === "object" ? d.currentPeriod : {};
  return {
    kpis: [kpi("Today", text(d.today, "—")), kpi("Timezone", text(d.timezone, "—"))],
    hasPeriod: !!text(p.name, ""),
    period: periodRowModel(p),
  };
}
