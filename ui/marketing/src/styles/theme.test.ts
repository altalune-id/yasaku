import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vite-plus/test";

const css = readFileSync(resolve(import.meta.dirname, "main.css"), "utf8");

function theme(name: string): Record<string, string> {
  const block = css.match(new RegExp(`name:\\s*["']${name}["'];([\\s\\S]*?)\\n\\}`))![1]!;
  return Object.fromEntries(
    [...block.matchAll(/(--[\w-]+):\s*([^;]+);/g)].map((m) => [m[1]!, m[2]!.trim()]),
  );
}

function luminance(oklch: string): number {
  const [l, c, h] = oklch
    .match(/oklch\(([\d.]+)(%?)\s+([\d.]+)\s+([\d.]+)\)/)!
    .slice(1)
    .reduce<number[]>((acc, v, i) => {
      if (i === 0) acc.push(Number(v));
      else if (i === 1) acc[0] = v === "%" ? acc[0]! / 100 : acc[0]!;
      else acc.push(Number(v));
      return acc;
    }, []) as [number, number, number];
  const a = c * Math.cos((h * Math.PI) / 180);
  const b = c * Math.sin((h * Math.PI) / 180);
  const l_ = (l + 0.3963377774 * a + 0.2158037573 * b) ** 3;
  const m_ = (l - 0.1055613458 * a - 0.0638541728 * b) ** 3;
  const s_ = (l - 0.0894841775 * a - 1.291485548 * b) ** 3;
  const clamp = (v: number) => Math.min(1, Math.max(0, v));
  const r = clamp(4.0767416621 * l_ - 3.3077115913 * m_ + 0.2309699292 * s_);
  const g = clamp(-1.2684380046 * l_ + 2.6097574011 * m_ - 0.3413193965 * s_);
  const bl = clamp(-0.0041960863 * l_ - 0.7034186147 * m_ + 1.707614701 * s_);
  return 0.2126 * r + 0.7152 * g + 0.0722 * bl;
}

function contrast(a: string, b: string): number {
  const [x, y] = [luminance(a), luminance(b)].sort((p, q) => q - p) as [number, number];
  return (x + 0.05) / (y + 0.05);
}

describe.each(["site-light", "site-dark"])("%s theme", (name) => {
  it("keeps heading text readable over the highlighter", () => {
    const t = theme(name);
    expect(t["--highlight"]).toBeDefined();
    expect(contrast(t["--color-base-content"]!, t["--highlight"]!)).toBeGreaterThanOrEqual(4.5);
  });
});

describe("brand token", () => {
  it("differs between the light and dark themes and backs text-brand", () => {
    expect(theme("site-light")["--brand"]).toBe("#a86f00");
    expect(theme("site-dark")["--brand"]).toBe("#ffbb00");
    expect(css).toMatch(/--color-brand:\s*var\(--brand\)/);
  });
});
