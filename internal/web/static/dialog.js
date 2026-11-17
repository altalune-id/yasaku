(function () {
	'use strict';

	if (window.__altDialog) return;
	window.__altDialog = 1;

	var openers = new WeakMap();
	var shown = null;
	var pressedOn = null;

	function closest(el, sel) {
		return el && el.closest ? el.closest(sel) : null;
	}

	document.addEventListener('mousedown', function (e) {
		pressedOn = e.target;
	});

	document.addEventListener('click', function (e) {
		var opener = closest(e.target, '[data-dialog-open]');
		if (opener) {
			var target = document.getElementById(opener.getAttribute('data-dialog-open'));
			if (!target) {
				load(opener, e);
				return;
			}
			if (typeof target.showModal !== 'function') return;
			e.preventDefault();
			open(target, opener);
			return;
		}
		var closer = closest(e.target, '[data-dialog-close]');
		if (closer) {
			var owner = closest(closer, 'dialog');
			if (!owner) {
				var menu = closest(closer, 'details[open]');
				if (menu) {
					menu.removeAttribute('open');
					var summary = menu.querySelector(':scope > summary');
					if (summary) summary.focus();
				}
				return;
			}
			e.preventDefault();
			owner.close();
			return;
		}
		var self = e.target;
		if (self instanceof HTMLDialogElement && self.open && pressedOn === self) self.close();
	});

	function open(target, opener) {
		openers.set(target, opener);
		shown = target;
		if (!target.open) target.showModal();
		focusInside(target);
	}

	function load(opener, e) {
		var src = opener.getAttribute('data-dialog-src');
		var host = document.getElementById('dialog-host');
		if (!src || !host || !window.htmx) return;
		e.preventDefault();
		if (opener.hasAttribute('aria-busy')) return;
		opener.setAttribute('aria-busy', 'true');
		var id = opener.getAttribute('data-dialog-open');
		var fallback = function () {
			window.location.assign(opener.getAttribute('href') || src);
		};
		window.htmx.ajax('GET', src, { target: host, swap: 'beforeend' }).then(function () {
			opener.removeAttribute('aria-busy');
			var target = document.getElementById(id);
			if (!target || typeof target.showModal !== 'function') return fallback();
			open(target, opener);
		}, function () {
			opener.removeAttribute('aria-busy');
			fallback();
		});
	}

	document.addEventListener('keydown', function (e) {
		if (e.defaultPrevented || e.repeat || e.ctrlKey || e.metaKey || e.altKey || e.shiftKey || e.isComposing) return;
		var t = e.target;
		if (!/^[a-z]$/.test(e.key) || document.querySelector('dialog[open]')) return;
		if (t && (t.isContentEditable || closest(t, 'input, textarea, select'))) return;
		var el = document.querySelector('[data-shortcut="' + e.key.toLowerCase() + '"]');
		if (!el) return;
		e.preventDefault();
		el.click();
	});

	var focusable =
		'[autofocus], a[href], button:not([disabled]), input:not([disabled]):not([type=hidden]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

	function focusInside(dialog) {
		if (dialog.contains(document.activeElement)) return;
		var first = dialog.querySelector('[autofocus]') || dialog.querySelector(focusable);
		if (first) first.focus();
	}

	function fallbackFocus(opener) {
		var target = opener && opener.isConnected ? opener : document.getElementById('main');
		if (target) target.focus();
	}

	document.addEventListener(
		'close',
		function (e) {
			if (e.target === shown) shown = null;
			var opener = openers.get(e.target);
			if (!opener) return;
			openers.delete(e.target);
			fallbackFocus(opener);
		},
		true
	);

	document.addEventListener('htmx:after:swap', function () {
		if (!shown || shown.isConnected) return;
		var opener = openers.get(shown);
		shown = null;
		fallbackFocus(opener);
	});

	document.addEventListener('htmx:after:request', function (e) {
		var ctx = e.detail && e.detail.ctx;
		var status = ctx && ctx.response && ctx.response.status;
		if (typeof status !== 'number' || status >= 400) return;
		var form = e.target;
		if (!form || form.tagName !== 'FORM') return;
		var owner = closest(form, 'dialog[data-dialog-close-on-success]');
		if (!owner || !owner.open) return;
		var submitter = ctx.request && ctx.request.submitter;
		if (submitter && submitter.hasAttribute('data-keep-open')) return;
		owner.close();
	});
})();
