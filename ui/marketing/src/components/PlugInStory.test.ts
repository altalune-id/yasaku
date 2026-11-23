import { mount } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it } from "vite-plus/test";
import { setReducedMotion } from "../shared/composables/useReducedMotion";
import { installIntersectionObserver } from "../shared/testing/fakes";
import PlugInStory from "./PlugInStory.vue";

const steps = [
  { title: "Copy", body: "Copy the link.", typed: "https://example.test/mcp" },
  { title: "Sign in", body: "Sign in once.", typed: "Sign in" },
  { title: "Talk", body: "Say what you spent.", typed: "I spent 5 on tea." },
];

describe("PlugInStory", () => {
  let restore: () => void;
  beforeEach(() => {
    restore = installIntersectionObserver();
    setReducedMotion(false);
  });
  afterEach(() => {
    restore();
    localStorage.clear();
  });

  it("renders each step with its typed text and visual slot", () => {
    const wrapper = mount(PlugInStory, {
      props: { steps },
      slots: { "step-visual-2": "<b>visual two</b>" },
    });
    const cards = wrapper.findAll("[data-step]");
    expect(cards).toHaveLength(3);
    steps.forEach((step, i) => {
      expect(cards[i]!.text()).toContain(step.title);
      expect(cards[i]!.text()).toContain(step.body);
      expect(cards[i]!.find("[data-typed]").text()).toBe(step.typed);
    });
    expect(cards[1]!.find("b").text()).toBe("visual two");
  });

  it("shows the typed demo as plain text, not as a control", () => {
    for (const typed of mount(PlugInStory, { props: { steps } }).findAll("[data-typed]")) {
      const box = typed.element.parentElement!;
      expect(
        [...box.classList].filter((c) => /^(?:bg-|rounded|border|btn|px-|py-)/.test(c)),
      ).toEqual([]);
    }
  });

  it("skips the typed demo for a step whose slot holds a real control", () => {
    const withControl = [{ ...steps[0]!, typed: "" }, steps[1]!, steps[2]!];
    const wrapper = mount(PlugInStory, {
      props: { steps: withControl },
      slots: { "step-visual-1": "<button>Copy</button>" },
    });
    const cards = wrapper.findAll("[data-step]");
    expect(cards[0]!.find("[data-typed]").exists()).toBe(false);
    expect(cards[0]!.find("button").text()).toBe("Copy");
    expect(cards[1]!.find("[data-typed]").exists()).toBe(true);
  });

  it("accepts exactly three steps", () => {
    const validator = (
      PlugInStory as unknown as { props: { steps: { validator: (v: unknown[]) => boolean } } }
    ).props.steps.validator;
    expect(validator(steps)).toBe(true);
    expect(validator(steps.slice(0, 2))).toBe(false);
    expect(validator([...steps, steps[0]])).toBe(false);
  });

  it("makes the active card pop with border, lift, shadow and tint", () => {
    const wrapper = mount(PlugInStory, { props: { steps } });
    const active = wrapper.findAll("[data-step]")[0]!;
    expect(active.classes()).toEqual(
      expect.arrayContaining([
        "border-primary",
        "shadow-pop",
        "-translate-y-1",
        "bg-[color-mix(in_oklab,var(--color-primary)_8%,var(--color-base-100))]",
        "ring-2",
      ]),
    );
    expect(active.find(".animate-type-on").exists()).toBe(true);
    expect(active.find(".animate-cursor-blink").exists()).toBe(true);
    const inactive = wrapper.findAll("[data-step]")[1]!;
    expect(inactive.classes()).not.toContain("border-primary");
    expect(inactive.find(".animate-type-on").exists()).toBe(false);
  });

  it("lays the steps in one column until lg", () => {
    const grid = mount(PlugInStory, { props: { steps } }).find("[data-step]").element
      .parentElement!;
    expect(grid.classList).toContain("lg:grid-cols-3");
    expect([...grid.classList].some((c) => c.startsWith("md:grid-cols-"))).toBe(false);
  });

  it("keeps inactive cards at opacity 80 or higher", () => {
    const wrapper = mount(PlugInStory, { props: { steps } });
    for (const card of wrapper.findAll("[data-step]")) {
      for (const cls of card.classes()) {
        const match = cls.match(/^opacity-(\d+)$/);
        if (match) expect(Number(match[1])).toBeGreaterThanOrEqual(80);
      }
    }
  });
});
