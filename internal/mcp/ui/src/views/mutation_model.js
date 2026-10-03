// NOTE: an "org"/"project" need must submit as target.org/target.project — the request nests them under Target, and the server ignores a bare org (internal/controlplane/target.go).
const NEED_FIELD = Object.assign(Object.create(null), {
  org: "target.org",
  project: "target.project",
});

function needFieldModel(n) {
  const field = text(n.field, "");
  const candidates = Array.isArray(n.candidates) ? n.candidates : [];
  return {
    label: field,
    name: NEED_FIELD[field] || field,
    reason: text(n.reason, ""),
    select: candidates.length > 0,
    options: candidates.map(function (c) { return text(c, ""); }),
  };
}

function needsModel(d, action, tool, title) {
  return {
    phase: "needs",
    title: title,
    fields: needsList(d).map(needFieldModel),
    edit: action("edit", tool, { confirm: false }),
  };
}

// NOTE: close_period's preview is a bare Snapshot carrying no period id, so the subject comes from the view's own answers.
function subjectOf(d) {
  const p = d.preview && typeof d.preview === "object" ? d.preview : {};
  const name = text(p.name, "") || text((p.wallet || {}).name, "") || text((p.category || {}).name, "");
  if (name) return ": " + name;
  const from = typeof capturedArgs === "function" ? capturedArgs() : {};
  const period = text(from && from.period, "");
  return period ? ": " + period : "";
}

function walletEntity(w) {
  const e = w && typeof w === "object" ? w : {};
  return { kind: "kpis", kpis: [kpi("Wallet", text(e.name, "—")), kpi("Kind", text(e.kind, "—")), kpi("Balance", money(e.balance))] };
}

function txEntity(t) {
  return { kind: "tx", tx: txRowModel(t) };
}

function periodEntity(p) {
  return { kind: "period", period: periodRowModel(p) };
}

function categoryEntity(c) {
  const e = c && typeof c === "object" ? c : {};
  return { kind: "kpis", kpis: [kpi("Category", text(e.name, "—")), kpi("Kind", text(e.kind, "—"))] };
}

function snapshotEntity(s) {
  return { kind: "kpis", kpis: snapshotKpis(s) };
}

function entityMutationModel(opts) {
  return function (d, action) {
    const phase = phaseOf(d);
    const warning = text(d.warning, "");
    if (phase === "needs") return needsModel(d, action, opts.tool, opts.title);
    if (phase === "preview") {
      return {
        phase: "preview",
        title: opts.title + subjectOf(d),
        note: opts.previewNote,
        entity: opts.preview(d.preview),
        warning: warning,
        commitLabel: opts.commitLabel,
        commit: action("commit", opts.tool, { confirm: true }),
        edit: action("edit", opts.tool, { confirm: false }),
      };
    }
    if (phase === "result") {
      return { phase: "result", title: opts.resultLabel, entity: opts.result(d.result), warning: warning };
    }
    return { phase: "empty", message: warning || "Nothing to do." };
  };
}

function mutationModel(tool, title, previewNote, commitLabel, resultLabel, preview, result) {
  return entityMutationModel({
    tool: tool,
    title: title,
    previewNote: previewNote,
    commitLabel: commitLabel,
    resultLabel: resultLabel,
    preview: preview,
    result: result || preview,
  });
}

const MUTATION_MODELS = Object.assign(Object.create(null), {
  create_wallet: mutationModel("create_wallet", "New wallet", "nothing is saved yet", "Create", "Wallet created", walletEntity),
  update_wallet: mutationModel("update_wallet", "Update wallet", "nothing is saved yet", "Save", "Wallet updated", walletEntity),
  archive_wallet: mutationModel("archive_wallet", "Archive wallet", "this wallet will be archived", "Archive", "Wallet archived", walletEntity),
  adjust_balance: mutationModel("adjust_balance", "Adjust balance", "nothing is saved yet", "Adjust", "Balance adjusted", txEntity),
  record_expense: mutationModel("record_expense", "Record expense", "nothing is saved yet", "Save", "Expense recorded", txEntity),
  record_income: mutationModel("record_income", "Record income", "nothing is saved yet", "Save", "Income recorded", txEntity),
  record_transfer: mutationModel("record_transfer", "Record transfer", "nothing is saved yet", "Save", "Transfer recorded", txEntity),
  revise_tx: mutationModel("revise_tx", "Revise transaction", "nothing is saved yet", "Save", "Transaction revised", txEntity),
  delete_tx: mutationModel("delete_tx", "Delete transaction", "this transaction will be deleted", "Delete", "Transaction deleted", txEntity),
  reopen_period: mutationModel("reopen_period", "Reopen period", "nothing is saved yet", "Reopen", "Period reopened", periodEntity),
  create_category: mutationModel("create_category", "New category", "nothing is saved yet", "Create", "Category created", categoryEntity),
  // NOTE: close_period's preview is a Snapshot and its result a Period — different types, so no diff is possible.
  close_period: mutationModel("close_period", "Close period", "recomputed at close time", "Close", "Period closed", snapshotEntity, periodEntity),
});
