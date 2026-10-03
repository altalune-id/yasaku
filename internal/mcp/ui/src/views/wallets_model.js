function walletRowModel(w) {
  return {
    name: text(w.name, "—"),
    meta: [text(w.kind, ""), text(w.provider, "")].filter(Boolean).join(" · "),
    excluded: !!w.excludeFromTotal,
    archived: !!w.archived,
    balance: money(w.balance),
  };
}

function walletListModel(d) {
  const wallets = Array.isArray(d.wallets) ? d.wallets : [];
  return { empty: wallets.length === 0, rows: wallets.map(walletRowModel) };
}

function walletDetailModel(d) {
  const w = d.wallet && typeof d.wallet === "object" ? d.wallet : {};
  const recent = Array.isArray(d.recent) ? d.recent : [];
  return {
    name: text(w.name, "Wallet"),
    meta: [text(w.kind, ""), text(w.provider, ""), text(w.currency, "")].filter(Boolean).join(" · "),
    kpis: [kpi("Balance", money(w.balance)), kpi("Excluded", w.excludeFromTotal ? "yes" : "no")],
    recent: recent.map(txRowModel),
  };
}

function walletTotalsModel(d) {
  const p = d.period && typeof d.period === "object" ? d.period : {};
  const name = text(p.name, "");
  return {
    period: name ? name + " · " + dateRange(p.startDate, p.endDate) : "",
    totals: [kpi("Spendable", money(d.spendableTotal)), kpi("Total", money(d.total))],
    flows: [kpi("Income", money(d.income)), kpi("Expense", money(d.expense)), kpi("Net", money(d.net))],
    rows: (Array.isArray(d.wallets) ? d.wallets : []).map(function (l) {
      return {
        name: text((l.wallet || {}).name, "—"),
        kind: text(l.kind, ""),
        excluded: !!l.excludeFromTotal,
        closing: money(l.closing),
      };
    }),
  };
}
