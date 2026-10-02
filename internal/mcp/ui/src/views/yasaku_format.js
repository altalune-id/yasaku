const CIVIL_DATE = /^\d{4}-\d{2}-\d{2}$/;

// NOTE: protojson normalises an instant to UTC, which reads a day early east of Greenwich, so only the server's civil date is ever shown.
function civilDay(date) {
  const d = text(date, "");
  return CIVIL_DATE.test(d) ? d : "—";
}

const INSTANT = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}(:\d{2}(\.\d+)?)?Z$/;

// NOTE: goja has no Intl, so an instant falls back to its UTC wall time.
function dateTime(ts) {
  const s = text(ts, "");
  if (!INSTANT.test(s)) return "—";
  if (typeof Intl === "object") {
    try {
      return new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" }).format(new Date(s));
    } catch (e) {}
  }
  return s.slice(0, 10) + " " + s.slice(11, 16) + " UTC";
}

function browserOffset(ts) {
  if (typeof Intl !== "object") return "";
  try {
    const parts = new Intl.DateTimeFormat("en-US", { timeZoneName: "shortOffset" }).formatToParts(new Date(ts));
    const p = parts.find((x) => x.type === "timeZoneName");
    if (!p) return "";
    return p.value === "GMT" ? "GMT+0" : p.value;
  } catch (e) {
    return "";
  }
}

function recordedTitle(ts) {
  const s = text(ts, "");
  const off = INSTANT.test(s) ? browserOffset(s) : "";
  return off ? "Recorded " + dateTime(s) + " · your time (" + off + ")" : "";
}

function money(m) {
  if (!m || typeof m !== "object") return "—";
  const amount = text(m.amount, "0");
  const currency = text(m.currency, "");
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
  const a = text(start, "");
  const b = text(end, "");
  if (!a && !b) return "";
  if (!b) return a;
  if (!a) return b;
  return a + " → " + b;
}
