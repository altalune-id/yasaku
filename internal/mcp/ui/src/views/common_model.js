function kpi(label, value) {
  return { label: label, value: value };
}

// NOTE: null-prototype, so a kind like "constructor" cannot resolve to an inherited value.
const TX_SIGN = Object.assign(Object.create(null), {
  expense: "−", income: "+", transfer: "⇄", opening: "•", adjustment_in: "+", adjustment_out: "−",
});

function txRowModel(t) {
  const x = t && typeof t === "object" ? t : {};
  const wallet = text((x.wallet || {}).name, "");
  const to = text((x.toWallet || {}).name, "");
  const cat = text((x.category || {}).name, "");
  const note = text(x.note, "");
  const kind = text(x.kind, "");
  const where = to ? wallet + " → " + to : wallet;
  return {
    sign: TX_SIGN[kind] || "•",
    label: note || cat || kind,
    sub: note && cat ? cat + " · " + where : where,
    date: civilDay(x.date),
    title: recordedTitle(x.occurredAt),
    amount: money(x.amount),
  };
}

function periodRowModel(p) {
  const x = p && typeof p === "object" ? p : {};
  return {
    name: text(x.name, "—"),
    range: dateRange(x.startDate, x.endDate),
    status: text(x.status, "open"),
    closedAt: x.closedAt ? dateTime(x.closedAt) : "",
  };
}

function snapshotKpis(s) {
  const snap = s && typeof s === "object" ? s : {};
  return [
    kpi("Income", money(snap.income)),
    kpi("Expense", money(snap.expense)),
    kpi("Net", money(snap.net)),
    kpi("Transactions", num(snap.txCount || 0)),
  ];
}
