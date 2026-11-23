import { describe, expect, it } from "vite-plus/test";
import { isNoindex, pageUrl, site } from "./site";

describe("site", () => {
  it("builds absolute page urls under the base", () => {
    expect(site.pages.map(pageUrl)).toEqual([
      "https://altalune.id/yasaku/",
      "https://altalune.id/yasaku/id/",
    ]);
  });

  it("keeps noindex on unless the value is exactly false", () => {
    expect(isNoindex(undefined)).toBe(true);
    expect(isNoindex("1")).toBe(true);
    expect(isNoindex("")).toBe(true);
    expect(isNoindex("false")).toBe(false);
  });

  it("links every family app absolutely from the origin", () => {
    expect(site.apps.length).toBe(5);
    for (const app of site.apps) {
      expect(app.href.startsWith(`${site.origin}/`)).toBe(true);
      expect(app.href.endsWith("/")).toBe(true);
    }
  });
});
