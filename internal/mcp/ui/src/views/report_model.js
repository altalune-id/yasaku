function categoryShareModel(s) {
  const cat = s.category && typeof s.category === "object" ? s.category : {};
  return {
    swatch: swatchColour(cat.color),
    name: text(cat.name, "Uncategorized"),
    width: pct(s.share),
    amount: money(s.amount),
    share: pct(s.share),
  };
}

function periodReportModel(d, action) {
  const p = d.period && typeof d.period === "object" ? d.period : {};
  const spend = Array.isArray(d.spendByCategory) ? d.spendByCategory : [];
  return {
    name: text(p.name, "Period"),
    sub: dateRange(p.startDate, p.endDate) + " · " + text(p.status, "open"),
    kpis: snapshotKpis(d),
    spendEmpty: spend.length === 0,
    donut: donutModel(spend),
    categories: spend.map(categoryShareModel),
    refresh: action("refresh", "period_report", { period: text(p.id, "") }),
  };
}

function cashflowReportModel(d) {
  const points = Array.isArray(d.points) ? d.points : [];
  return {
    empty: points.length === 0,
    label: "Cashflow, last " + num(points.length) + " periods",
    chart: cashflowModel(points),
    rows: points.map(function (p) {
      return {
        name: text((p.period || {}).name, "—"),
        income: "in " + money(p.income),
        expense: "out " + money(p.expense),
        net: money(p.net),
      };
    }),
  };
}

function previewCloseModel(d, action) {
  const p = d.period && typeof d.period === "object" ? d.period : {};
  const s = d.snapshot && typeof d.snapshot === "object" ? d.snapshot : {};
  return {
    title: "Close " + text(p.name, "period") + "?",
    sub: dateRange(p.startDate, p.endDate) + " · nothing is saved yet",
    kpis: snapshotKpis(s),
    wallets: (Array.isArray(s.wallets) ? s.wallets : []).map(function (w) {
      return { name: text((w.wallet || {}).name, "—"), closing: money(w.closing) };
    }),
    close: action("close-period", "close_period", { period: text(p.id, ""), confirm: false }),
  };
}
