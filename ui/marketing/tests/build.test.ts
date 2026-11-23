import { existsSync, readdirSync, readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vite-plus/test";
import { copy } from "../src/content";
import { pageHead } from "../src/seo";
import { site } from "../src/site";

const dist = resolve(import.meta.dirname, "../dist");
const read = (path: string) => readFileSync(resolve(dist, path), "utf8");

const pages = [
  { file: "index.html", locale: "en" },
  { file: "id/index.html", locale: "id" },
] as const;

function text(html: string): string {
  return html
    .replace(/<[^>]+>/g, " ")
    .replace(/&quot;/g, '"')
    .replace(/&#39;/g, "'")
    .replace(/&amp;/g, "&")
    .replace(/\s+/g, " ");
}

describe("build", () => {
  it.each(pages)("prerenders $file", ({ file, locale }) => {
    expect(existsSync(resolve(dist, file))).toBe(true);
    expect(read(file)).toMatch(new RegExp(`<html[^>]* lang="${locale}"`));
  });

  it.each(pages)("puts the content of $file in the raw HTML", ({ file, locale }) => {
    const html = read(file);
    const c = copy[locale];
    const h1 = text(html.match(/<h1[^>]*>([\s\S]*?)<\/h1>/)![1]!);
    expect(h1.trim()).toBe(c.hero.title);
    const body = text(html);
    for (const item of c.faq.items) expect(body).toContain(item.q);
    expect(html).toContain(`href="${site.loginUrl}"`);
    const footer = html.match(/<footer[\s\S]*?<\/footer>/)![0];
    for (const app of site.apps) expect(footer).toContain(`href="${app.href}"`);
    expect(footer).toMatch(/\u00a9 \d{4} altalune\.id/);
    for (const href of [...footer.matchAll(/href="(https?:[^"]+)"/g)].map((m) => m[1]!)) {
      if (site.apps.some((app) => app.href === href))
        expect(href.startsWith(`${site.origin}/`)).toBe(true);
    }
  });

  it.each(pages)("prefixes every root path in $file with the base", ({ file }) => {
    const html = read(file);
    const paths = [...html.matchAll(/(?:src|href)="(\/[^"]*)"/g)].map((m) => m[1]!);
    expect(paths.length).toBeGreaterThan(0);
    for (const path of paths) {
      expect(path.startsWith(site.base)).toBe(true);
      const rest = path.slice(site.base.length).split(/[?#]/)[0]!;
      const target = rest === "" || rest.endsWith("/") ? `${rest}index.html` : rest;
      expect(existsSync(resolve(dist, target)), path).toBe(true);
    }
  });
});

describe("fonts", () => {
  it("preloads exactly two woff2 files under the base", () => {
    for (const page of ["index.html", "id/index.html"]) {
      const html = read(page);
      const links = html.match(/<link[^>]*rel="preload"[^>]*as="font"[^>]*>/g) ?? [];
      expect(links).toHaveLength(2);
      for (const link of links) {
        const href = link.match(/href="([^"]+)"/)![1]!;
        expect(href.startsWith("/yasaku/assets/")).toBe(true);
        expect(existsSync(resolve(dist, href.slice("/yasaku/".length)))).toBe(true);
      }
    }
  });
});

describe("shared shadow tokens", () => {
  it("take their tint from the theme, not a fixed hue", () => {
    const css = readdirSync(resolve(dist, "assets"))
      .filter((name) => name.endsWith(".css"))
      .map((name) => readFileSync(resolve(dist, "assets", name), "utf8"))
      .join("\n");
    const rules = [...css.matchAll(/\.shadow-(?:card|pop)\{[^}]*\}/g)].map((m) => m[0]);
    expect(rules.length).toBeGreaterThanOrEqual(2);
    for (const rule of rules) {
      expect(rule).not.toMatch(/285\)|285\//);
      expect(rule).toMatch(/var\(--shadow-(card|pop)-tint\)/);
    }
    expect(css).toMatch(
      /--shadow-pop-tint:\s*color-mix\(in oklab,\s*var\(--color-primary\) 35%,\s*transparent\)/,
    );
  });
});

function escape(value: string): string {
  return value.replace(/&/g, "&amp;").replace(/"/g, "&quot;");
}

function hasTag(html: string, tag: string, attrs: Record<string, string>): boolean {
  return [...html.matchAll(new RegExp(`<${tag}\\b[^>]*>`, "g"))].some(([el]) =>
    Object.entries(attrs).every(([key, value]) => el.includes(`${key}="${escape(value)}"`)),
  );
}

describe("head", () => {
  it.each(pages)("writes the full head of $file into the raw HTML", ({ file, locale }) => {
    const html = read(file);
    const head = pageHead(locale, true);
    expect(html).toContain(`<title>${escape(head.title)}</title>`);
    for (const meta of head.meta) {
      const key: Record<string, string> = meta.name
        ? { name: meta.name }
        : { property: meta.property! };
      expect(hasTag(html, "meta", { ...key, content: meta.content }), JSON.stringify(meta)).toBe(
        true,
      );
    }
    for (const link of head.link) {
      const attrs: Record<string, string> = { rel: link.rel, href: link.href };
      if (link.hreflang) attrs.hreflang = link.hreflang;
      expect(hasTag(html, "link", attrs), JSON.stringify(link)).toBe(true);
    }
    expect(hasTag(html, "meta", { name: "robots", content: "noindex" })).toBe(true);
    const image = head.meta.find((m) => m.property === "og:image")!.content;
    expect(existsSync(resolve(dist, image.slice(`${site.origin}${site.base}`.length)))).toBe(true);
  });
});

describe("artifacts", () => {
  it("writes the sitemap and the noindex headers, and no robots.txt", () => {
    expect(existsSync(resolve(dist, "sitemap.xml"))).toBe(true);
    expect(read("_headers")).toBe("/yasaku/*\n  X-Robots-Tag: noindex\n");
    expect(existsSync(resolve(dist, "robots.txt"))).toBe(false);
  });
});

describe("wrangler.jsonc", () => {
  it("serves dist under the site base", () => {
    const config = JSON.parse(
      readFileSync(resolve(import.meta.dirname, "../wrangler.jsonc"), "utf8").replace(
        /,(\s*[}\]])/g,
        "$1",
      ),
    );
    expect(config.assets.directory).toBe("./dist");
    expect(config.assets.base_path).toBe(site.base.replace(/\/$/, ""));
    expect(config.routes.map((r: { pattern: string }) => r.pattern)).toEqual([
      "altalune.id/yasaku*",
    ]);
  });
});
