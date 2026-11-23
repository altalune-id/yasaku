export interface SankeyInputNode {
  id: string;
  column: number;
}

export interface SankeyInputLink {
  source: string;
  target: string;
  value: number;
}

export interface SankeyBox {
  width: number;
  height: number;
  nodeWidth: number;
  nodeGap: number;
  columnX: number[];
}

export interface SankeyNode extends SankeyInputNode {
  value: number;
  x: number;
  y: number;
  h: number;
}

export interface SankeyLink extends SankeyInputLink {
  w: number;
  y0: number;
  y1: number;
  d: string;
  center: string;
}

const r = (n: number) => Math.round(n * 100) / 100;

export function layoutSankey(
  inputNodes: SankeyInputNode[],
  inputLinks: SankeyInputLink[],
  box: SankeyBox,
): { nodes: SankeyNode[]; links: SankeyLink[] } {
  const sum = (id: string, side: "source" | "target") =>
    inputLinks.filter((l) => l[side] === id).reduce((s, l) => s + l.value, 0);
  const columns = [...new Set(inputNodes.map((n) => n.column))].sort((a, b) => a - b);
  const valued = inputNodes.map((n) => ({
    ...n,
    value: Math.max(sum(n.id, "source"), sum(n.id, "target")),
  }));

  const scale = Math.min(
    ...columns.map((c) => {
      const col = valued.filter((n) => n.column === c);
      return (box.height - box.nodeGap * (col.length - 1)) / col.reduce((s, n) => s + n.value, 0);
    }),
  );

  const nodes: SankeyNode[] = [];
  for (const c of columns) {
    const col = valued.filter((n) => n.column === c);
    const total = col.reduce((s, n) => s + n.value * scale, 0) + box.nodeGap * (col.length - 1);
    let y = (box.height - total) / 2;
    for (const n of col) {
      const h = n.value * scale;
      nodes.push({ ...n, x: box.columnX[c] ?? 0, y, h });
      y += h + box.nodeGap;
    }
  }

  const byId = new Map(nodes.map((n) => [n.id, n]));
  const outOffset = new Map(nodes.map((n) => [n.id, n.y]));
  const inOffset = new Map(nodes.map((n) => [n.id, n.y]));
  const ordered = [...inputLinks].sort(
    (a, b) =>
      byId.get(a.source)!.y - byId.get(b.source)!.y ||
      byId.get(a.target)!.y - byId.get(b.target)!.y,
  );

  const links = ordered.map((l) => {
    const s = byId.get(l.source)!;
    const t = byId.get(l.target)!;
    const w = l.value * scale;
    const y0 = outOffset.get(s.id)!;
    const y1 = inOffset.get(t.id)!;
    outOffset.set(s.id, y0 + w);
    inOffset.set(t.id, y1 + w);
    const x0 = s.x + box.nodeWidth;
    const x1 = t.x;
    const xm = (x0 + x1) / 2;
    const d = `M${r(x0)} ${r(y0)} C${r(xm)} ${r(y0)} ${r(xm)} ${r(y1)} ${r(x1)} ${r(y1)} L${r(x1)} ${r(y1 + w)} C${r(xm)} ${r(y1 + w)} ${r(xm)} ${r(y0 + w)} ${r(x0)} ${r(y0 + w)} Z`;
    const c0 = y0 + w / 2;
    const c1 = y1 + w / 2;
    const center = `M${r(x0)} ${r(c0)} C${r(xm)} ${r(c0)} ${r(xm)} ${r(c1)} ${r(x1)} ${r(c1)}`;
    return { ...l, w, y0, y1, d, center };
  });

  return { nodes, links };
}
