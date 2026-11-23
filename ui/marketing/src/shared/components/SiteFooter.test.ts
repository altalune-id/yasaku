import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vite-plus/test";
import SiteFooter from "./SiteFooter.vue";

const props = {
  apps: [
    { name: "One", href: "https://example.com/one/" },
    { name: "Two", href: "https://example.com/two/" },
  ],
  legal: [{ label: "Terms", href: "https://example.com/terms" }],
  resources: [{ label: "Source", href: "https://example.com/src" }],
  tagline: "A tagline.",
  copyright: "(c) Example",
  localeHref: "/other/",
  localeLabel: "Other language",
  columnTitles: { apps: "Apps", legal: "Legal", resources: "Resources" },
};

describe("SiteFooter", () => {
  it("renders every entry as a real link", () => {
    const wrapper = mount(SiteFooter, { props });
    const hrefs = wrapper.findAll("a").map((a) => [a.text(), a.attributes("href")]);
    for (const app of props.apps) expect(hrefs).toContainEqual([app.name, app.href]);
    expect(hrefs).toContainEqual(["Terms", "https://example.com/terms"]);
    expect(hrefs).toContainEqual(["Source", "https://example.com/src"]);
    expect(hrefs).toContainEqual(["Other language", "/other/"]);
    expect(wrapper.text()).toContain("A tagline.");
    expect(wrapper.text()).toContain("(c) Example");
  });

  it("renders three muted column titles", () => {
    const titles = mount(SiteFooter, { props }).findAll("[data-footer-title]");
    expect(titles).toHaveLength(3);
    for (const title of titles) expect(title.classes()).toContain("text-base-content/80");
  });

  it("renders an optional note in the bottom row", () => {
    expect(mount(SiteFooter, { props }).find("[data-note]").exists()).toBe(false);
    const note = mount(SiteFooter, {
      props: { ...props, note: "Names are trademarks of their owners." },
    }).find("[data-note]");
    expect(note.text()).toBe("Names are trademarks of their owners.");
  });

  it("renders the brand slot", () => {
    const wrapper = mount(SiteFooter, { props, slots: { brand: '<b class="brand">B</b>' } });
    expect(wrapper.find(".brand").exists()).toBe(true);
  });
});
