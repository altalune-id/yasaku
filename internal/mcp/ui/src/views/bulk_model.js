// NOTE: BatchOutcome.index is omitted for row 0, and a refused row may carry error with no errorCode.
function batchRowModel(o, i) {
  const x = o && typeof o === "object" ? o : {};
  const idx = (typeof x.index === "number" ? x.index : i) + 1;
  const code = text(x.errorCode, "");
  const error = text(x.error, "");
  const failed = !!(code || error);
  return {
    row: "Row " + num(idx),
    failed: failed,
    code: code || "refused",
    error: error,
    tx: txRowModel(failed ? {} : x.transaction),
  };
}

function batchCardModel(maybeRows) {
  const rows = Array.isArray(maybeRows) ? maybeRows : [];
  const models = rows.map(batchRowModel);
  const clean = models.filter(function (r) { return !r.failed; }).length;
  return { label: num(clean) + " of " + num(rows.length) + " rows are writable", rows: models };
}

function recordBatchModel(d, action) {
  const phase = phaseOf(d);
  if (phase === "needs") return needsModel(d, action, "record_batch", "Record batch");
  if (phase === "result") return { phase: "result", title: "Batch recorded", batch: batchCardModel(d.results) };
  if (phase === "preview") {
    return {
      phase: "preview",
      title: "Record batch",
      note: "nothing is saved yet",
      batch: batchCardModel(d.preview),
      commit: action("commit", "record_batch", { confirm: true }),
    };
  }
  return { phase: "empty", message: text(d.warning, "Nothing to record.") };
}

// NOTE: at zero, previewed and committed are byte-identical {} on the wire — they cannot be told apart.
function seedCategoriesModel(d, action) {
  if (needsList(d).length > 0) return needsModel(d, action, "seed_default_categories", "Seed default categories");
  if (d.inserted !== undefined) {
    return { phase: "result", title: "Defaults added", note: num(d.inserted) + " categories inserted." };
  }
  if (d.previewCount !== undefined) {
    return {
      phase: "preview",
      title: "Seed default categories",
      note: num(d.previewCount) + " would be added. Nothing is saved yet.",
      commit: action("commit", "seed_default_categories", { confirm: true }),
    };
  }
  return { phase: "empty", message: text(d.warning, "Nothing to do — the project already has every default.") };
}
