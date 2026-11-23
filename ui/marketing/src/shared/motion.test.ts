import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vite-plus/test";

interface Rule {
  selector: string;
  body: string;
}

function leafRules(css: string): Rule[] {
  const src = css.replace(/\/\*[\s\S]*?\*\//g, "");
  const rules: Rule[] = [];
  const stack: { selector: string; start: number; nested: boolean }[] = [];
  let last = 0;
  for (let i = 0; i < src.length; i++) {
    const ch = src[i];
    if (ch === "{") {
      const parent = stack.at(-1);
      if (parent) parent.nested = true;
      stack.push({
        selector: src.slice(last, i).split(/[;}]/).at(-1)!.trim(),
        start: i + 1,
        nested: false,
      });
      last = i + 1;
    } else if (ch === "}") {
      const block = stack.pop()!;
      const inKeyframes = stack.some((s) => s.selector.startsWith("@keyframes"));
      if (!block.nested && !inKeyframes)
        rules.push({ selector: block.selector, body: src.slice(block.start, i) });
      last = i + 1;
    }
  }
  return rules;
}

function hiddenOutsideInview(source: string): string[] {
  return leafRules(source)
    .filter((r) => /(?:^|;)\s*opacity:\s*0\s*(?:;|$)/.test(r.body))
    .map((r) => r.selector)
    .filter((selector) => !/\[data-inview=["']false["']\]/.test(selector));
}

const css = readFileSync(resolve(import.meta.dirname, "motion.css"), "utf8");

describe("motion.css", () => {
  it("lets the reduced marquee rows shrink and wrap", () => {
    const rows = leafRules(css).filter(
      (r) => /\.marquee-row\s*$/.test(r.selector) && /data-motion/.test(r.selector),
    );
    expect(rows).toHaveLength(2);
    for (const rule of rows) {
      expect(rule.body).toMatch(/flex-shrink:\s*1/);
      expect(rule.body).toMatch(/min-width:\s*0/);
    }
  });

  it("fades the marquee edges, but not the wrapped reduced rows", () => {
    const rules = leafRules(css).filter((r) => /\.marquee\s*$/.test(r.selector));
    const base = rules.filter((r) => !/data-motion/.test(r.selector));
    const reduced = rules.filter((r) => /data-motion/.test(r.selector));
    expect(base.some((r) => /mask-image:\s*linear-gradient\(/.test(r.body))).toBe(true);
    expect(reduced).toHaveLength(2);
    for (const rule of reduced) expect(rule.body).toMatch(/mask-image:\s*none/);
  });

  it("hides content only under data-inview=false", () => {
    const hiding = leafRules(css).filter((r) => /(?:^|;)\s*opacity:\s*0\s*(?:;|$)/.test(r.body));
    expect(hiding.length).toBeGreaterThan(0);
    expect(hiddenOutsideInview(css)).toEqual([]);
  });

  it("accepts either quote style around the attribute value", () => {
    expect(hiddenOutsideInview(".reveal[data-inview='false'] { opacity: 0; }")).toEqual([]);
    expect(hiddenOutsideInview('.reveal[data-inview="false"] { opacity: 0; }')).toEqual([]);
    expect(hiddenOutsideInview(".reveal { opacity: 0; }")).toEqual([".reveal"]);
  });
});
