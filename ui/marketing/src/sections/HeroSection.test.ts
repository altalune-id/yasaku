import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vite-plus/test";
import { en } from "../content/en";
import { site } from "../site";
import HeroSection from "./HeroSection.vue";

describe("HeroSection", () => {
  it("leads with the product outcome and two real links", () => {
    const wrapper = mount(HeroSection, {
      props: { copy: en.hero, mocks: en.mocks },
    });
    expect(wrapper.find("h1").text()).toBe(en.hero.title);
    const [primary, secondary] = wrapper.findAll("a");
    expect(primary!.attributes("href")).toBe(site.loginUrl);
    expect(primary!.classes()).toEqual(expect.arrayContaining(["btn", "btn-accent", "btn-pop"]));
    expect(primary!.classes().some((c) => /^btn-(?:xs|sm|lg|xl)$/.test(c))).toBe(false);
    expect(secondary!.attributes("href")).toBe("#connect");
  });
});
