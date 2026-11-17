(function () {
	'use strict';

	if (window.__altDate) return;
	window.__altDate = 1;

	function label(field, value) {
		var el = field.querySelector('[data-date-label]');
		if (!el) return;
		if (value === field.dataset.today) { el.textContent = field.dataset.todayLabel; return; }
		if (value === field.dataset.yesterday) { el.textContent = field.dataset.yesterdayLabel; return; }
		var p = value.split('-').map(Number);
		if (p.length !== 3 || p.some(isNaN)) { el.textContent = value; return; }
		el.textContent = new Intl.DateTimeFormat(field.dataset.locale, {
			day: 'numeric', month: 'short', year: 'numeric', timeZone: 'UTC'
		}).format(new Date(Date.UTC(p[0], p[1] - 1, p[2])));
	}

	function set(field, value) {
		var input = field.querySelector('[data-date-input]');
		if (!input || !value) return;
		input.value = value;
		input.dispatchEvent(new Event('change', { bubbles: true }));
		var cal = field.querySelector('calendar-date');
		if (cal && cal.value !== value) cal.value = value;
		label(field, value);
		var dialog = field.querySelector('dialog');
		if (dialog && dialog.open) dialog.close();
	}

	function pad(n) {
		return (n < 10 ? '0' : '') + n;
	}

	// NOTE: the server's max is the ledger's today; a browser already on a later day may still pick its own today.
	function capFuture(field) {
		var max = field.dataset.max;
		if (!max) return;
		var now = new Date();
		var local = now.getFullYear() + '-' + pad(now.getMonth() + 1) + '-' + pad(now.getDate());
		if (local > max) max = local;
		field.querySelector('[data-date-input]').max = max;
		var cal = field.querySelector('calendar-date');
		if (cal) cal.setAttribute('max', max);
	}

	function mount() {
		document.querySelectorAll('[data-date-field]:not([data-date-mounted])').forEach(function (field) {
			field.dataset.dateMounted = '1';
			capFuture(field);
			field.querySelector('[data-date-native]').hidden = true;
			field.querySelector('[data-date-picker]').hidden = false;
		});
	}

	document.addEventListener('change', function (e) {
		var cal = e.target.closest && e.target.closest('calendar-date');
		var field = cal && cal.closest('[data-date-field]');
		if (field) set(field, cal.value);
	}, true);

	document.addEventListener('click', function (e) {
		var btn = e.target.closest && e.target.closest('[data-date-set]');
		var field = btn && btn.closest('[data-date-field]');
		if (field) set(field, btn.dataset.dateSet);
	});

	function ready() {
		mount();
		document.addEventListener('htmx:after:swap', mount);
	}
	if (window.customElements) window.customElements.whenDefined('calendar-date').then(ready);
})();
