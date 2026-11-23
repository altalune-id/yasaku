import { mount } from "@vue/test-utils";
import { h, nextTick } from "vue";
import { afterEach, beforeEach, describe, expect, it, vi } from "vite-plus/test";
import { setReducedMotion } from "../shared/composables/useReducedMotion";
import { FakeIntersectionObserver, installIntersectionObserver } from "../shared/testing/fakes";
import SpotlightLoop from "./SpotlightLoop.vue";

const slots = {
  default: ({ index, active }: { index: number; active: boolean }) =>
    h("i", { "data-active": String(active) }, String(index)),
};

function actives(wrapper: ReturnType<typeof mount>) {
  return wrapper.findAll("i").map((i) => i.attributes("data-active") === "true");
}

describe("SpotlightLoop", () => {
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

  it("cycles 0, 1, 2, 0 while in view", async () => {
    const wrapper = mount(SpotlightLoop, { props: { count: 3, intervalMs: 100 }, slots });
    await nextTick();
    FakeIntersectionObserver.trigger(true);
    await nextTick();
    const seen = [(wrapper.vm as unknown as { active: number }).active];
    for (let i = 0; i < 3; i++) {
      vi.advanceTimersByTime(100);
      await nextTick();
      seen.push(actives(wrapper).indexOf(true));
    }
    expect(seen).toEqual([0, 1, 2, 0]);
    expect(wrapper.findAll("i").map((i) => i.text())).toEqual(["0", "1", "2"]);
  });

  it("pins the index when frozen", async () => {
    const wrapper = mount(SpotlightLoop, {
      props: { count: 3, intervalMs: 100, frozen: 2 },
      slots,
    });
    await nextTick();
    FakeIntersectionObserver.trigger(true);
    await nextTick();
    vi.advanceTimersByTime(500);
    await nextTick();
    expect(actives(wrapper)).toEqual([false, false, true]);
    expect(vi.getTimerCount()).toBe(0);
  });

  it("makes every slot active with no timer under reduced motion", async () => {
    setReducedMotion(true);
    const wrapper = mount(SpotlightLoop, { props: { count: 3, intervalMs: 100 }, slots });
    await nextTick();
    FakeIntersectionObserver.trigger(true);
    await nextTick();
    expect(actives(wrapper)).toEqual([true, true, true]);
    expect(vi.getTimerCount()).toBe(0);
  });
});
