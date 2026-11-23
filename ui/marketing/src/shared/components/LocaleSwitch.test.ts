import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vite-plus/test";
import LocaleSwitch from "./LocaleSwitch.vue";

const locales = [
  { code: "EN", href: "/app/", hreflang: "en", name: "English", current: true },
  { code: "ID", href: "/app/id/", hreflang: "id", name: "Bahasa Indonesia", current: false },
];

describe("LocaleSwitch", () => {
  it("renders a compact EN/ID switch of real links with hreflang", () => {
    const wrapper = mount(LocaleSwitch, { props: { label: "Language", locales } });
    expect(wrapper.attributes("aria-label")).toBe("Language");
    const links = wrapper.findAll("a");
    expect(links.map((a) => a.text())).toEqual(["EN", "ID"]);
    expect(links.map((a) => [a.attributes("href"), a.attributes("hreflang")])).toEqual([
      ["/app/", "en"],
      ["/app/id/", "id"],
    ]);
    expect(links[0]!.attributes("aria-current")).toBe("true");
    expect(links[1]!.attributes("aria-current")).toBeUndefined();
    expect(links[1]!.attributes("title")).toBe("Bahasa Indonesia");
    expect(wrapper.html()).not.toMatch(/flag/i);
  });
});
