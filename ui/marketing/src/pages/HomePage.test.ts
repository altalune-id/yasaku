import { createHead } from "@unhead/vue/client";
import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vite-plus/test";
import { copy } from "../content";
import { site } from "../site";
import HomePage from "./HomePage.vue";

const order = ["top", "payday", "features", "ai", "open-source", "faq", "get-started"];

function pmStrings(locale: "en" | "id"): string[] {
  const c = copy[locale];
  return [
    c.nav.cta,
    c.hero.title,
    c.hero.sub,
    c.hero.primaryCta,
    c.hero.secondaryCta,
    c.features.title,
    ...c.features.cards.flatMap((card) => [card.title, card.body]),
    c.payday.title,
    c.payday.body,
    c.ai.title,
    c.ai.body,
    c.ai.note,
    ...c.ai.steps.flatMap((s) => [s.title, s.body]),
    c.openSource.title,
    c.openSource.body,
    c.openSource.cloud.title,
    c.openSource.selfHost.title,
    ...c.faq.items.flatMap((item) => [item.q, item.a]),
    c.finalCta.title,
    c.finalCta.cta,
    c.finalCta.note,
    c.ai.setup.chat.tab,
    c.ai.setup.coding.tab,
    c.ai.setup.chat.body,
    c.ai.setup.chat.copy,
    c.ai.setup.chat.idle,
    c.footer.trademarks,
  ];
}

describe.each(["en", "id"] as const)("HomePage %s", (locale) => {
  const wrapper = mount(HomePage, { props: { locale }, global: { plugins: [createHead()] } });

  it("starts with a skip link to a focusable main", () => {
    const first = wrapper.find("a");
    expect(first.attributes("href")).toBe("#main");
    expect(first.text()).toBe(copy[locale].nav.skipLabel);
    expect(first.classes()).toEqual(
      expect.arrayContaining(["sr-only", "focus:not-sr-only", "focus:min-h-8"]),
    );
    const main = wrapper.find("main");
    expect(main.attributes("id")).toBe("main");
    expect(main.attributes("tabindex")).toBe("-1");
  });

  it("orders the sections as planned", () => {
    expect(wrapper.findAll("main > section[id]").map((s) => s.attributes("id"))).toEqual(order);
  });

  it("links the primary CTA to the login page", () => {
    const primary = wrapper.findAll("a").filter((a) => a.text() === copy[locale].hero.primaryCta);
    expect(primary.length).toBeGreaterThan(0);
    for (const a of primary) expect(a.attributes("href")).toBe(site.loginUrl);
  });

  it("shows exactly one copy button, Copy link in step 1, carrying the MCP URL", () => {
    const buttons = wrapper.findAll("[data-copy]");
    expect(buttons).toHaveLength(1);
    expect(buttons[0]!.text()).toBe(copy[locale].ai.setup.chat.copy);
    expect(buttons[0]!.attributes("title")).toBe(site.mcpUrl);
    expect(wrapper.find("[data-step] #connect [data-copy]").exists()).toBe(true);
  });

  it("keeps steps 2 and 3 as non-interactive pictures", () => {
    const cards = wrapper.findAll("[data-step]");
    for (const card of cards.slice(1)) {
      expect(card.find("button, a, input, [tabindex]").exists()).toBe(false);
      expect(card.find('[aria-hidden="true"]').exists()).toBe(true);
    }
  });

  it("renders every pm string verbatim", () => {
    const text = wrapper.text().replace(/\s+/g, " ");
    for (const s of pmStrings(locale)) expect(text).toContain(s);
  });

  it("gives the nav and hero primary buttons the same variant and size", () => {
    const nav = wrapper.find("nav .navbar-end a.btn-accent").classes();
    const hero = wrapper.find("#top a.btn-accent").classes();
    const look = (cls: string[]) =>
      cls.filter((c) => c.startsWith("btn") && !c.includes(":")).sort();
    expect(look(nav)).toEqual(look(hero));
  });

  it("shows the self-host command and the GitHub link", () => {
    const section = wrapper.find("#open-source");
    expect(section.text()).toContain("ghcr.io/altalune-id/yasaku");
    expect(section.find(".mockup-code").exists()).toBe(false);
    expect(section.findAll("a").map((a) => a.attributes("href"))).toContain(
      `${site.githubUrl}/releases`,
    );
    expect(section.findAll("a").map((a) => a.attributes("href"))).toContain(site.githubUrl);
  });

  it("links the footer apps to site.apps", () => {
    const footer = wrapper.find("footer");
    const hrefs = footer.findAll("a").map((a) => a.attributes("href"));
    for (const app of site.apps) expect(hrefs).toContain(app.href);
  });
});
