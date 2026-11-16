(function () {
	'use strict';

	if (window.__altDropdown) return;
	window.__altDropdown = 1;

	var menuEscape = null;

	function close(menu, focus) {
		menu.removeAttribute('open');
		if (!focus) return;
		var summary = menu.querySelector(':scope > summary');
		if (summary) summary.focus();
	}

	document.addEventListener('click', function (e) {
		document.querySelectorAll('details.dropdown[open]').forEach(function (menu) {
			if (!menu.contains(e.target)) close(menu, false);
		});
	});

	document.addEventListener('keydown', function (e) {
		if (e.key !== 'Escape') return;
		document.querySelectorAll('details.dropdown[open]').forEach(function (menu) {
			menuEscape = e;
			close(menu, menu.contains(document.activeElement));
		});
	});

	var drawerOpener = null;

	function drawerToggle(opener) {
		var toggle = document.getElementById(opener.htmlFor);
		if (!toggle) return;
		toggle.checked = !toggle.checked;
		toggle.dispatchEvent(new Event('change', { bubbles: true }));
		if (!toggle.checked) return;
		drawerOpener = opener;
		var first = document.querySelector('.drawer-side a[href], .drawer-side summary, .drawer-side button');
		var tries = 20;
		(function focusFirst() {
			if (!first || !toggle.checked || document.activeElement === first || tries-- <= 0) return;
			first.focus();
			requestAnimationFrame(focusFirst);
		})();
	}

	function opener(e) {
		return e.target instanceof Element && e.target.closest('label[data-drawer-toggle]');
	}

	document.addEventListener('change', function (e) {
		var toggle = e.target;
		if (!(toggle instanceof HTMLInputElement) || !toggle.id) return;
		document.querySelectorAll('label[data-drawer-toggle]').forEach(function (label) {
			if (label.htmlFor === toggle.id) label.setAttribute('aria-expanded', toggle.checked ? 'true' : 'false');
		});
	});

	document.addEventListener('keydown', function (e) {
		var o = opener(e);
		if (o && (e.key === 'Enter' || e.key === ' ')) {
			e.preventDefault();
			if (e.key === 'Enter' && !e.repeat) drawerToggle(o);
			return;
		}
		if (e.key !== 'Escape' || menuEscape === e || document.querySelector('dialog[open]')) return;
		var open = document.querySelector('input.drawer-toggle:checked');
		if (!open) return;
		open.checked = false;
		open.dispatchEvent(new Event('change', { bubbles: true }));
		var back = [drawerOpener].concat(Array.from(document.querySelectorAll('label[data-drawer-toggle]'))).find(function (l) {
			return l && l.htmlFor === open.id && l.isConnected && l.getClientRects().length > 0;
		});
		(back || document.getElementById('main')).focus();
		drawerOpener = null;
	});

	document.addEventListener('keyup', function (e) {
		var o = opener(e);
		if (o && e.key === ' ') drawerToggle(o);
	});
	document.addEventListener(
		'toggle',
		function (e) {
			var menu = e.target;
			if (!(menu instanceof HTMLDetailsElement) || !menu.open || !menu.classList.contains('dropdown')) return;
			document.querySelectorAll('details.dropdown[open]').forEach(function (other) {
				if (other !== menu && !other.contains(menu)) close(other, false);
			});
		},
		true
	);
})();
