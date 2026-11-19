(function () {
	'use strict';

	if (window.__yasakuCharts) return;
	window.__yasakuCharts = 1;

	var ENTITIES = { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' };
	var instances = [];
	var resizeQueued = false;
	var pixel = null;

	// SECURITY: echarts assigns a string tooltip formatter's return value to innerHTML, so every
	// payload-derived value (category, wallet and period names are user input) must be escaped here.
	function esc(v) {
		return String(v == null ? '' : v).replace(/[&<>"']/g, function (c) {
			return ENTITIES[c];
		});
	}

	function rgba(value) {
		var probe = document.createElement('span');
		probe.style.display = 'none';
		probe.style.color = value;
		document.body.appendChild(probe);
		var computed = getComputedStyle(probe).color;
		probe.remove();
		if (!pixel) {
			var canvas = document.createElement('canvas');
			canvas.width = canvas.height = 1;
			pixel = canvas.getContext('2d', { willReadFrequently: true });
		}
		pixel.clearRect(0, 0, 1, 1);
		pixel.fillStyle = computed;
		pixel.fillRect(0, 0, 1, 1);
		var d = pixel.getImageData(0, 0, 1, 1).data;
		return 'rgba(' + d[0] + ',' + d[1] + ',' + d[2] + ',' + (d[3] / 255).toFixed(3) + ')';
	}

	function cssVar(name) {
		return /^--color-[a-z0-9-]+$/.test(name || '') ? rgba('var(' + name + ')') : '';
	}

	function mix(name, pct) {
		return rgba('color-mix(in oklab, var(' + name + ') ' + pct + '%, var(--color-base-100))');
	}

	function theme() {
		var muted = rgba('color-mix(in oklab, var(--color-base-content) 70%, transparent)');
		return {
			income: cssVar('--color-success'),
			expense: cssVar('--color-error'),
			net: cssVar('--color-primary'),
			primary: cssVar('--color-primary'),
			soft: mix('--color-primary', 45),
			faint: mix('--color-primary', 40),
			neutral: cssVar('--color-neutral'),
			ink: rgba('var(--color-base-content)'),
			muted: muted,
			border: rgba('var(--color-base-300)'),
			surface: rgba('var(--color-base-100)'),
			font: getComputedStyle(document.documentElement).getPropertyValue('--font-sans').trim() || 'sans-serif',
		};
	}

	function reducedMotion() {
		return window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches;
	}

	function lang() {
		return document.documentElement.getAttribute('lang') || 'en-US';
	}

	function rtl() {
		return document.documentElement.getAttribute('dir') === 'rtl';
	}

	function axisFormatter(unit) {
		var divisor = unit && unit.divisor > 0 ? unit.divisor : 1;
		var symbol = (unit && unit.symbol) || '';
		var nf = new Intl.NumberFormat(lang(), { notation: 'compact', maximumFractionDigits: 1 });
		return function (v) {
			var sign = v < 0 ? '−' : '';
			return sign + symbol + nf.format(Math.abs(v) / divisor);
		};
	}

	function at(arr, i) {
		return Array.isArray(arr) && i < arr.length ? arr[i] : null;
	}

	function payloadFor(el) {
		var holder = null;
		var next = el.nextElementSibling;
		if (next && next.matches('script[type="application/json"]') && next.getAttribute('data-chart-for') === el.id) {
			holder = next;
		} else {
			var all = document.querySelectorAll('script[type="application/json"][data-chart-for]');
			for (var i = 0; i < all.length; i++) {
				if (all[i].getAttribute('data-chart-for') === el.id) {
					holder = all[i];
					break;
				}
			}
		}
		if (!holder) return null;
		try {
			return JSON.parse(holder.textContent);
		} catch (e) {
			return null;
		}
	}

	function baseOption(t) {
		return { animation: !reducedMotion(), textStyle: { fontFamily: t.font } };
	}

	function baseTooltip(t) {
		return {
			trigger: 'item',
			borderColor: t.border,
			borderWidth: 1,
			backgroundColor: t.surface,
			textStyle: { color: t.ink, fontSize: 12, fontFamily: t.font },
			extraCssText: 'box-shadow:0 4px 12px rgba(0,0,0,.12);border-radius:6px;',
		};
	}

	function axisLabel(t, extra) {
		return Object.assign({ color: t.muted, fontSize: 12, fontFamily: t.font }, extra || {});
	}

	function categoryAxis(t, data, extra) {
		return Object.assign(
			{
				type: 'category',
				data: data || [],
				inverse: rtl(),
				axisLine: { lineStyle: { color: t.border } },
				axisTick: { show: false },
				axisLabel: axisLabel(t, { interval: 0, rotate: 0, width: 48, overflow: 'truncate' }),
			},
			extra || {}
		);
	}

	function valueAxis(t, unit) {
		return {
			type: 'value',
			position: rtl() ? 'right' : 'left',
			splitNumber: 3,
			splitLine: { lineStyle: { color: t.border } },
			axisLabel: axisLabel(t, { formatter: axisFormatter(unit) }),
		};
	}

	function legend(t) {
		var side = rtl() ? { right: 0 } : { left: 0 };
		return Object.assign({ top: 0, icon: 'circle', itemWidth: 8, itemHeight: 8, itemGap: 16, textStyle: { color: t.muted, fontSize: 12, fontFamily: t.font } }, side);
	}

	function donutOption(p, t) {
		var data = (p.items || []).map(function (it) {
			return {
				name: it.name,
				value: it.value,
				formatted: it.formatted,
				itemStyle: { color: cssVar(it.color) || t.neutral, borderColor: t.surface, borderWidth: 2 },
			};
		});
		return Object.assign(baseOption(t), {
			tooltip: Object.assign(baseTooltip(t), {
				formatter: function (params) {
					return esc(params.name) + '<br/><b>' + esc(params.data.formatted || params.value) + '</b>';
				},
			}),
			series: [
				{
					type: 'pie',
					radius: ['68%', '96%'],
					center: ['50%', '50%'],
					label: { show: false },
					labelLine: { show: false },
					emphasis: { scale: false },
					data: data,
				},
			],
		});
	}

	function barsOption(p, t) {
		var fmt = p.formatted || {};
		var labels = p.labels || {};
		var full = p.full || [];
		function bar(key, color) {
			return {
				name: labels[key] || key,
				type: 'bar',
				itemStyle: { color: color },
				barMaxWidth: 12,
				barGap: '10%',
				barCategoryGap: '30%',
				data: (p[key] || []).map(function (v) {
					return { value: v, itemStyle: { color: color, borderRadius: v < 0 ? [0, 0, 4, 4] : [4, 4, 0, 0] } };
				}),
			};
		}
		return Object.assign(baseOption(t), {
			grid: { left: 0, right: 0, top: 32, bottom: 0, containLabel: true },
			tooltip: Object.assign(baseTooltip(t), {
				trigger: 'axis',
				axisPointer: { type: 'shadow' },
				formatter: function (params) {
					var keys = ['income', 'expense', 'net'];
					var i = params.length ? params[0].dataIndex : 0;
					var out = esc(at(full, i) || (params.length ? params[0].axisValue : ''));
					params.forEach(function (s) {
						var v = at(fmt[keys[s.seriesIndex]], s.dataIndex);
						out += '<br/>' + s.marker + esc(s.seriesName) + ' <b>' + esc(v === null ? s.value : v) + '</b>';
					});
					return out;
				},
			}),
			legend: legend(t),
			xAxis: categoryAxis(t, p.categories),
			yAxis: valueAxis(t, p.unit),
			series: [bar('income', t.income), bar('expense', t.expense), bar('net', t.net)],
		});
	}

	function trendOption(p, t) {
		var current = typeof p.current === 'number' ? p.current : -1;
		var fmt = p.formatted || [];
		return Object.assign(baseOption(t), {
			grid: { left: 0, right: 0, top: 8, bottom: 0, containLabel: true },
			tooltip: Object.assign(baseTooltip(t), {
				trigger: 'axis',
				axisPointer: { type: 'shadow' },
				formatter: function (params) {
					var s = params[0];
					return esc(s.axisValue) + '<br/><b>' + esc(at(fmt, s.dataIndex) || s.value) + '</b>';
				},
			}),
			xAxis: categoryAxis(
				t,
				(p.labels || []).map(function (v, i) {
					return i === current ? { value: v, textStyle: { fontWeight: 600, color: t.ink } } : v;
				})
			),
			yAxis: valueAxis(t, p.unit),
			series: [
				{
					type: 'bar',
					barMaxWidth: 28,
					data: (p.values || []).map(function (v, i) {
						return { value: v, itemStyle: { color: i === current ? t.primary : t.soft, borderRadius: [4, 4, 0, 0] } };
					}),
				},
			],
		});
	}

	function compareOption(p, t) {
		var fmt = p.formatted || {};
		var labels = p.labels || {};
		function series(key, color) {
			return { name: labels[key] || key, type: 'bar', barMaxWidth: 12, barGap: '20%', itemStyle: { color: color, borderRadius: rtl() ? [4, 0, 0, 4] : [0, 4, 4, 0] }, data: p[key] || [] };
		}
		return Object.assign(baseOption(t), {
			grid: { left: 0, right: 0, top: 32, bottom: 0, containLabel: true },
			tooltip: Object.assign(baseTooltip(t), {
				trigger: 'axis',
				axisPointer: { type: 'shadow' },
				formatter: function (params) {
					var keys = ['previous', 'current'];
					var out = params.length ? esc(params[0].axisValue) : '';
					params.forEach(function (s) {
						var v = at(fmt[keys[s.seriesIndex]], s.dataIndex);
						out += '<br/>' + s.marker + esc(s.seriesName) + ' <b>' + esc(v === null ? s.value : v) + '</b>';
					});
					return out;
				},
			}),
			legend: Object.assign(legend(t), { itemWidth: 12, itemHeight: 12, icon: 'roundRect' }),
			xAxis: (function () {
				var x = Object.assign(valueAxis(t, p.unit), { position: 'bottom', inverse: rtl() });
				x.axisLabel = Object.assign(x.axisLabel, { alignMinLabel: rtl() ? 'right' : 'left', alignMaxLabel: rtl() ? 'left' : 'right' });
				return x;
			})(),
			yAxis: {
				type: 'category',
				inverse: true,
				position: rtl() ? 'right' : 'left',
				data: p.categories || [],
				axisLine: { lineStyle: { color: t.border } },
				axisTick: { show: false },
				axisLabel: axisLabel(t, { width: 88, overflow: 'truncate' }),
			},
			series: [series('previous', t.faint), series('current', t.primary)],
		});
	}

	function sankeyOption(p, el, t) {
		var flip = rtl();
		var nodes = (p.nodes || []).map(function (n) {
			return { name: n.name, itemStyle: { color: cssVar(n.color) || t.muted } };
		});
		var links = (p.links || []).map(function (l) {
			return {
				source: flip ? l.target : l.source,
				target: flip ? l.source : l.target,
				value: l.value,
				tooltip: l.tooltip,
			};
		});
		var width = el.clientWidth < 640 ? 120 : 160;
		var label = function (pos) {
			return { position: pos, color: t.ink, fontSize: 12, lineHeight: 14, fontFamily: t.font, width: width, overflow: 'truncate', textBorderColor: t.surface, textBorderWidth: 3 };
		};
		return Object.assign(baseOption(t), {
			tooltip: Object.assign(baseTooltip(t), {
				formatter: function (params) {
					if (params.dataType === 'edge') return esc(params.data.tooltip || params.value);
					return esc(params.name);
				},
			}),
			series: [
				{
					type: 'sankey',
					left: 0,
					right: 0,
					top: 8,
					bottom: 8,
					data: nodes,
					links: links,
					nodeAlign: 'justify',
					nodeWidth: 12,
					nodeGap: 16,
					draggable: false,
					layoutIterations: 32,
					levels: [
						{ depth: 0, label: label('right') },
						{ depth: 1, label: label('left') },
					],
					label: label('right'),
					lineStyle: { color: flip ? 'source' : 'target', opacity: 0.35 },
					emphasis: { focus: 'adjacency', lineStyle: { opacity: 0.6 } },
				},
			],
		});
	}

	function build(kind, p, el, t) {
		if (kind === 'donut') return donutOption(p, t);
		if (kind === 'bars') return barsOption(p, t);
		if (kind === 'sankey') return sankeyOption(p, el, t);
		if (kind === 'trend') return trendOption(p, t);
		if (kind === 'compare') return compareOption(p, t);
		return null;
	}

	function render(el) {
		var existing = window.echarts.getInstanceByDom(el);
		if (existing) {
			existing.dispose();
			instances = instances.filter(function (c) {
				return c !== existing;
			});
		}
		if (el.clientWidth === 0) return;
		var p = payloadFor(el);
		if (!p) return;
		var option = build(el.getAttribute('data-chart'), p, el, theme());
		if (!option) return;
		var chart = window.echarts.init(el, null, { renderer: 'canvas' });
		chart.setOption(option);
		instances.push(chart);
	}

	function prune() {
		instances = instances.filter(function (c) {
			if (c.isDisposed()) return false;
			if (!c.getDom().isConnected) {
				c.dispose();
				return false;
			}
			return true;
		});
	}

	function renderAll() {
		if (!window.echarts) return;
		document.querySelectorAll('[data-needs-js]').forEach(function (el) {
			el.hidden = false;
		});
		prune();
		document.querySelectorAll('[data-chart]').forEach(render);
		prune();
	}

	function resizeAll() {
		if (resizeQueued) return;
		resizeQueued = true;
		window.requestAnimationFrame(function () {
			resizeQueued = false;
			instances.forEach(function (c) {
				if (!c.isDisposed()) c.resize();
			});
		});
	}

	if (document.readyState === 'loading') {
		document.addEventListener('DOMContentLoaded', renderAll);
	} else {
		renderAll();
	}
	document.body.addEventListener('htmx:after:settle', renderAll);
	window.addEventListener('resize', resizeAll);

	new MutationObserver(renderAll).observe(document.documentElement, {
		attributes: true,
		attributeFilter: ['data-theme', 'data-family', 'dir', 'lang'],
	});
	new MutationObserver(function (records) {
		if (!window.echarts) return;
		records.forEach(function (r) {
			if (r.target.open) r.target.querySelectorAll('[data-chart]').forEach(render);
		});
	}).observe(document.body, { subtree: true, attributes: true, attributeFilter: ['open'] });
	if (window.matchMedia) {
		window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', renderAll);
	}
})();
