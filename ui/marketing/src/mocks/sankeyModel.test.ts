import { describe, expect, it } from "vite-plus/test";
import { moneyIn, moneyOut, spending } from "./data";
import { SANKEY_BOX, sankeyGraph, sankeyLayout } from "./sankeyModel";

describe("sankeyGraph", () => {
  const { nodes, links } = sankeyGraph();
  const sum = (id: string, side: "source" | "target") =>
    links.filter((l) => l[side] === id).reduce((s, l) => s + l.value, 0);

  it("balances every wallet: money in equals money out", () => {
    for (const n of nodes.filter((n) => n.id.startsWith("w:")))
      expect(sum(n.id, "target")).toBe(sum(n.id, "source"));
  });

  it("matches the report: income in, spending per category, and savings", () => {
    const incomeOut = nodes
      .filter((n) => n.id.startsWith("in:"))
      .reduce((s, n) => s + sum(n.id, "source"), 0);
    expect(incomeOut).toBe(moneyIn);
    for (const cat of spending) expect(sum(`c:${cat.key}`, "target")).toBe(cat.amount);
    expect(sum("saved", "target")).toBe(moneyIn - moneyOut);
  });

  it("only gives inflow to wallets that receive money", () => {
    for (const id of ["w:cash", "w:ewallet"]) expect(sum(id, "target")).toBeGreaterThan(0);
  });

  it("leaves room for a 12px label line beside every node, inside the svg", () => {
    const layout = sankeyLayout();
    for (const col of [0, 1, 2, 3]) {
      const centers = layout.nodes
        .filter((n) => n.column === col)
        .map((n) => n.y + n.h / 2)
        .sort((a, b) => a - b);
      for (let i = 1; i < centers.length; i++)
        expect(centers[i]! - centers[i - 1]!).toBeGreaterThanOrEqual(15);
      for (const c of centers) {
        expect(c - 8).toBeGreaterThanOrEqual(0);
        expect(c + 8).toBeLessThanOrEqual(SANKEY_BOX.height);
      }
    }
  });
});
