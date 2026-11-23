import { mount } from "@vue/test-utils";
import { defineComponent, h, nextTick } from "vue";
import { afterEach, beforeEach, describe, expect, it, vi } from "vite-plus/test";

async function load() {
  vi.resetModules();
  return import("./useReducedMotion");
}

function harness(mod: Awaited<ReturnType<typeof load>>, seen: boolean[]) {
  return defineComponent({
    setup() {
      const reduced = mod.useReducedMotion();
      seen.push(reduced.value);
      return () => h("div", String(reduced.value));
    },
  });
}

describe("useReducedMotion", () => {
  beforeEach(() => {
    delete document.documentElement.dataset.motion;
    localStorage.clear();
  });
  afterEach(() => {
    delete document.documentElement.dataset.motion;
    localStorage.clear();
  });

  it("is false before mount even with data-motion=reduce, true after", async () => {
    document.documentElement.dataset.motion = "reduce";
    const mod = await load();
    const seen: boolean[] = [];
    const wrapper = mount(harness(mod, seen));
    expect(seen[0]).toBe(false);
    await nextTick();
    expect(wrapper.text()).toBe("true");
  });

  it("is false after mount without data-motion", async () => {
    const mod = await load();
    const wrapper = mount(harness(mod, []));
    await nextTick();
    expect(wrapper.text()).toBe("false");
  });

  it("setReducedMotion sets the attribute, storage and ref", async () => {
    const mod = await load();
    const wrapper = mount(harness(mod, []));
    await nextTick();
    mod.setReducedMotion(true);
    await nextTick();
    expect(document.documentElement.dataset.motion).toBe("reduce");
    expect(localStorage.getItem("altalune-motion")).toBe("reduce");
    expect(wrapper.text()).toBe("true");
    mod.setReducedMotion(false);
    await nextTick();
    expect(document.documentElement.dataset.motion).toBe("full");
    expect(localStorage.getItem("altalune-motion")).toBe("full");
    expect(wrapper.text()).toBe("false");
  });
});
