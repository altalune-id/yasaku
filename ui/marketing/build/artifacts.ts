import { writeFile } from "node:fs/promises";
import { join } from "node:path";
import { pageUrl, type SitePage, site } from "../src/site";

export function sitemapXml(pages: readonly SitePage[]): string {
  const xDefault = pages.find((p) => p.locale === site.xDefault)!;
  const alternates = [
    ...pages.map((p) => ({ hreflang: p.hreflang as string, href: pageUrl(p) })),
    { hreflang: "x-default", href: pageUrl(xDefault) },
  ]
    .map((a) => `    <xhtml:link rel="alternate" hreflang="${a.hreflang}" href="${a.href}"/>`)
    .join("\n");
  const urls = pages
    .map((p) => `  <url>\n    <loc>${pageUrl(p)}</loc>\n${alternates}\n  </url>`)
    .join("\n");
  return `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9" xmlns:xhtml="http://www.w3.org/1999/xhtml">
${urls}
</urlset>
`;
}

export function headersFile(noindex: boolean): string | null {
  return noindex ? `${site.base}*\n  X-Robots-Tag: noindex\n` : null;
}

export async function writeArtifacts(outDir: string, noindex: boolean): Promise<void> {
  await writeFile(join(outDir, "sitemap.xml"), sitemapXml(site.pages));
  const headers = headersFile(noindex);
  if (headers) await writeFile(join(outDir, "_headers"), headers);
}
