// @vitest-environment node
import { createSSRApp, h, type Component } from "vue";
import { renderToString } from "vue/server-renderer";
import { describe, expect, it } from "vite-plus/test";
import GitHubStars from "./components/GitHubStars.vue";
import LocaleSwitch from "./components/LocaleSwitch.vue";
import StagePauseButton from "./components/StagePauseButton.vue";
import SiteFooter from "./components/SiteFooter.vue";
import SiteNav from "./components/SiteNav.vue";
import ThemeToggle from "./components/ThemeToggle.vue";

async function render(
  component: Component,
  props: Record<string, unknown> = {},
  slots: Record<string, () => unknown> = {},
) {
  return renderToString(createSSRApp({ render: () => h(component, props, slots) }));
}

describe("shared components render on the server", () => {
  it("SiteNav renders on the server", async () => {
    const html = await render(SiteNav, {
      appName: "App",
      logoSrc: "/l.svg",
      links: [{ label: "L", href: "#l" }],
      ctaLabel: "Go",
      ctaHref: "https://x.test/",
      locales: [{ code: "EN", href: "/", hreflang: "en", name: "English", current: true }],
      localeLabel: "Language",
      themeLabel: "Theme",
      menuLabel: "Menu",
      github: { href: "https://x.test/repo", label: "Repo", stars: 99 },
    });
    expect(html).toContain("Go");
    expect(html).not.toContain("data-inview");
  });

  it("ThemeToggle, LocaleSwitch, GitHubStars and StagePauseButton render on the server", async () => {
    expect(await render(ThemeToggle, { label: "Theme" })).toContain("Theme");
    expect(
      await render(LocaleSwitch, {
        label: "L",
        locales: [{ code: "EN", href: "/", hreflang: "en", name: "English", current: true }],
      }),
    ).toContain("EN");
    expect(
      await render(GitHubStars, { href: "https://x.test", label: "Repo", stars: 120 }),
    ).toContain("120");
    expect(await render(StagePauseButton, { pauseLabel: "P", playLabel: "Y" })).not.toContain(
      "<button",
    );
  });

  it("SiteFooter renders on the server", async () => {
    const html = await render(SiteFooter, {
      apps: [{ name: "A", href: "https://x.test/a/" }],
      legal: [],
      resources: [],
      tagline: "T",
      copyright: "C",
      localeHref: "/o/",
      localeLabel: "Other",
      columnTitles: { apps: "Apps", legal: "Legal", resources: "Res" },
    });
    expect(html).toContain("https://x.test/a/");
    expect(html).not.toContain("data-inview");
  });
});
