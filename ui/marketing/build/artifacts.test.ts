import { describe, expect, it } from "vite-plus/test";
import { site } from "../src/site";
import { headersFile, sitemapXml } from "./artifacts";

describe("sitemapXml", () => {
  it("lists each page with the three alternates", () => {
    const xml = sitemapXml(site.pages);
    const urls = [...xml.matchAll(/<url>([\s\S]*?)<\/url>/g)].map((m) => m[1]!);
    expect(urls).toHaveLength(2);
    expect(urls[0]).toContain("<loc>https://altalune.id/yasaku/</loc>");
    expect(urls[1]).toContain("<loc>https://altalune.id/yasaku/id/</loc>");
    for (const url of urls) {
      expect(url).toContain(
        '<xhtml:link rel="alternate" hreflang="en" href="https://altalune.id/yasaku/"/>',
      );
      expect(url).toContain(
        '<xhtml:link rel="alternate" hreflang="id" href="https://altalune.id/yasaku/id/"/>',
      );
      expect(url).toContain(
        '<xhtml:link rel="alternate" hreflang="x-default" href="https://altalune.id/yasaku/"/>',
      );
    }
    expect(xml).toContain('xmlns:xhtml="http://www.w3.org/1999/xhtml"');
    expect(xml).not.toContain("lastmod");
  });
});

describe("headersFile", () => {
  it("returns the noindex block when on", () => {
    expect(headersFile(true)).toBe("/yasaku/*\n  X-Robots-Tag: noindex\n");
  });

  it("returns null when off", () => {
    expect(headersFile(false)).toBeNull();
  });
});
