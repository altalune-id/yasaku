(function () {
	'use strict';

	if (window.__yasakuDialog) return;
	window.__yasakuDialog = 1;

	var openers = new WeakMap();
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
			if (!target || typeof target.showModal !== 'function') return;
			e.preventDefault();
			openers.set(target, opener);
			if (!target.open) target.showModal();
			return;
		}
		var closer = closest(e.target, '[data-dialog-close]');
		if (closer) {
			var owner = closest(closer, 'dialog');
			if (!owner) return;
			e.preventDefault();
			owner.close();
			return;
		}
		var self = e.target;
		if (self instanceof HTMLDialogElement && self.open && pressedOn === self) self.close();
	});

	document.addEventListener(
		'close',
		function (e) {
			var opener = openers.get(e.target);
			if (!opener) return;
			openers.delete(e.target);
			if (opener.isConnected) opener.focus();
		},
		true
	);

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
