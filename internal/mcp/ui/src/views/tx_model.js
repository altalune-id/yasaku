// NOTE: "Load more" re-runs the tool that painted the list; boot.js merges that tool's own criteria under the declared cursor.
function txListModelFor(tool) {
  return function (d, action) {
    const rows = Array.isArray(d.transactions) ? d.transactions : [];
    const cursor = text(d.nextCursor, "");
    return {
      empty: rows.length === 0,
      totals: !!(d.totalIn || d.totalOut),
      kpis: [kpi("In", money(d.totalIn)), kpi("Out", money(d.totalOut)), kpi("Rows", num(rows.length))],
      rows: rows.map(txRowModel),
      more: cursor ? action("more", tool, { cursor: cursor }) : "",
    };
  };
}

const TX_LIST_MODELS = Object.assign(Object.create(null), {
  list_recent_tx: txListModelFor("list_recent_tx"),
  search_tx: txListModelFor("search_tx"),
});
