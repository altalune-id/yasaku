(function () {
	'use strict';

	if (window.__altRecent) return;
	window.__altRecent = 1;

	var OPENS = 'yasaku.sheetOpens';
	var SHOWN = 3;

	function kindOf(form) {
		var k = form.querySelector('[name=kind]:checked') || form.querySelector('[name=kind][type=hidden]');
		return k ? k.value : 'expense';
	}

	function categoryOf(form) {
		var c = form.querySelector('[name=category_id]:checked');
		return c ? c.value : '';
	}

	function sync(form) {
		var box = form.querySelector('[data-recent-box]');
		if (!box) return;
		var cat = categoryOf(form), kind = kindOf(form), shown = 0;
		box.querySelectorAll('[data-recent]').forEach(function (b) {
			var match = cat ? b.dataset.category === cat : b.dataset.kind === kind && shown < SHOWN;
			b.hidden = !match;
			if (match) shown++;
		});
		box.hidden = shown === 0;
	}

	function pick(form, name, value) {
		var el = form.querySelector('[name=' + name + '][value="' + CSS.escape(value) + '"]');
		if (el && !el.disabled) el.checked = true;
	}

	function apply(form, url) {
		var q = new URL(url, window.location.href).searchParams;
		pick(form, 'kind', q.get('kind') || 'expense');
		var picked = form.querySelector('[name=category_id]:checked');
		if (picked) picked.checked = false;
		form.removeAttribute('data-category-picked');
		if (q.get('category')) {
			pick(form, 'category_id', q.get('category'));
			form.setAttribute('data-category-picked', '1');
		}
		var wallet = form.querySelector('[name=wallet_id]');
		if (wallet && q.get('wallet') && wallet.querySelector('option[value="' + CSS.escape(q.get('wallet')) + '"]')) {
			wallet.value = q.get('wallet');
			wallet.dispatchEvent(new Event('change', { bubbles: true }));
		}
	}

	function counted(form) {
		var n = 1;
		try {
			n = Number(window.localStorage.getItem(OPENS) || 0) + 1;
			window.localStorage.setItem(OPENS, String(n));
		} catch (_) { /* storage blocked: the tip waits */ }
		var tpl = form.querySelector('template[data-recent-tip]');
		if (n >= 2 && tpl) tpl.replaceWith(tpl.content.cloneNode(true));
		sync(form);
	}

	function fill(form, chip) {
		pick(form, 'category_id', chip.dataset.category);
		form.setAttribute('data-category-picked', '1');
		var wallet = form.querySelector('[name=wallet_id]');
		if (wallet && wallet.querySelector('option[value="' + CSS.escape(chip.dataset.wallet) + '"]')) {
			wallet.value = chip.dataset.wallet;
			wallet.dispatchEvent(new Event('change', { bubbles: true }));
		}
		var amount = form.querySelector('[name=amount]');
		amount.value = chip.dataset.amount;
		amount.dispatchEvent(new Event('input', { bubbles: true }));
		var note = form.querySelector('[name=note]');
		if (note) note.value = chip.dataset.note;
		var save = form.querySelector('button[type=submit]');
		if (save) save.focus();
	}

	document.addEventListener('click', function (e) {
		var chip = e.target.closest && e.target.closest('[data-recent]');
		if (chip) { fill(chip.form, chip); return; }
		var opener = e.target.closest && e.target.closest('[data-dialog-open]');
		var dialog = opener && document.getElementById(opener.getAttribute('data-dialog-open'));
		var form = dialog && dialog.querySelector('form[data-tx-sheet]');
		if (!form) return;
		if (opener.href) apply(form, opener.href);
		counted(form);
	}, true);

	document.addEventListener('change', function (e) {
		var form = e.target.form;
		if (!form || !form.matches('[data-tx-sheet]')) return;
		if (e.target.name === 'category_id') form.setAttribute('data-category-picked', '1');
		if (e.target.name === 'kind') {
			var c = form.querySelector('[name=category_id]:checked');
			var grid = c && c.closest('[data-cat-kind]');
			if (grid && grid.dataset.catKind !== e.target.value) c.checked = false;
		}
		sync(form);
	});

	function keepPick(e) {
		var ctx = e.detail && e.detail.ctx;
		var target = ctx && ctx.target;
		var form = target && target.id === 'tx-category' && target.closest('form');
		if (form && form.hasAttribute('data-category-picked')) e.preventDefault();
	}
	document.addEventListener('htmx:before:request', keepPick);
	document.addEventListener('htmx:before:swap', keepPick);

	// NOTE: mirrors the server format: a no-break space after a letter symbol ("IDR 25K"), none after a sign ("$25K").
	function compact(form) {
		var nf = new Intl.NumberFormat(document.documentElement.lang || undefined, { notation: 'compact', maximumFractionDigits: 1 });
		form.querySelectorAll('[data-compact]').forEach(function (el) {
			var n = Number(el.dataset.compact), sym = el.dataset.symbol || '';
			if (isFinite(n)) el.textContent = sym + (/\p{L}$/u.test(sym) ? '\u00a0' : '') + nf.format(n);
		});
	}

	function mount() {
		document.querySelectorAll('form[data-tx-sheet]').forEach(function (form) {
			if (form.dataset.recentMounted) return;
			form.dataset.recentMounted = '1';
			compact(form);
			if (!form.closest('dialog')) counted(form);
			else sync(form);
		});
	}
	document.addEventListener('htmx:after:swap', function () {
		document.querySelectorAll('form[data-tx-sheet]').forEach(sync);
		mount();
	});
	if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', mount);
	else mount();
})();
