// format.js
// NOTE: goja does not implement toLocaleString(locale) — it reads the argument
// as a radix and throws RangeError. Group digits by hand, which also keeps the
// output identical across host locales we do not control.
function group(digits) {
  return String(digits).replace(/\B(?=(\d{3})+(?!\d))/g, ",");
}

function num(n) {
  const v = Number(n);
  if (!isFinite(v)) return "0";
  const neg = v < 0;
  return (neg ? "-" : "") + group(Math.abs(v));
}

function money(m) {
  if (!m) return "—";
  const amount = m.amount === undefined || m.amount === null ? "0" : String(m.amount);
  const currency = m.currency || "";
  const neg = amount.charAt(0) === "-";
  const digits = neg ? amount.slice(1) : amount;
  return (currency ? currency + " " : "") + (neg ? "-" : "") + group(digits);
}

function pct(f) {
  const v = Number(f);
  if (!isFinite(v)) return "0%";
  return Math.round(v * 100) + "%";
}

function dateRange(start, end) {
  if (!start && !end) return "";
  if (!end) return String(start);
  if (!start) return String(end);
  return start + " → " + end;
}
