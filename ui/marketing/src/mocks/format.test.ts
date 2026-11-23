import { afterEach, describe, expect, it, vi } from "vite-plus/test";

describe("formatIDR", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.resetModules();
  });

  it("formats the same in every engine, so prerender and hydration match", async () => {
    const webkitLike = class {
      format(n: number) {
        return `Rp${n.toLocaleString("en-US").replaceAll(",", ".")}`;
      }
    };
    vi.stubGlobal("Intl", { ...Intl, NumberFormat: webkitLike });
    vi.resetModules();
    const { formatIDR } = await import("./format");
    expect(formatIDR(25000)).toBe("Rp 25.000");
    expect(formatIDR(1225000)).toBe("Rp 1.225.000");
    expect(formatIDR(0)).toBe("Rp 0");
    expect(formatIDR(999)).toBe("Rp 999");
  });
});
