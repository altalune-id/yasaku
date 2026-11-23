import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vite-plus/test";
import BrowserFrame from "./BrowserFrame.vue";

describe("BrowserFrame", () => {
  const wrapper = mount(BrowserFrame, {
    props: { path: "/reports", alt: "Reports" },
    slots: { default: "<p>body</p>" },
  });
  const bar = wrapper.find(".mockup-browser-toolbar .input");

  it("keeps the address on one fixed-height line that truncates", () => {
    expect(bar.classes()).toEqual(expect.arrayContaining(["h-8", "flex-nowrap"]));
    expect(bar.find(".truncate").classes()).toContain("whitespace-nowrap");
  });

  it("shows the host only below sm and the full path from sm up", () => {
    expect(bar.text()).toContain("yasaku.altalune.id");
    const path = bar.find("[data-path]");
    expect(path.text()).toBe("/reports");
    expect(path.classes()).toEqual(expect.arrayContaining(["hidden", "sm:inline"]));
  });

  it("puts no button in or beside the bar", () => {
    expect(wrapper.find(".mockup-browser-toolbar button").exists()).toBe(false);
    expect(wrapper.find("button").exists()).toBe(false);
  });
});
