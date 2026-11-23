import { describe, expect, it } from "vite-plus/test";
import { layoutSankey } from "./sankey";

const nodes = [
  { id: "a", column: 0 },
  { id: "b", column: 0 },
  { id: "m", column: 1 },
  { id: "x", column: 2 },
  { id: "y", column: 2 },
];
const links = [
  { source: "a", target: "m", value: 60 },
  { source: "b", target: "m", value: 40 },
  { source: "m", target: "x", value: 70 },
  { source: "m", target: "y", value: 30 },
];
const box = { width: 600, height: 300, nodeWidth: 12, nodeGap: 16, columnX: [100, 300, 500] };

describe("layoutSankey", () => {
  const layout = layoutSankey(nodes, links, box);
  const node = (id: string) => layout.nodes.find((n) => n.id === id)!;

  it("sizes nodes by their larger side and keeps columns inside the box without overlap", () => {
    expect(node("m").value).toBe(100);
    for (const col of [0, 1, 2]) {
      const inCol = layout.nodes.filter((n) => n.column === col).sort((p, q) => p.y - q.y);
      for (const n of inCol) {
        expect(n.y).toBeGreaterThanOrEqual(0);
        expect(n.y + n.h).toBeLessThanOrEqual(box.height + 0.001);
      }
      for (let i = 1; i < inCol.length; i++)
        expect(inCol[i]!.y).toBeGreaterThanOrEqual(inCol[i - 1]!.y + inCol[i - 1]!.h);
    }
  });

  it("stacks each node's ribbons to exactly fill it", () => {
    const m = node("m");
    const outs = layout.links.filter((l) => l.source === "m");
    expect(outs.reduce((s, l) => s + l.w, 0)).toBeCloseTo(m.h);
    expect(Math.min(...outs.map((l) => l.y0))).toBeCloseTo(m.y);
    const ins = layout.links.filter((l) => l.target === "m");
    expect(ins.reduce((s, l) => s + l.w, 0)).toBeCloseTo(m.h);
  });

  it("emits closed ribbon paths and a centerline per link", () => {
    for (const l of layout.links) {
      expect(l.d).toMatch(/^M[\d.\s,-]+C[\d.\s,-]+L[\d.\s,-]+C[\d.\s,-]+Z$/);
      expect(l.center).toMatch(/^M[\d.\s,-]+C[\d.\s,-]+$/);
      expect(l.d).not.toContain("NaN");
    }
  });
});
