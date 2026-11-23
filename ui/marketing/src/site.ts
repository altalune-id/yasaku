export type Locale = "en" | "id";

export interface SitePage {
  locale: Locale;
  path: "/" | "/id/";
  hreflang: "en" | "id";
  ogLocale: "en_US" | "id_ID";
}

export interface FamilyApp {
  name: string;
  href: string;
}

const origin = "https://altalune.id";

export const site = {
  origin,
  base: "/yasaku/",
  pages: [
    { locale: "en", path: "/", hreflang: "en", ogLocale: "en_US" },
    { locale: "id", path: "/id/", hreflang: "id", ogLocale: "id_ID" },
  ] as readonly SitePage[],
  xDefault: "en" as Locale,
  apps: [
    { name: "Yasaku", href: `${origin}/yasaku/` },
    { name: "GOpenWA", href: `${origin}/openwa/` },
    { name: "OAgent", href: `${origin}/oagent/` },
    { name: "Othra", href: `${origin}/othra/` },
    { name: "OpenSheet", href: `${origin}/opensheet/` },
  ] as readonly FamilyApp[],
  loginUrl: "https://yasaku.altalune.id/login",
  mcpUrl: "https://yasaku.altalune.id/mcp",
  githubRepo: "altalune-id/yasaku",
  githubUrl: "https://github.com/altalune-id/yasaku",
  termsUrl: "https://yasaku.altalune.id/terms",
  privacyUrl: "https://yasaku.altalune.id/privacy",
} as const;

export function pageUrl(page: SitePage): string {
  return site.origin + site.base + page.path.slice(1);
}

export function isNoindex(value: string | undefined): boolean {
  return value !== "false";
}
