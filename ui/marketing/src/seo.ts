import { copy } from "./content";
import { type Locale, pageUrl, site } from "./site";

type MetaTag = {
  name?: string;
  property?: string;
  content: string;
};

type LinkTag = {
  rel: string;
  href: string;
  hreflang?: string;
};

export type PageHead = {
  htmlAttrs: { lang: Locale };
  title: string;
  meta: MetaTag[];
  link: LinkTag[];
};

export function pageHead(locale: Locale, noindex: boolean): PageHead {
  const c = copy[locale].meta;
  const page = site.pages.find((p) => p.locale === locale)!;
  const url = pageUrl(page);
  const xDefault = pageUrl(site.pages.find((p) => p.locale === site.xDefault)!);
  const image = `${site.origin}${site.base}og/${locale}.png`;

  return {
    htmlAttrs: { lang: locale },
    title: c.title,
    meta: [
      { name: "description", content: c.description },
      ...(noindex ? [{ name: "robots", content: "noindex" }] : []),
      { property: "og:type", content: "website" },
      { property: "og:url", content: url },
      { property: "og:locale", content: page.ogLocale },
      { property: "og:site_name", content: "Yasaku" },
      { property: "og:title", content: c.ogTitle },
      { property: "og:description", content: c.ogDescription },
      { property: "og:image", content: image },
      { property: "og:image:width", content: "1200" },
      { property: "og:image:height", content: "630" },
      { property: "og:image:alt", content: c.ogTitle },
      { name: "twitter:card", content: "summary_large_image" },
    ],
    link: [
      { rel: "canonical", href: url },
      ...site.pages.map((p) => ({ rel: "alternate", hreflang: p.hreflang, href: pageUrl(p) })),
      { rel: "alternate", hreflang: "x-default", href: xDefault },
      { rel: "icon", href: `${site.base}favicon-32x32.png` },
    ],
  };
}
