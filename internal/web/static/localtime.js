(function () {
	'use strict';

	if (window.__yasakuLocaltime) return;
	window.__yasakuLocaltime = 1;

	var lang = document.documentElement.lang || undefined;
	var stamp = { dateStyle: 'medium', timeStyle: 'short' };
	var seq = 0;

	function fmt(opts, at) {
		try {
			return new Intl.DateTimeFormat(lang, opts).format(at);
		} catch (e) {
			return '';
		}
	}

	function offset(tz, at) {
		try {
			var o = { timeZoneName: 'shortOffset' };
			if (tz) o.timeZone = tz;
			var part = new Intl.DateTimeFormat('en-US', o).formatToParts(at).find(function (p) {
				return p.type === 'timeZoneName';
			});
			return part.value === 'GMT' ? 'GMT+0' : part.value;
		} catch (e) {
			return '';
		}
	}

	function differs(tz, at) {
		var ledger = offset(tz, at);
		return ledger !== '' && ledger !== offset('', at);
	}

	function arm(el, at, time, off) {
		var tz = el.getAttribute('data-ledger-tz');
		if (!tz || isNaN(at) || !differs(tz, at)) return;
		var text = el.getAttribute('data-dt-alt-label').replace('{time}', time(tz)).replace('{offset}', off(tz));
		var btn = el.closest('[data-dt-trigger]');
		var tpl = document.querySelector('template[data-dt-template]');
		if (!btn && tpl) {
			var wrap = document.importNode(tpl.content.firstElementChild, true);
			btn = wrap.querySelector('[data-dt-trigger]');
			var pop = wrap.querySelector('[data-dt-pop]');
			pop.id = 'dt-pop-' + ++seq;
			btn.setAttribute('aria-describedby', pop.id);
			btn.popoverTargetElement = pop;
			el.replaceWith(wrap);
			btn.appendChild(el);
		}
		if (btn) btn.popoverTargetElement.textContent = text;
	}

	function apply() {
		var now = new Date();
		document.querySelectorAll('time[data-datetime]').forEach(function (el) {
			var at = new Date(el.getAttribute('datetime'));
			if (isNaN(at)) return;
			el.textContent = fmt(stamp, at) || el.textContent;
			arm(el, at, function (tz) {
				return fmt(Object.assign({ timeZone: tz }, stamp), at);
			}, function (tz) {
				return offset(tz, at);
			});
		});
		document.querySelectorAll('time[data-ledger-date]').forEach(function (el) {
			var m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(el.getAttribute('datetime') || '');
			if (!m) return;
			var compact = el.getAttribute('data-date-style') === 'compact';
			var day = new Date(Date.UTC(+m[1], m[2] - 1, +m[3]));
			el.textContent = fmt({ timeZone: 'UTC', day: 'numeric', month: 'short', year: compact ? undefined : 'numeric' }, day) || el.textContent;
			var at = new Date(el.getAttribute('data-datetime-alt'));
			arm(el, at, function () {
				return fmt(stamp, at);
			}, function () {
				return offset('', at);
			});
		});
		document.querySelectorAll('[data-tz-hint]').forEach(function (el) {
			var tz = el.getAttribute('data-ledger-tz');
			if (!el.hasAttribute('data-tz-tpl')) el.setAttribute('data-tz-tpl', el.textContent.trim());
			el.hidden = !differs(tz, now);
			if (!el.hidden) el.textContent = el.getAttribute('data-tz-tpl').replace('{offset}', offset('', now)).replace('{ledgerOffset}', offset(tz, now));
		});
	}

	function open(pop) {
		return pop.matches(':popover-open');
	}

	function show(btn, mode) {
		var pop = btn.popoverTargetElement;
		if (!open(pop)) pop.showPopover();
		pop.dataset.dtMode = mode;
		var r = btn.getBoundingClientRect();
		var top = r.bottom + 4;
		if (top + pop.offsetHeight > window.innerHeight - 8) top = Math.max(8, r.top - pop.offsetHeight - 4);
		pop.style.left = Math.max(8, Math.min(r.left, window.innerWidth - pop.offsetWidth - 8)) + 'px';
		pop.style.top = top + 'px';
	}

	function closeAll() {
		document.querySelectorAll('[data-dt-pop]:popover-open').forEach(function (pop) {
			pop.hidePopover();
		});
	}

	function on(type, fn) {
		document.addEventListener(type, function (e) {
			var btn = e.target.closest && e.target.closest('[data-dt-trigger]');
			if (btn) fn(e, btn, btn.popoverTargetElement);
		});
	}

	on('click', function (e, btn, pop) {
		e.preventDefault();
		if (open(pop) && pop.dataset.dtMode === 'pin') pop.hidePopover();
		else show(btn, 'pin');
	});
	on('pointerover', function (e, btn, pop) {
		if (e.pointerType === 'mouse' && !open(pop)) show(btn, 'hover');
	});
	on('pointerout', function (e, btn, pop) {
		if (!btn.contains(e.relatedTarget) && open(pop) && pop.dataset.dtMode === 'hover') pop.hidePopover();
	});
	on('focusin', function (e, btn, pop) {
		if (btn.matches(':focus-visible') && !open(pop)) show(btn, 'hover');
	});
	on('focusout', function (e, btn, pop) {
		if (open(pop)) pop.hidePopover();
	});
	document.addEventListener('scroll', closeAll, { capture: true, passive: true });
	window.addEventListener('resize', closeAll);

	if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', apply);
	else apply();
	document.addEventListener('htmx:after:settle', apply);
})();
