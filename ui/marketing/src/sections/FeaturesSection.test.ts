import { mount } from "@vue/test-utils";
import { nextTick } from "vue";
import { afterEach, beforeEach, describe, expect, it } from "vite-plus/test";
import { en } from "../content/en";
import { removeIntersectionObserver } from "../shared/testing/fakes";
import FeaturesSection from "./FeaturesSection.vue";

describe("FeaturesSection", () => {
  let restore: () => void;
  beforeEach(() => {
    restore = removeIntersectionObserver();
  });
  afterEach(() => restore());

  it("shows all six cards when IntersectionObserver is missing", async () => {
    const wrapper = mount(FeaturesSection, { props: { copy: en.features, mocks: en.mocks } });
    await nextTick();
    await nextTick();
    expect(wrapper.findAll("[data-feature]")).toHaveLength(6);
    expect(wrapper.find('[data-inview="false"]').exists()).toBe(false);
  });
});

describe("FeaturesSection spacing", () => {
  const spacing = (cls: string[], prefix: string) =>
    cls
      .map((c) => c.match(new RegExp(`^${prefix}-(\\d+(?:\\.\\d+)?|auto)$`))?.[1])
      .filter(Boolean) as string[];

  it("aligns copy and visuals across a row and keeps each visual within 32px of its copy", () => {
    const wrapper = mount(FeaturesSection, { props: { copy: en.features, mocks: en.mocks } });
    for (const card of wrapper.findAll("[data-feature]")) {
      expect(card.classes()).toEqual(expect.arrayContaining(["row-span-2", "grid-rows-subgrid"]));
      const gap = spacing(card.classes(), "gap-y").map(Number);
      const visual = card.find("[data-visual]");
      const margins = spacing(visual.classes(), "mt").concat(spacing(visual.classes(), "pt"));
      expect(margins).not.toContain("auto");
      expect(
        Math.max(0, ...gap) * 4 + margins.reduce((sum, m) => sum + Number(m) * 4, 0),
      ).toBeLessThanOrEqual(32);
      expect(card.element.lastElementChild).toBe(visual.element);
      expect(visual.find("[aria-hidden]").classes()).toContain("h-full");
    }
  });
});
