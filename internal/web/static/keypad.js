(function () {
	'use strict';

	if (window.__altKeypad) return;
	window.__altKeypad = 1;

	var OPS = /([+\-−×÷*/xX])/;
	var coarse = window.matchMedia('(pointer: coarse)');

	function amountOf(form) {
		return form && form.querySelector('[data-keypad-input]');
	}

	function pow(exp) {
		return Math.pow(10, exp);
	}

	function grouped(whole, group) {
		return /^\d+$/.test(whole) || new RegExp('^[1-9]\\d{0,2}(\\' + group + '\\d{3})+$').test(whole);
	}

	// NOTE: mirrors money.ParseLocale; a shape the server would refuse returns null so the raw text reaches the server.
	function toMinor(s, dec, exp) {
		if (exp === 0) {
			if (!grouped(s, '.') && !grouped(s, ',')) return null;
			var n = Number(s.replace(/[.,]/g, ''));
			return Number.isSafeInteger(n) ? n : null;
		}
		var parts = s.split(dec);
		var frac = parts.length === 2 ? parts[1] : '';
		if (parts.length > 2 || !grouped(parts[0], dec === ',' ? '.' : ',') || !/^\d*$/.test(frac) || frac.length > exp) return null;
		var minor = Number(parts[0].replace(/[.,]/g, '')) * pow(exp) + Number((frac + '000000').slice(0, exp));
		return Number.isSafeInteger(minor) ? minor : null;
	}

	function evaluate(raw, dec, exp) {
		var toks = raw.replace(/\s/g, '').split(OPS).filter(function (t) { return t !== ''; });
		if (toks.length === 0 || OPS.test(toks[0])) return null;
		if (OPS.test(toks[toks.length - 1])) toks.pop();
		if (toks.length === 1) return toMinor(toks[0], dec, exp);
		var nums = [], ops = [];
		for (var i = 0; i < toks.length; i++) {
			if (i % 2 === 1) { ops.push(toks[i]); continue; }
			var n = toMinor(toks[i], dec, exp);
			if (n === null) return null;
			nums.push(n);
		}
		var acc = [nums[0]], add = [];
		for (var j = 0; j < ops.length; j++) {
			var op = ops[j], b = nums[j + 1], last = acc.length - 1;
			if (op === '×' || op === '*' || op === 'x' || op === 'X') {
				if (!Number.isSafeInteger(acc[last] * b)) return null;
				acc[last] = Math.round((acc[last] * b) / pow(exp));
			} else if (op === '÷' || op === '/') {
				if (b === 0) return null;
				acc[last] = Math.round((acc[last] * pow(exp)) / b);
			} else { add.push(op); acc.push(b); }
		}
		var r = acc[0];
		for (var k = 0; k < add.length; k++) r = add[k] === '+' ? r + acc[k + 1] : r - acc[k + 1];
		return Number.isSafeInteger(r) && r >= 0 ? r : null;
	}

	function render(minor, dec, exp) {
		var whole = Math.floor(minor / pow(exp)), frac = minor % pow(exp);
		if (exp === 0 || frac === 0) return String(whole);
		return whole + dec + String(frac).padStart(exp, '0');
	}

	function settings(input) {
		return { dec: input.dataset.decimal || '.', exp: Number(input.dataset.exponent || 0), cur: input.dataset.currency };
	}

	function preview(form) {
		var input = amountOf(form);
		var out = form.querySelector('[data-keypad-preview]');
		var clear = form.querySelector('[data-key="clear"]');
		if (clear) clear.hidden = input.value === '';
		var mirror = form.querySelector('[data-amount-text]');
		if (mirror) mirror.textContent = input.value || input.placeholder;
		var save = form.querySelector('[data-tx-save]');
		if (save) save.disabled = !/[1-9]/.test(input.value);
		if (!out) return;
		var s = settings(input);
		var hasOp = OPS.test(input.value.replace(/^\s*-/, ''));
		var r = hasOp ? evaluate(input.value, s.dec, s.exp) : null;
		if (r === null) { out.textContent = ''; return; }
		var fmt = new Intl.NumberFormat(document.documentElement.lang || undefined, {
			style: 'currency', currency: s.cur, minimumFractionDigits: r % pow(s.exp) ? s.exp : 0, maximumFractionDigits: s.exp
		});
		out.textContent = '= ' + fmt.format(r / pow(s.exp));
	}

	function press(form, key) {
		var input = amountOf(form);
		var s = settings(input), v = input.value;
		var lastNum = v.split(OPS).pop();
		if (key === 'clear') v = '';
		else if (key === 'back') v = v.slice(0, -1);
		else if (key === 'dec') { if (s.exp > 0 && lastNum.indexOf(s.dec) < 0) v += (lastNum === '' ? '0' : '') + s.dec; }
		else if (OPS.test(key)) { if (v !== '') v = (OPS.test(v.slice(-1)) ? v.slice(0, -1) : v) + key; }
		else v += key;
		input.value = v;
		input.dispatchEvent(new Event('input', { bubbles: true }));
	}

	function syncWallet(form) {
		var select = form.querySelector('[name=wallet_id]');
		var opt = select && select.selectedOptions[0];
		var input = amountOf(form);
		if (!opt || !opt.dataset.currency) return;
		if (input.dataset.exponent !== opt.dataset.exponent) input.value = '';
		input.dataset.currency = opt.dataset.currency;
		input.dataset.exponent = opt.dataset.exponent;
		var slot = form.querySelector('[data-tx-symbol-slot]');
		if (slot) slot.textContent = opt.dataset.txSymbol || '';
		var none = opt.dataset.exponent === '0';
		form.querySelectorAll('[data-key="dec"]').forEach(function (b) { b.hidden = none; });
		form.querySelectorAll('[data-key="000"]').forEach(function (b) { b.hidden = !none; });
		preview(form);
	}

	function mount() {
		document.querySelectorAll('form[data-tx-sheet]').forEach(function (form) {
			var pad = form.querySelector('[data-keypad]');
			var input = amountOf(form);
			if (!pad || !input || form.dataset.keypadMounted) return;
			form.dataset.keypadMounted = '1';
			pad.hidden = false;
			if (coarse.matches) input.setAttribute('inputmode', 'none');
			syncWallet(form);
		});
	}

	document.addEventListener('click', function (e) {
		var key = e.target.closest && e.target.closest('[data-key]');
		var form = key && key.closest('form[data-tx-sheet]');
		if (!form) return;
		press(form, key.dataset.key);
	});

	document.addEventListener('input', function (e) {
		if (e.target.matches && e.target.matches('[data-keypad-input]')) preview(e.target.form);
	});

	document.addEventListener('change', function (e) {
		if (e.target.matches && e.target.matches('form[data-tx-sheet] [name=wallet_id]')) syncWallet(e.target.form);
	});

	document.addEventListener('submit', function (e) {
		var input = amountOf(e.target.matches && e.target.matches('form[data-tx-sheet]') ? e.target : null);
		if (!input || !OPS.test(input.value.replace(/^\s*-/, ''))) return;
		var s = settings(input);
		var r = evaluate(input.value, s.dec, s.exp);
		if (r !== null) input.value = render(r, s.dec, s.exp);
	}, true);

	document.addEventListener('htmx:after:swap', mount);
	if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', mount);
	else mount();
})();
