import { describe, expect, it } from "vite-plus/test";
import { copy } from "./content";
import { pageHead } from "./seo";

const origin = "https://altalune.id";
const urls = { en: `${origin}/yasaku/`, id: `${origin}/yasaku/id/` };

function meta(head: ReturnType<typeof pageHead>, key: string) {
  return head.meta.find((m) => m.name === key || m.property === key)?.content;
}

describe.each(["en", "id"] as const)("pageHead %s", (locale) => {
  const head = pageHead(locale, true);
  const c = copy[locale];

  it("sets lang, title and description", () => {
    expect(head.htmlAttrs.lang).toBe(locale);
    expect(head.title).toBe(c.meta.title);
    expect(meta(head, "description")).toBe(c.meta.description);
  });

  it("sets canonical and the three hreflang links", () => {
    const link = (rel: string, hreflang?: string) =>
      head.link.find((l) => l.rel === rel && l.hreflang === hreflang)?.href;
    expect(link("canonical")).toBe(urls[locale]);
    expect(link("alternate", "en")).toBe(urls.en);
    expect(link("alternate", "id")).toBe(urls.id);
    expect(link("alternate", "x-default")).toBe(urls.en);
    expect(link("icon")).toBe("/yasaku/favicon-32x32.png");
  });

  it("sets the og and twitter tags", () => {
    expect(meta(head, "og:type")).toBe("website");
    expect(meta(head, "og:url")).toBe(urls[locale]);
    expect(meta(head, "og:locale")).toBe(locale === "en" ? "en_US" : "id_ID");
    expect(meta(head, "og:site_name")).toBe("Yasaku");
    expect(meta(head, "og:title")).toBe(c.meta.ogTitle);
    expect(meta(head, "og:description")).toBe(c.meta.ogDescription);
    expect(meta(head, "og:image")).toBe(`${origin}/yasaku/og/${locale}.png`);
    expect(meta(head, "og:image:width")).toBe("1200");
    expect(meta(head, "og:image:height")).toBe("630");
    expect(meta(head, "og:image:alt")).toBe(c.meta.ogTitle);
    expect(meta(head, "twitter:card")).toBe("summary_large_image");
  });

  it("adds robots noindex only when the switch is on", () => {
    expect(meta(head, "robots")).toBe("noindex");
    expect(meta(pageHead(locale, false), "robots")).toBeUndefined();
  });
});
