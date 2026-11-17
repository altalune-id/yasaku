(function () {
	'use strict';

	if (window.__altSwipe) return;
	window.__altSwipe = 1;

	var OPEN_AT = 48;
	var drag = null;
	var suppressClick = false;

	function rtl() {
		return document.documentElement.dir === 'rtl';
	}

	function setOpen(row, open) {
		var front = row.querySelector('[data-swipe-front]');
		var actions = row.querySelector('[data-swipe-actions]');
		front.toggleAttribute('data-swiped', open);
		actions.inert = !open;
	}

	function closeAll(except) {
		document.querySelectorAll('[data-swipe-front][data-swiped]').forEach(function (f) {
			var row = f.closest('[data-swipe]');
			if (row !== except) setOpen(row, false);
		});
	}

	document.addEventListener('pointerdown', function (e) {
		if (e.pointerType === 'mouse') return;
		var front = e.target.closest && e.target.closest('[data-swipe-front]');
		if (!front || e.target.closest('[data-row-menu]')) return;
		drag = { front: front, x: e.clientX, y: e.clientY, dx: 0, moving: false, open: front.hasAttribute('data-swiped') };
	});

	document.addEventListener('pointermove', function (e) {
		if (!drag) return;
		var dx = e.clientX - drag.x, dy = e.clientY - drag.y;
		if (!drag.moving) {
			if (Math.abs(dy) > Math.abs(dx)) { drag = null; return; }
			if (Math.abs(dx) < 8) return;
			drag.moving = true;
			drag.front.closest('[data-swipe]').querySelector('[data-swipe-actions]').inert = false;
		}
		var width = drag.front.closest('[data-swipe]').querySelector('[data-swipe-actions]').offsetWidth;
		var dir = rtl() ? -1 : 1;
		var logical = dx * dir + (drag.open ? -width : 0);
		drag.dx = Math.min(0, Math.max(-width, logical));
		drag.front.style.transition = 'none';
		drag.front.style.transform = 'translateX(' + drag.dx * dir + 'px)';
	});

	function end() {
		if (!drag) return;
		var d = drag;
		drag = null;
		d.front.style.transition = '';
		d.front.style.transform = '';
		if (!d.moving) return;
		suppressClick = true;
		var row = d.front.closest('[data-swipe]');
		var open = -d.dx >= OPEN_AT;
		if (open) closeAll(row);
		setOpen(row, open);
	}
	document.addEventListener('pointerup', end);
	document.addEventListener('pointercancel', end);

	document.addEventListener('click', function (e) {
		if (suppressClick) {
			suppressClick = false;
			e.preventDefault();
			e.stopPropagation();
			return;
		}
		var row = e.target.closest && e.target.closest('[data-swipe]');
		if (!row || !row.querySelector('[data-swipe-actions]').contains(e.target)) closeAll(null);
	}, true);

	document.addEventListener('click', function (e) {
		var opener = e.target.closest && e.target.closest('[data-row-edit]');
		var dialog = opener && document.getElementById(opener.getAttribute('data-dialog-open'));
		if (!dialog) return;
		dialog.querySelector('[data-row-target="edit"]').href = opener.dataset.rowEdit;
		dialog.querySelector('[data-row-target="duplicate"]').href = opener.dataset.rowDuplicate;
		dialog.querySelector('[data-row-target="delete"]').action = opener.dataset.rowDelete;
		var title = dialog.querySelector('#' + CSS.escape(dialog.id) + '-title');
		if (title) title.textContent = opener.dataset.rowTitle;
	}, true);

	document.addEventListener('keydown', function (e) {
		if (e.key === 'Escape') closeAll(null);
	});
})();
