// Run with Playwright MCP browser_evaluate: pass this file's text as the function, e.g. (opts) => ...
// opts.drawer: CSS selector of an off-canvas drawer, skipped while closed. Returns {hits, summary}.
// Hit kinds: document-scroll, overflow-x, text-overlap (skips content that scrolls clear of a fixed or sticky bar, and raised floating panels over content), occluded-content, wrapped-control, small-target (filled, .btn or menu-row controls under 44px below 1024px), asymmetric-box. Opt out with data-scan-asym="<reason>" or data-scan-small="<reason>".
async (opts = {}) => {
  const { drawer = null, snippet = 60 } = opts;
  const frame = () => new Promise((r) => { requestAnimationFrame(() => requestAnimationFrame(r)); setTimeout(r, 100); });
  scrollTo(0, 0);
  await Promise.race([Promise.allSettled(document.getAnimations().filter((a) => a.timeline instanceof DocumentTimeline && a.effect?.getComputedTiming().endTime !== Infinity).map((a) => a.finished)), new Promise((r) => setTimeout(r, 1000))]);
  await frame();
  const vw = document.documentElement.clientWidth;
  const drawerEl = drawer ? document.querySelector(drawer) : null;
  const dr = drawerEl && drawerEl.getBoundingClientRect();
  const drawerClosed = !!drawerEl && (getComputedStyle(drawerEl).visibility === 'hidden' || dr.right <= 0 || dr.left >= vw || dr.bottom <= 0 || dr.top >= innerHeight);
  const modal = [...document.querySelectorAll('dialog[open]')].filter((d) => d.matches(':modal')).pop() || (drawerEl && !drawerClosed && getComputedStyle(drawerEl).position === 'fixed' ? drawerEl : null);
  const R = (r) => ({ x: Math.round(r.left), y: Math.round(r.top), w: Math.round(r.width), h: Math.round(r.height) });
  const path = (el) => {
    const parts = [];
    for (; el && el !== document.body && parts.length < 5; el = el.parentElement) {
      const cls = [...el.classList].slice(0, 2).join('.');
      parts.unshift(el.tagName.toLowerCase() + (el.id ? '#' + el.id : cls ? '.' + cls : ''));
    }
    return 'body > ' + parts.join(' > ');
  };
  const hidden = (el) => {
    for (let n = el; n && n !== document.documentElement; n = n.parentElement) {
      const cs = getComputedStyle(n);
      if (cs.display === 'none' || cs.visibility === 'hidden' || n.getAttribute('aria-hidden') === 'true' || n.hasAttribute('inert')) return true;
      if (n.tagName === 'DETAILS' && !n.open && n !== el && !n.querySelector(':scope > summary')?.contains(el)) return true;
      if (n.tagName === 'DIALOG' && !n.open) return true;
      if (drawerClosed && n === drawerEl) return true;
    }
    const r = el.getBoundingClientRect();
    if (!r.width || !r.height) return true;
    const cs = getComputedStyle(el);
    return cs.clip === 'rect(0px, 0px, 0px, 0px)' || (cs.position === 'absolute' && r.width <= 1 && r.height <= 1);
  };
  const hits = [];
  const add = (kind, el, rects) => hits.push({ kind, path: path(el), text: (el.textContent || '').trim().replace(/\s+/g, ' ').slice(0, snippet), rects });

  const all = [...document.body.querySelectorAll('*')].filter((el) => !['SCRIPT', 'STYLE', 'TEMPLATE', 'NOSCRIPT', 'SVG', 'PATH'].includes(el.tagName.toUpperCase()) && (!modal || modal.contains(el)) && !hidden(el));
  const clipped = (el) => {
    for (let n = el.parentElement; n && n !== document.documentElement; n = n.parentElement) {
      if (/auto|scroll|hidden|clip/.test(getComputedStyle(n).overflowX)) return true;
    }
    return false;
  };
  if (document.documentElement.scrollWidth > vw + 1) {
    add('document-scroll', document.body, [{ scrollWidth: document.documentElement.scrollWidth, viewport: vw }]);
  }
  for (const el of all) {
    const r = el.getBoundingClientRect();
    if ((r.right > vw + 1 || r.left < -1) && !clipped(el)) add('overflow-x', el, [R(r)]);
  }

  const clipBox = (el) => {
    let box = null;
    for (let n = el; n && n !== document.body; n = n.parentElement) {
      if (!/auto|scroll|hidden|clip/.test(getComputedStyle(n).overflowX)) continue;
      const r = n.getBoundingClientRect();
      box = box ? DOMRect.fromRect({ x: Math.max(box.left, r.left), y: Math.max(box.top, r.top), width: Math.max(0, Math.min(box.right, r.right) - Math.max(box.left, r.left)), height: Math.max(0, Math.min(box.bottom, r.bottom) - Math.max(box.top, r.top)) }) : r;
    }
    return box;
  };
  const leaves = [];
  for (const el of all) {
    const own = [...el.childNodes].some((n) => n.nodeType === 3 && n.textContent.trim());
    if (!own) continue;
    const range = document.createRange();
    range.selectNodeContents(el);
    const box = clipBox(el);
    const rects = [...range.getClientRects()].map((r) => box ? DOMRect.fromRect({ x: Math.max(r.left, box.left), y: Math.max(r.top, box.top), width: Math.min(r.right, box.right) - Math.max(r.left, box.left), height: Math.min(r.bottom, box.bottom) - Math.max(r.top, box.top) }) : r).filter((r) => r.width > 0 && r.height > 0);
    if (rects.length) leaves.push({ el, rects });
  }
  const pinned = (el) => {
    for (let n = el; n && n !== document.documentElement; n = n.parentElement) {
      const pos = getComputedStyle(n).position;
      if (pos === 'fixed' || pos === 'sticky') return n;
    }
    return null;
  };
  const maxScroll = Math.max(0, document.documentElement.scrollHeight - innerHeight);
  const revealable = (content, bar) => {
    const pin = pinned(bar);
    if (!pin || pinned(content)) return false;
    const b = pin.getBoundingClientRect(), e = content.getBoundingClientRect();
    return e.bottom - b.top <= maxScroll || e.top - b.bottom >= 0;
  };
  const floating = (el) => {
    for (let n = el; n && n !== document.body; n = n.parentElement) {
      const cs = getComputedStyle(n);
      if (cs.position === 'absolute' && cs.zIndex !== 'auto' && Number(cs.zIndex) > 0) return n;
    }
    return null;
  };
  const inter = (a, b) => Math.min(a.right, b.right) - Math.max(a.left, b.left) > 1 && Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top) > 1;
  for (let i = 0; i < leaves.length; i++) {
    for (let j = i + 1; j < leaves.length; j++) {
      const a = leaves[i], b = leaves[j];
      if (a.el.contains(b.el) || b.el.contains(a.el)) continue;
      if (revealable(a.el, b.el) || revealable(b.el, a.el)) continue;
      if (floating(a.el) !== floating(b.el)) continue;
      const pair = a.rects.flatMap((ra) => b.rects.filter((rb) => inter(ra, rb)).map((rb) => [R(ra), R(rb)]))[0];
      if (pair) add('text-overlap', a.el, [...pair, { other: path(b.el), text: b.el.textContent.trim().replace(/\s+/g, ' ').slice(0, snippet) }]);
    }
  }
  const bars = all.filter((el) => getComputedStyle(el).position === 'fixed' && Math.abs(el.getBoundingClientRect().bottom - innerHeight) <= 1 && el.getBoundingClientRect().height < innerHeight / 2);
  for (const bar of bars) {
    const b = bar.getBoundingClientRect();
    for (const { el, rects } of leaves) {
      if (pinned(el) || bar.contains(el)) continue;
      const last = rects[rects.length - 1];
      if (last.right <= b.left || last.left >= b.right) continue;
      if (last.bottom - maxScroll > b.top + 1) add('occluded-content', el, [R(last), { bar: path(bar), barTop: Math.round(b.top), maxScroll }]);
    }
  }
  for (const el of all.filter((e) => e.matches('button, .btn, [role="button"]'))) {
    const walker = document.createTreeWalker(el, NodeFilter.SHOW_TEXT);
    for (let t = walker.nextNode(); t; t = walker.nextNode()) {
      if (!t.textContent.trim() || hidden(t.parentElement)) continue;
      const range = document.createRange();
      range.selectNodeContents(t);
      const tops = new Set([...range.getClientRects()].filter((r) => r.width > 0 && r.height > 0).map((r) => Math.round(r.top)));
      if (tops.size > 1) {
        add('wrapped-control', el, [R(el.getBoundingClientRect())]);
        break;
      }
    }
  }

  if (vw < 1024) {
    for (const el of all.filter((e) => e.matches('a, button, summary, [role="button"]'))) {
      const bg = getComputedStyle(el).backgroundColor;
      const filled = el.classList.contains('btn') || el.matches('.menu li > a, .menu li > button, .menu li > details > summary') || !/^(transparent|rgba\(0, 0, 0, 0\))$/.test(bg);
      if (!filled || el.closest('[data-scan-small]')) continue;
      const r = el.getBoundingClientRect();
      if (r.height < 43.5) add('small-target', el, [R(r)]);
    }
  }

  const px = (v) => parseFloat(v) || 0;
  const boxes = '.btn, .menu li > a, .menu li > button, .menu li > details > summary, .dock > *, .card-body, .modal-box, .dropdown > div, .dropdown-content, .navbar, [role="button"], summary';
  for (const el of all) {
    if (!el.matches(boxes) || el.closest('[data-scan-asym]')) continue;
    const cs = getComputedStyle(el);
    if (Math.abs(px(cs.paddingTop) - px(cs.paddingBottom)) > 1 || Math.abs(px(cs.paddingInlineStart) - px(cs.paddingInlineEnd)) > 1) {
      add('asymmetric-box', el, [{ padding: cs.padding }]);
      continue;
    }
    const kids = [...el.children].filter((c) => { const cs = getComputedStyle(c), r = c.getBoundingClientRect(); return cs.display !== 'none' && cs.visibility !== 'hidden' && r.width > 0 && r.height > 0; });
    if (kids.length !== 1) continue;
    const r = el.getBoundingClientRect(), k = kids[0].getBoundingClientRect();
    const top = k.top - (r.top + px(cs.borderTopWidth) + px(cs.paddingTop));
    const bottom = r.bottom - px(cs.borderBottomWidth) - px(cs.paddingBottom) - k.bottom;
    if (Math.abs(top - bottom) > 1) add('asymmetric-box', el, [{ childTop: Math.round(top), childBottom: Math.round(bottom) }]);
  }
  return { hits, summary: { viewport: vw, checked: all.length, textBoxes: leaves.length, hits: hits.length, drawerSkipped: drawerClosed, modal: !!modal } };
}
