// Minimal DOM for goja: enough to mount boot.js and dispatch synthetic events.
(function () {
  function El(tag, attrs) {
    this.tagName = tag || "div";
    this.attrs = Object.assign(Object.create(null), attrs || {});
    this.children = [];
    this.innerHTML = "";
    this.disabled = false;
    this.listeners = Object.create(null);
  }
  El.prototype.getAttribute = function (k) {
    return k in this.attrs ? this.attrs[k] : null;
  };
  El.prototype.addEventListener = function (type, fn) {
    (this.listeners[type] = this.listeners[type] || []).push(fn);
  };
  El.prototype.dispatch = function (type, ev) {
    const ls = this.listeners[type] || [];
    for (let i = 0; i < ls.length; i++) ls[i](ev);
  };
  El.prototype.matches = function (sel) {
    if (sel === "[name]") return this.attrs["name"] !== undefined;
    if (sel === "[data-action]") return this.attrs["data-action"] !== undefined;
    if (sel === "form") return this.tagName === "form";
    return false;
  };
  El.prototype.querySelectorAll = function (sel) {
    const out = [];
    for (let i = 0; i < this.children.length; i++) {
      if (this.children[i].matches(sel)) out.push(this.children[i]);
    }
    return out;
  };
  El.prototype.querySelector = function (sel) {
    const all = this.querySelectorAll(sel);
    return all.length ? all[0] : null;
  };
  El.prototype.closest = function (sel) {
    let n = this;
    while (n) {
      if (n.matches(sel)) return n;
      n = n.parent || null;
    }
    return null;
  };

  const root = new El("div", { id: "root" });
  globalThis.__root = root;
  globalThis.__mkEl = function (attrs, children, tag) {
    const e = new El(tag || "button", attrs);
    e.children = children || [];
    for (let i = 0; i < e.children.length; i++) e.children[i].parent = e;
    return e;
  };
  globalThis.console = globalThis.console || { error: function () {}, log: function () {} };
  globalThis.document = {
    getElementById: function (id) {
      return id === "root" ? root : null;
    },
  };
})();
