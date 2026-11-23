import { mount } from "@vue/test-utils";
import { defineComponent, h, nextTick, shallowRef } from "vue";
import { afterEach, beforeEach, describe, expect, it, vi } from "vite-plus/test";
import { useTimeline } from "./useTimeline";

function setup(opts: { intro?: boolean; inView?: boolean; reduced?: boolean } = {}) {
  const log: string[] = [];
  const inView = shallowRef(opts.inView ?? true);
  const reduced = shallowRef(opts.reduced ?? false);
  let running!: Readonly<{ value: boolean }>;
  const Host = defineComponent({
    setup() {
      running = useTimeline({
        intro:
          opts.intro === false
            ? undefined
            : [
                { at: 0, run: () => log.push("i0") },
                { at: 100, run: () => log.push("i100") },
              ],
        introMs: 300,
        loop: [
          { at: 0, run: () => log.push("l0") },
          { at: 50, run: () => log.push("l50") },
        ],
        loopMs: 200,
        inView,
        reduced,
        onReset: () => log.push("reset"),
      }).running;
      return () => h("div");
    },
  });
  return { log, inView, reduced, running: () => running.value, mount: () => mount(Host) };
}

async function advance(ms: number) {
  await vi.advanceTimersByTimeAsync(ms);
  expect(vi.getTimerCount()).toBeLessThanOrEqual(1);
}

describe("useTimeline", () => {
  beforeEach(async () => {
    vi.useFakeTimers();
    mount(defineComponent({ setup: () => () => h("div") })).unmount();
    await vi.runAllTimersAsync();
  });
  afterEach(() => vi.useRealTimers());

  it("runs nothing before mount", () => {
    const t = setup();
    expect(t.log).toEqual([]);
    expect(vi.getTimerCount()).toBe(0);
  });

  it("fires intro cues once at their time, then loops with a reset before each pass", async () => {
    const t = setup();
    const w = t.mount();
    await nextTick();
    expect(t.log).toEqual([]);
    expect(t.running()).toBe(true);
    await advance(0);
    expect(t.log).toEqual(["i0"]);
    await advance(99);
    expect(t.log).toEqual(["i0"]);
    await advance(1);
    expect(t.log).toEqual(["i0", "i100"]);
    await advance(200);
    expect(t.log).toEqual(["i0", "i100", "reset", "l0"]);
    await advance(50);
    expect(t.log).toEqual(["i0", "i100", "reset", "l0", "l50"]);
    await advance(150);
    expect(t.log).toEqual(["i0", "i100", "reset", "l0", "l50", "reset", "l0"]);
    for (let i = 0; i < 20; i++) await advance(37);
    expect(t.log.filter((e) => e.startsWith("i"))).toEqual(["i0", "i100"]);
    w.unmount();
  });

  it("starts the loop at once without an intro", async () => {
    const t = setup({ intro: false });
    t.mount();
    await nextTick();
    await advance(0);
    expect(t.log).toEqual(["reset", "l0"]);
  });

  it("clears the timer and resets when leaving view, then restarts the loop at 0", async () => {
    const t = setup();
    t.mount();
    await nextTick();
    await advance(400);
    t.log.length = 0;
    t.inView.value = false;
    await nextTick();
    expect(vi.getTimerCount()).toBe(0);
    expect(t.log).toEqual(["reset"]);
    expect(t.running()).toBe(false);
    t.log.length = 0;
    t.inView.value = true;
    await nextTick();
    await advance(0);
    expect(t.log).toEqual(["reset", "l0"]);
  });

  it("replays the intro from 0 when view was left mid-intro", async () => {
    const t = setup();
    t.mount();
    await nextTick();
    await advance(50);
    t.inView.value = false;
    await nextTick();
    t.log.length = 0;
    t.inView.value = true;
    await nextTick();
    await advance(0);
    expect(t.log).toEqual(["i0"]);
    await advance(100);
    expect(t.log).toEqual(["i0", "i100"]);
  });

  it("starts nothing under reduced motion at mount", async () => {
    const t = setup({ reduced: true });
    t.mount();
    await nextTick();
    await advance(1000);
    expect(t.log).toEqual([]);
    expect(vi.getTimerCount()).toBe(0);
    expect(t.running()).toBe(false);
  });

  it("clears the timer and resets when reduced motion turns on mid-loop", async () => {
    const t = setup();
    t.mount();
    await nextTick();
    await advance(350);
    t.log.length = 0;
    t.reduced.value = true;
    await nextTick();
    expect(t.log).toEqual(["reset"]);
    expect(vi.getTimerCount()).toBe(0);
    await advance(1000);
    expect(t.log).toEqual(["reset"]);
  });

  it("clears the timer on unmount", async () => {
    const t = setup();
    const w = t.mount();
    await nextTick();
    await advance(10);
    w.unmount();
    expect(vi.getTimerCount()).toBe(0);
  });
});
