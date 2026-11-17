(function () {
	'use strict';

	if (window.__altUndo) return;
	window.__altUndo = 1;

	function arm(toast) {
		if (toast.dataset.undoArmed) return;
		toast.dataset.undoArmed = '1';
		var left = Number(toast.dataset.undoMs) || 8000;
		var timer = null;
		var started = 0;

		function run() {
			if (timer || toast.matches(':hover, :focus-within')) return;
			started = Date.now();
			timer = setTimeout(function () { toast.remove(); }, left);
		}
		function pause() {
			if (!timer) return;
			clearTimeout(timer);
			timer = null;
			left -= Date.now() - started;
		}
		toast.addEventListener('pointerenter', pause);
		toast.addEventListener('focusin', pause);
		toast.addEventListener('pointerleave', function () { setTimeout(run, 0); });
		toast.addEventListener('focusout', function () { setTimeout(run, 0); });
		run();
	}

	function scan() {
		document.querySelectorAll('[data-undo-toast]').forEach(arm);
	}

	document.addEventListener('htmx:after:swap', scan);
	if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', scan);
	else scan();
})();
