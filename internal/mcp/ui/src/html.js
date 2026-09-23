// html.js
// SECURITY: RAW holds only markup this bundle minted. Server JSON is interpolated
// verbatim, so a duck-typed {__raw:…} from a tool result must never bypass escaping.
const RAW = new WeakSet();

function esc(value) {
  if (value === null || value === undefined) return "";
  if (typeof value === "object") {
    return RAW.has(value) ? value.__raw : "";
  }
  return String(value).replace(/[&<>"']/g, function (c) {
    return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c];
  });
}

function html(strings, ...values) {
  let out = strings[0];
  for (let i = 0; i < values.length; i++) {
    const v = values[i];
    out += (Array.isArray(v) ? v.map(esc).join("") : esc(v)) + strings[i + 1];
  }
  return out;
}

// raw marks already-escaped markup (a nested html`` result) as safe to embed.
function raw(markup) {
  if (markup && typeof markup === "object" && RAW.has(markup)) return markup;
  const box = { __raw: String(markup) };
  RAW.add(box);
  return box;
}
