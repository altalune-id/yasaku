// NOTE: the vendored Lit core exports no svg tag; this is lit-html's own svg tag (result type 2), so a fragment mapped inside <svg> parses in the SVG namespace.
const chartSvg = (strings, ...values) => ({ _$litType$: 2, strings: strings, values: values });

function donutChart(m) {
  if (!m || m.empty) return nothing;
  return html`<svg class="app-chart" viewBox="0 0 120 120" width="120" height="120" role="img" aria-label="Spend by category">
    <circle cx="60" cy="60" r=${m.r} fill="none" stroke-width="16" stroke="var(--color-background-tertiary, #e4e4e7)"></circle>
    ${m.segments.map((s) => chartSvg`<circle cx="60" cy="60" r=${m.r} fill="none" stroke-width="16"
      stroke=${s.stroke} stroke-dasharray=${s.dash} stroke-dashoffset=${s.offset} transform="rotate(-90 60 60)"></circle>`)}
  </svg>`;
}

function cashflowChart(m) {
  if (!m || m.empty) return nothing;
  return html`<svg class="app-chart" viewBox="0 0 320 120" width="100%" height="120" role="img" aria-label="Cashflow by period">
    ${m.bars.map((b) => chartSvg`<rect x=${b.x} y=${b.y} width=${b.width} height=${b.height} rx="2" fill=${b.fill}></rect>`)}
    ${m.labels.map((l) => chartSvg`<text x=${l.x} y=${l.y} font-size="9" text-anchor="middle" fill="currentColor" opacity="0.7">${l.text}</text>`)}
  </svg>`;
}
