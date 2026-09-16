/* charts.js — draws each <div data-chart="donut|bars|sankey"> from the
   <script type="application/json" data-chart-for="<id>"> that follows it. */
(function () {
	'use strict';

	var DONUT_SLOTS = ['--chart-1', '--chart-4', '--chart-2', '--chart-5', '--chart-3'];
	var FALLBACK_SLOT = '--muted-foreground';
	var instances = [];
	var resizeQueued = false;

	function token(name) {
		var raw = getComputedStyle(document.documentElement).getPropertyValue(name).trim();
		if (!raw) return '';
		if (raw.charAt(0) === '#' || raw.indexOf('(') !== -1) return raw;
		return 'hsl(' + raw + ')';
	}

	function theme() {
		return {
			series: DONUT_SLOTS.map(token),
			income: token('--chart-2'),
			expense: token('--chart-4'),
			net: token('--chart-1'),
			fallback: token(FALLBACK_SLOT),
			ink: token('--foreground'),
			muted: token('--muted-foreground'),
			border: token('--border'),
			surface: token('--card') || token('--background'),
		};
	}

	function seriesColor(t, i) {
		return i < t.series.length ? t.series[i] : t.fallback;
	}

	function reducedMotion() {
		return window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches;
	}

	function lang() {
		return document.documentElement.getAttribute('lang') || 'en-US';
	}

	function axisFormatter(unit) {
		var divisor = unit && unit.divisor > 0 ? unit.divisor : 1;
		var symbol = (unit && unit.symbol) || '';
		var nf = new Intl.NumberFormat(lang(), { notation: 'compact', maximumFractionDigits: 1 });
		return function (v) {
			return symbol + nf.format(v / divisor);
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

	function emptyState(el, message) {
		el.textContent = '';
		var p = document.createElement('p');
		p.className = 'flex h-full min-h-64 items-center justify-center text-sm text-muted-foreground';
		p.textContent = message || '';
		el.appendChild(p);
	}

	function baseTooltip(t) {
		return {
			trigger: 'item',
			borderColor: t.border,
			borderWidth: 1,
			backgroundColor: t.surface,
			textStyle: { color: t.ink, fontSize: 12 },
			extraCssText: 'box-shadow:0 4px 12px rgba(0,0,0,.12);border-radius:6px;',
		};
	}

	function donutOption(p, t) {
		var items = p.items || [];
		var data = items.map(function (it, i) {
			return {
				name: it.name,
				value: it.value,
				formatted: it.formatted,
				itemStyle: { color: seriesColor(t, i), borderColor: t.surface, borderWidth: 2 },
			};
		});
		return {
			animation: !reducedMotion(),
			tooltip: Object.assign(baseTooltip(t), {
				formatter: function (params) {
					var v = params.data.formatted || params.value;
					return params.name + '<br/><b>' + v + '</b> (' + params.percent + '%)';
				},
			}),
			legend: {
				bottom: 0,
				icon: 'circle',
				itemWidth: 8,
				itemHeight: 8,
				textStyle: { color: t.muted, fontSize: 12 },
			},
			series: [
				{
					type: 'pie',
					radius: ['55%', '80%'],
					center: ['50%', '45%'],
					avoidLabelOverlap: true,
					padAngle: 1,
					label: {
						show: true,
						color: t.ink,
						fontSize: 12,
						formatter: function (params) {
							return params.name + '\n' + (params.data.formatted || params.value);
						},
					},
					labelLine: { length: 8, length2: 8, lineStyle: { color: t.border } },
					emphasis: { scale: false, itemStyle: { opacity: 0.85 } },
					data: data,
				},
			],
		};
	}

	function barsOption(p, t) {
		var fmt = p.formatted || {};
		var labels = p.labels || {};
		var axis = axisFormatter(p.unit);
		function bar(key, color) {
			return {
				name: labels[key] || key,
				type: 'bar',
				data: p[key] || [],
				barGap: '10%',
				itemStyle: { color: color, borderRadius: [4, 4, 0, 0] },
				emphasis: { itemStyle: { opacity: 0.85 } },
			};
		}
		return {
			animation: !reducedMotion(),
			grid: { left: 8, right: 8, top: 24, bottom: 8, containLabel: true },
			tooltip: Object.assign(baseTooltip(t), {
				trigger: 'axis',
				axisPointer: { type: 'shadow' },
				formatter: function (params) {
					var keys = ['income', 'expense', 'net'];
					var out = params.length ? params[0].axisValue : '';
					params.forEach(function (s, i) {
						var series = fmt[keys[s.seriesIndex]] || fmt[keys[i]];
						var v = at(series, s.dataIndex);
						out += '<br/>' + s.marker + s.seriesName + ' <b>' + (v === null ? s.value : v) + '</b>';
					});
					return out;
				},
			}),
			legend: {
				top: 0,
				icon: 'circle',
				itemWidth: 8,
				itemHeight: 8,
				textStyle: { color: t.muted, fontSize: 12 },
			},
			xAxis: {
				type: 'category',
				data: p.categories || [],
				axisLine: { lineStyle: { color: t.border } },
				axisTick: { show: false },
				axisLabel: { color: t.muted, fontSize: 11 },
			},
			yAxis: {
				type: 'value',
				splitLine: { lineStyle: { color: t.border, opacity: 0.4 } },
				axisLabel: { color: t.muted, fontSize: 11, formatter: axis },
			},
			series: [
				bar('income', t.income),
				bar('expense', t.expense),
				{
					name: labels.net || 'net',
					type: 'line',
					data: p.net || [],
					lineStyle: { width: 2, color: t.net },
					itemStyle: { color: t.net, borderColor: t.surface, borderWidth: 2 },
					symbolSize: 8,
					smooth: false,
				},
			],
		};
	}

	function sankeyOption(p, t) {
		var nodes = (p.nodes || p.data || []).map(function (n, i) {
			return { name: n.name, itemStyle: { color: n.color || seriesColor(t, i) } };
		});
		var links = (p.links || []).map(function (l) {
			return {
				source: l.source,
				target: l.target,
				value: l.value,
				formatted: l.formatted,
				lineStyle: { color: 'gradient', opacity: 0.35 },
			};
		});
		return {
			animation: !reducedMotion(),
			tooltip: Object.assign(baseTooltip(t), {
				formatter: function (params) {
					if (params.dataType === 'edge') {
						var v = params.data.formatted || params.value;
						return params.data.source + ' → ' + params.data.target + '<br/><b>' + v + '</b>';
					}
					return params.name;
				},
			}),
			series: [
				{
					type: 'sankey',
					data: nodes,
					links: links,
					nodeAlign: 'left',
					nodeGap: 10,
					nodeWidth: 14,
					emphasis: { focus: 'adjacency' },
					label: { color: t.ink, fontSize: 12 },
					lineStyle: { color: 'gradient', opacity: 0.35 },
				},
			],
		};
	}

	function isEmpty(kind, p) {
		if (kind === 'donut') return !p.items || p.items.length === 0;
		if (kind === 'bars') return !p.categories || p.categories.length === 0;
		if (kind === 'sankey') return !p.links || p.links.length === 0;
		return true;
	}

	function build(kind, p, t) {
		if (kind === 'donut') return donutOption(p, t);
		if (kind === 'bars') return barsOption(p, t);
		if (kind === 'sankey') return sankeyOption(p, t);
		return null;
	}

	function render(el) {
		var kind = el.getAttribute('data-chart');
		var existing = window.echarts.getInstanceByDom(el);
		if (existing) {
			existing.dispose();
			instances = instances.filter(function (c) {
				return c !== existing;
			});
		}
		var p = payloadFor(el);
		if (!p) return;
		if (isEmpty(kind, p)) {
			emptyState(el, p.empty);
			return;
		}
		var option = build(kind, p, theme());
		if (!option) return;
		var chart = window.echarts.init(el, null, { renderer: 'canvas' });
		chart.setOption(option);
		instances.push(chart);
	}

	function renderAll() {
		if (!window.echarts) return;
		instances = instances.filter(function (c) {
			if (c.isDisposed()) return false;
			if (!c.getDom().isConnected) {
				c.dispose();
				return false;
			}
			return true;
		});
		document.querySelectorAll('[data-chart]').forEach(render);
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
	document.body.addEventListener('htmx:afterSettle', renderAll);
	window.addEventListener('resize', resizeAll);

	new MutationObserver(renderAll).observe(document.documentElement, {
		attributes: true,
		attributeFilter: ['class', 'data-theme', 'data-color-mode'],
	});
	if (window.matchMedia) {
		window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', renderAll);
	}
})();
