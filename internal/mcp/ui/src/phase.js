// phase.js
function needsList(d) {
  const n = d && d.needs;
  if (!n || !Array.isArray(n.needs)) return [];
  return n.needs;
}

function phaseOf(d) {
  const data = d || {};
  if (needsList(data).length > 0) return "needs";
  if (Array.isArray(data.results)) return data.results.length > 0 ? "result" : "empty";
  if (Array.isArray(data.preview)) return data.preview.length > 0 ? "preview" : "empty";
  if (data.result !== undefined && data.result !== null) return "result";
  if (data.preview !== undefined && data.preview !== null) return "preview";
  if (data.inserted !== undefined) return "result";
  if (data.previewCount !== undefined) return "preview";
  return "empty";
}
