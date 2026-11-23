import { mount } from "@vue/test-utils";
import { nextTick } from "vue";
import { afterEach, beforeEach, describe, expect, it, vi } from "vite-plus/test";
import { setReducedMotion } from "../shared/composables/useReducedMotion";
import { FakeIntersectionObserver, installIntersectionObserver } from "../shared/testing/fakes";
import NameRotator from "./NameRotator.vue";

const names = ["ChatGPT", "Claude", "Cursor", "your AI"];
const shown = (w: ReturnType<typeof mount>) =>
  w
    .findAll("[data-name]")
    .filter((n) => !n.classes().includes("invisible"))
    .map((n) => n.text());

describe("NameRotator", () => {
  let restore: () => void;
  beforeEach(() => {
    vi.useFakeTimers();
    restore = installIntersectionObserver();
    setReducedMotion(false);
  });
  afterEach(() => {
    vi.useRealTimers();
    restore();
    localStorage.clear();
    delete document.documentElement.dataset.motion;
  });

  it("rests on the last name before it runs, and lists every name for screen readers", () => {
    const wrapper = mount(NameRotator, { props: { names } });
    expect(shown(wrapper)).toEqual(["your AI"]);
    expect(wrapper.find(".sr-only").text()).toBe("ChatGPT, Claude, Cursor, your AI");
    expect(wrapper.find("[aria-hidden='true']").exists()).toBe(true);
  });

  it("walks through the names once in view and stops on the last", async () => {
    const wrapper = mount(NameRotator, { props: { names, intervalMs: 100 } });
    await nextTick();
    FakeIntersectionObserver.trigger(true);
    await nextTick();
    expect(shown(wrapper)).toEqual(["ChatGPT"]);
    const seen = [];
    for (let i = 0; i < 5; i++) {
      vi.advanceTimersByTime(100);
      await nextTick();
      seen.push(shown(wrapper)[0]);
    }
    expect(seen).toEqual(["Claude", "Cursor", "your AI", "your AI", "your AI"]);
    expect(vi.getTimerCount()).toBe(0);
  });

  it("shows only the last name under reduced motion", async () => {
    setReducedMotion(true);
    const wrapper = mount(NameRotator, { props: { names, intervalMs: 100 } });
    await nextTick();
    FakeIntersectionObserver.trigger(true);
    await nextTick();
    vi.advanceTimersByTime(1000);
    await nextTick();
    expect(shown(wrapper)).toEqual(["your AI"]);
    expect(vi.getTimerCount()).toBe(0);
  });
});
