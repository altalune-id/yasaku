(function () {
	'use strict';

	if (window.__altCopy) return;
	window.__altCopy = 1;

	document.addEventListener('click', function (e) {
		var btn = e.target instanceof Element && e.target.closest('[data-copy]');
		if (!btn) return;
		var text = btn.getAttribute('data-copy');
		var shown = btn.getAttribute('data-copy-text') || text;
		var slot = btn.querySelector('[data-copy-slot]') || btn;
		var done = function () {
			slot.textContent = btn.getAttribute('data-copied-label');
			btn.setAttribute('aria-label', btn.getAttribute('data-copied-label'));
			setTimeout(function () {
				slot.textContent = shown;
				btn.setAttribute('aria-label', btn.getAttribute('data-copy-label'));
			}, 1500);
		};
		if (navigator.clipboard && navigator.clipboard.writeText) {
			navigator.clipboard.writeText(text).then(done, function () {});
			return;
		}
		var ta = document.createElement('textarea');
		ta.value = text;
		ta.setAttribute('readonly', '');
		ta.style.position = 'fixed';
		ta.style.opacity = '0';
		document.body.appendChild(ta);
		ta.select();
		try {
			document.execCommand('copy');
			done();
		} catch (err) {}
		document.body.removeChild(ta);
	});
})();
