const CHART_FALLBACKS = ["#3b82f6", "#f97316", "#22c55e", "#a855f7", "#64748b"];

// SECURITY: the result lands in a raw SVG attribute and in style="background:…", so only an allow-listed token may leave this function.
const CHART_TOKEN = /^chart-[1-5]$/;

function swatchColour(token) {
  const t = String(token === undefined || token === null ? "" : token);
  if (HEX_COLOUR.test(t)) return t;
  if (CHART_TOKEN.test(t)) {
    const n = parseInt(t.slice(6), 10);
    return "var(--color-" + t + ", " + CHART_FALLBACKS[n - 1] + ")";
  }
  return CHART_FALLBACKS[4];
}

const DONUT_RADIUS = 42;

function donutModel(slices) {
  const list = Array.isArray(slices) ? slices : [];
  const c = 2 * Math.PI * DONUT_RADIUS;
  let offset = 0;
  const segments = list.map(function (s, i) {
    const share = isFinite(Number(s.share)) ? Number(s.share) : 0;
    const len = c * share;
    const seg = {
      stroke: swatchColour((s.category || {}).color || "chart-" + ((i % 5) + 1)),
      dash: len.toFixed(2) + " " + (c - len).toFixed(2),
      offset: (-offset).toFixed(2),
    };
    offset += len;
    return seg;
  });
  return { empty: segments.length === 0, r: String(DONUT_RADIUS), segments: segments };
}

const CASHFLOW_W = 320;
const CASHFLOW_H = 120;
const CASHFLOW_PAD = 18;

function cashflowModel(points) {
  const list = Array.isArray(points) ? points : [];
  const vals = list.map(function (p) {
    return {
      income: Math.abs(Number((p.income || {}).amount) || 0),
      expense: Math.abs(Number((p.expense || {}).amount) || 0),
      label: text((p.period || {}).name, ""),
    };
  });
  let max = 0;
  vals.forEach(function (v) { max = Math.max(max, v.income, v.expense); });
  if (max <= 0) max = 1;

  const w = CASHFLOW_W, h = CASHFLOW_H, pad = CASHFLOW_PAD;
  const slot = vals.length ? (w - pad * 2) / vals.length : 0;
  const bw = Math.max(3, slot / 3);
  const bars = [];
  const labels = [];
  vals.forEach(function (v, i) {
    const x = pad + i * slot + slot / 2;
    const ih = ((h - pad * 2) * v.income) / max;
    const eh = ((h - pad * 2) * v.expense) / max;
    bars.push({ x: (x - bw - 1).toFixed(1), y: (h - pad - ih).toFixed(1), width: bw.toFixed(1), height: ih.toFixed(1), fill: CHART_FALLBACKS[2] });
    bars.push({ x: (x + 1).toFixed(1), y: (h - pad - eh).toFixed(1), width: bw.toFixed(1), height: eh.toFixed(1), fill: CHART_FALLBACKS[1] });
    labels.push({ x: x.toFixed(1), y: String(h - 4), text: v.label });
  });
  return {
    empty: vals.length === 0,
    bars: bars,
    labels: labels,
  };
}
