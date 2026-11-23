import { describe, expect, it } from "vite-plus/test";
import { sankeyGraph } from "./sankeyModel";
import {
  cashflow,
  incomeFlows,
  moneyIn,
  moneyOut,
  movement,
  share,
  spendFlows,
  spending,
  wallets,
} from "./data";

describe("mock data", () => {
  it("adds up like a real period", () => {
    expect(moneyOut).toBe(6_240_000);
    expect(moneyIn - moneyOut).toBe(3_460_000);
    expect(cashflow.at(-1)).toEqual({ in: moneyIn, out: moneyOut });
    expect(spending.reduce((sum, s) => sum + share(s.amount), 0)).toBe(100);
  });

  it("moves wallets consistently with the period", () => {
    for (const w of movement)
      expect(w.opening + w.into - w.out).toBe(wallets.find((x) => x.key === w.key)!.balance);
    const sum = (k: "into" | "out") => movement.reduce((s, w) => s + w[k], 0);
    expect(sum("into") - sum("out")).toBe(moneyIn - moneyOut);
    const graph = sankeyGraph();
    for (const w of movement) {
      const into = graph.links
        .filter((l) => l.target === `w:${w.key}`)
        .reduce((t, l) => t + l.value, 0);
      const out = graph.links
        .filter((l) => l.source === `w:${w.key}` && l.target !== "saved")
        .reduce((t, l) => t + l.value, 0);
      expect([into, out]).toEqual([w.into, w.out]);
    }
  });

  it("flows money the same way the report and wallet movement do", () => {
    expect(incomeFlows.reduce((s, f) => s + f.amount, 0)).toBe(moneyIn);
    for (const cat of spending) {
      expect(spendFlows.filter((f) => f.to === cat.key).reduce((s, f) => s + f.amount, 0)).toBe(
        cat.amount,
      );
    }
  });

  it("uses six distinct category colors, Other last", () => {
    expect(new Set(spending.map((s) => s.slot)).size).toBe(6);
    expect(spending.at(-1)).toMatchObject({ key: "other", slot: "secondary" });
  });
});
