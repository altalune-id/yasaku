(function () {
	'use strict';

	if (window.__altDirtyForm) return;
	window.__altDirtyForm = 1;

	function changed(control) {
		if (control.tagName === 'SELECT') {
			return Array.prototype.some.call(control.options, function (o) { return o.selected !== o.defaultSelected; });
		}
		if (control.type === 'checkbox' || control.type === 'radio') return control.checked !== control.defaultChecked;
		return control.value !== control.defaultValue;
	}

	function isDirty(form) {
		return form.hasAttribute('data-dirty-start') || Array.prototype.some.call(form.elements, function (c) {
			return c.name && c.type !== 'hidden' && c.type !== 'submit' && c.type !== 'button' && changed(c);
		});
	}

	function toggle(form, selector, on) {
		form.querySelectorAll(selector).forEach(function (el) { el.hidden = !on; });
	}

	function sync(form) {
		var dirty = isDirty(form);
		var saving = form.hasAttribute('data-saving');
		toggle(form, '[data-dirty-only]', dirty && !saving);
		toggle(form, '[data-dirty-cancel]', dirty && !saving);
		toggle(form, '[data-dirty-saving]', saving);
		toggle(form, '[data-dirty-label]', !saving);
		form.querySelectorAll('[data-dirty-submit]').forEach(function (b) {
			b.disabled = !dirty || saving;
		});
	}

	function syncAll() {
		document.querySelectorAll('form[data-dirty-form]').forEach(function (f) {
			f.removeAttribute('data-saving');
			sync(f);
		});
	}

	function formOf(e) {
		return e.target && e.target.closest ? e.target.closest('form[data-dirty-form]') : null;
	}

	['input', 'change', 'focusin', 'focusout'].forEach(function (type) {
		document.addEventListener(type, function (e) {
			var form = formOf(e);
			if (form) sync(form);
		});
	});

	document.addEventListener('click', function (e) {
		var cancel = e.target.closest && e.target.closest('[data-dirty-cancel]');
		var form = cancel && cancel.closest('form[data-dirty-form]');
		if (!form) return;
		form.removeAttribute('data-dirty-start');
		form.reset();
		sync(form);
	});

	document.addEventListener('submit', function (e) {
		var form = formOf(e);
		if (!form) return;
		form.setAttribute('data-saving', '');
		window.setTimeout(function () { sync(form); }, 0);
	});

	window.addEventListener('pageshow', syncAll);
	window.setTimeout(syncAll, 500);
	if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', syncAll);
	else syncAll();
})();
