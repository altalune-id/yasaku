import { mount } from "@vue/test-utils";
import { nextTick } from "vue";
import { afterEach, describe, expect, it } from "vite-plus/test";
import ThemeToggle from "./ThemeToggle.vue";

describe("ThemeToggle", () => {
  afterEach(() => {
    localStorage.clear();
    delete document.documentElement.dataset.theme;
  });

  it("sets data-theme and storage when toggled", async () => {
    const wrapper = mount(ThemeToggle, { props: { label: "Toggle theme" } });
    const box = wrapper.find('input[type="checkbox"]');
    expect(box.attributes("aria-label")).toBe("Toggle theme");
    await box.setValue(true);
    expect(document.documentElement.dataset.theme).toBe("site-dark");
    expect(localStorage.getItem("altalune-theme")).toBe("site-dark");
    await box.setValue(false);
    expect(document.documentElement.dataset.theme).toBe("site-light");
    expect(localStorage.getItem("altalune-theme")).toBe("site-light");
  });

  it("rings the swap label when its input has keyboard focus", () => {
    const label = mount(ThemeToggle, { props: { label: "Toggle theme" } }).find("label.swap");
    expect(label.classes()).toEqual(
      expect.arrayContaining([
        "has-[input:focus-visible]:outline-2",
        "has-[input:focus-visible]:outline-solid",
        "has-[input:focus-visible]:outline-offset-2",
        "has-[input:focus-visible]:outline-base-content",
      ]),
    );
  });

  it("applies a stored value on mount", async () => {
    localStorage.setItem("altalune-theme", "site-dark");
    const wrapper = mount(ThemeToggle, { props: { label: "Toggle theme" } });
    await nextTick();
    expect(document.documentElement.dataset.theme).toBe("site-dark");
    expect((wrapper.find("input").element as HTMLInputElement).checked).toBe(true);
  });
});
