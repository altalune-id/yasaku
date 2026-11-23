import { incomeFlows, moneyIn, moneyOut, spendFlows, spending } from "./data";
import { layoutSankey, type SankeyInputLink, type SankeyInputNode } from "./sankey";

export const SANKEY_BOX = {
  width: 800,
  height: 400,
  nodeWidth: 12,
  nodeGap: 16,
  columnX: [112, 250, 400, 560],
};

export function sankeyGraph(): { nodes: SankeyInputNode[]; links: SankeyInputLink[] } {
  const spentFrom = (wallet: string) =>
    spendFlows.filter((f) => f.wallet === wallet).reduce((s, f) => s + f.amount, 0);
  const nodes: SankeyInputNode[] = [
    ...[...new Set(incomeFlows.map((f) => f.from))].map((id) => ({ id: `in:${id}`, column: 0 })),
    { id: "w:bank", column: 1 },
    { id: "w:cash", column: 2 },
    { id: "w:ewallet", column: 2 },
    ...spending.map((s) => ({ id: `c:${s.key}`, column: 3 })),
    { id: "saved", column: 3 },
  ];
  const links: SankeyInputLink[] = [
    ...incomeFlows.map((f) => ({
      source: `in:${f.from}`,
      target: `w:${f.wallet}`,
      value: f.amount,
    })),
    { source: "w:bank", target: "w:cash", value: spentFrom("cash") },
    { source: "w:bank", target: "w:ewallet", value: spentFrom("ewallet") },
    ...spendFlows.map((f) => ({ source: `w:${f.wallet}`, target: `c:${f.to}`, value: f.amount })),
    { source: "w:bank", target: "saved", value: moneyIn - moneyOut },
  ];
  return { nodes, links };
}

export function sankeyLayout() {
  const { nodes, links } = sankeyGraph();
  return layoutSankey(nodes, links, SANKEY_BOX);
}
