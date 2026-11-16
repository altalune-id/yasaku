(function () {
	'use strict';

	if (window.__altTips) return;
	window.__altTips = 1;

	var GAP = 8;
	var reduced = window.matchMedia('(prefers-reduced-motion: reduce)');

	function targetOf(tip) {
		var key = tip.getAttribute('data-tip-for');
		if (!key) return null;
		var el = document.querySelector('[data-tour="' + CSS.escape(key) + '"]');
		return el && el.getClientRects().length ? el : null;
	}

	function place(tip) {
		if (!tip.hasAttribute('data-tip-for')) return true;
		var target = targetOf(tip);
		tip.hidden = !target;
		if (!target) return false;
		var r = target.getBoundingClientRect();
		var vw = document.documentElement.clientWidth;
		var below = r.bottom + GAP + tip.offsetHeight <= window.innerHeight;
		tip.style.top = Math.max(GAP, below ? r.bottom + GAP : r.top - GAP - tip.offsetHeight) + 'px';
		var w = tip.offsetWidth;
		var left = r.left + r.width / 2 - w / 2;
		if (left + w > vw - GAP) left = r.right - w;
		if (left < GAP) left = r.left;
		tip.style.left = Math.min(Math.max(GAP, left), vw - w - GAP) + 'px';
		return true;
	}

	function hasAutofocus() {
		return Array.prototype.some.call(document.querySelectorAll('[autofocus]'), function (el) {
			var dialog = el.closest('dialog');
			return !dialog || dialog.open;
		});
	}

	function refresh(focus) {
		var first = null;
		document.querySelectorAll('[data-tip]').forEach(function (tip) {
			if (first) {
				tip.hidden = true;
				return;
			}
			tip.hidden = false;
			if (place(tip)) first = tip;
		});
		var active = document.activeElement;
		if (!focus || !first || (active && active !== document.body) || hasAutofocus()) return;
		first.focus({ preventScroll: true });
		first.scrollIntoView({ block: 'nearest', behavior: reduced.matches ? 'auto' : 'smooth' });
	}

	function dismiss(form, all) {
		var tip = form.closest('[data-tip]');
		if (!tip) return;
		var back = targetOf(tip) || document.getElementById('main');
		var flow = tip.getAttribute('data-tip-flow');
		var gone = all ? document.querySelectorAll('[data-tip-flow="' + CSS.escape(flow) + '"]') : [tip];
		gone.forEach(function (t) { t.remove(); });
		if (back) back.focus();
		refresh(false);
	}

	var queued = false;
	function schedule() {
		if (queued) return;
		queued = true;
		window.requestAnimationFrame(function () {
			queued = false;
			refresh(false);
		});
	}

	document.addEventListener('htmx:after:request', function (e) {
		var ctx = e.detail && e.detail.ctx;
		var status = ctx && ctx.response && ctx.response.status;
		var form = e.target;
		if (typeof status !== 'number' || status >= 400 || !form || !form.hasAttribute || !form.hasAttribute('data-tip-form')) return;
		var submitter = ctx.request && ctx.request.submitter;
		dismiss(form, !!(submitter && submitter.name === 'all'));
	});

	document.addEventListener('htmx:after:swap', schedule);
	window.addEventListener('resize', schedule);
	document.addEventListener('scroll', schedule, { capture: true, passive: true });

	if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', function () { refresh(true); });
	else refresh(true);
})();
