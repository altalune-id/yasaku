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
  El.prototype.querySelectorAll = function () {
    return this.children;
  };
  El.prototype.closest = function () {
    return this.attrs["data-action"] ? this : null;
  };

  const root = new El("div", { id: "root" });
  globalThis.__root = root;
  globalThis.__mkEl = function (attrs, children) {
    const e = new El("button", attrs);
    e.children = children || [];
    return e;
  };
  globalThis.console = globalThis.console || { error: function () {}, log: function () {} };
  globalThis.document = {
    getElementById: function (id) {
      return id === "root" ? root : null;
    },
  };
})();
