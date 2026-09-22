// charts.js
const CHART_FALLBACKS = ["#3b82f6", "#f97316", "#22c55e", "#a855f7", "#64748b"];

function swatchColour(token) {
  if (!token) return CHART_FALLBACKS[4];
  if (String(token).charAt(0) === "#") return String(token);
  const n = parseInt(String(token).replace("chart-", ""), 10);
  const fallback = CHART_FALLBACKS[(isFinite(n) ? n - 1 : 4) % CHART_FALLBACKS.length];
  return "var(--color-" + esc(token) + ", " + fallback + ")";
}

function donutSVG(slices) {
  if (!slices || !slices.length) return "";
  const r = 42;
  const c = 2 * Math.PI * r;
  let offset = 0;
  const ring = slices.map(function (s, i) {
    const share = isFinite(Number(s.share)) ? Number(s.share) : 0;
    const len = c * share;
    const seg =
      '<circle cx="60" cy="60" r="' + r + '" fill="none" stroke-width="16"' +
      ' stroke="' + swatchColour((s.category || {}).color || "chart-" + ((i % 5) + 1)) + '"' +
      ' stroke-dasharray="' + len.toFixed(2) + " " + (c - len).toFixed(2) + '"' +
      ' stroke-dashoffset="' + (-offset).toFixed(2) + '" transform="rotate(-90 60 60)"></circle>';
    offset += len;
    return seg;
  });
  return (
    '<svg viewBox="0 0 120 120" width="120" height="120" role="img" aria-label="Spend by category">' +
    '<circle cx="60" cy="60" r="' + r + '" fill="none" stroke-width="16" stroke="var(--color-background-tertiary, #e4e4e7)"></circle>' +
    ring.join("") +
    "</svg>"
  );
}

function cashflowSVG(points) {
  if (!points || !points.length) return "";
  const w = 320, h = 120, pad = 18;
  const vals = points.map(function (p) {
    return {
      income: Math.abs(Number((p.income || {}).amount) || 0),
      expense: Math.abs(Number((p.expense || {}).amount) || 0),
      label: (p.period || {}).name || "",
    };
  });
  let max = 0;
  vals.forEach(function (v) { max = Math.max(max, v.income, v.expense); });
  if (max <= 0) max = 1;

  const slot = (w - pad * 2) / vals.length;
  const bw = Math.max(3, slot / 3);
  const bars = vals.map(function (v, i) {
    const x = pad + i * slot + slot / 2;
    const ih = ((h - pad * 2) * v.income) / max;
    const eh = ((h - pad * 2) * v.expense) / max;
    return (
      '<rect x="' + (x - bw - 1).toFixed(1) + '" y="' + (h - pad - ih).toFixed(1) +
      '" width="' + bw.toFixed(1) + '" height="' + ih.toFixed(1) + '" rx="2" fill="' + CHART_FALLBACKS[2] + '"></rect>' +
      '<rect x="' + (x + 1).toFixed(1) + '" y="' + (h - pad - eh).toFixed(1) +
      '" width="' + bw.toFixed(1) + '" height="' + eh.toFixed(1) + '" rx="2" fill="' + CHART_FALLBACKS[1] + '"></rect>'
    );
  });
  const labels = vals.map(function (v, i) {
    const x = pad + i * slot + slot / 2;
    return '<text x="' + x.toFixed(1) + '" y="' + (h - 4) + '" font-size="9" text-anchor="middle" fill="currentColor" opacity="0.7">' + esc(v.label) + "</text>";
  });
  return (
    '<svg viewBox="0 0 ' + w + " " + h + '" width="100%" height="' + h + '" role="img" aria-label="Cashflow by period">' +
    bars.join("") + labels.join("") + "</svg>"
  );
}
