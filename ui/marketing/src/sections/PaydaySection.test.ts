import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vite-plus/test";
import { en } from "../content/en";
import PaydaySection from "./PaydaySection.vue";

describe("PaydaySection", () => {
  it("shows the step a visitor picks and the next period on step 3", async () => {
    const wrapper = mount(PaydaySection, {
      props: { copy: en.payday, mocks: en.mocks, pauseLabel: "Pause", playLabel: "Play" },
    });
    const steps = wrapper.findAll("[data-payday-step]");
    expect(steps).toHaveLength(3);
    expect(steps[0]!.attributes("aria-current")).toBe("step");

    await steps[2]!.trigger("click");
    expect(steps[2]!.attributes("aria-current")).toBe("step");
    expect(steps[0]!.attributes("aria-current")).toBeUndefined();
    expect(wrapper.text()).toContain(en.mocks.nextPeriod);
    expect(wrapper.find('[role="img"]').attributes("aria-label")).toBe(en.mocks.periodsAlt);

    await steps[1]!.trigger("click");
    expect(wrapper.find('[role="img"]').attributes("aria-label")).toBe(en.mocks.periodAlt);
  });

  it("pauses when a step is picked and resumes from the labelled pause button", async () => {
    const wrapper = mount(PaydaySection, {
      props: { copy: en.payday, mocks: en.mocks, pauseLabel: "Pause", playLabel: "Play" },
    });
    await wrapper.vm.$nextTick();
    const pause = wrapper.find("[data-story-pause]");
    expect(pause.text()).toBe("Pause");
    await wrapper.findAll("[data-payday-step]")[1]!.trigger("click");
    expect(pause.text()).toBe("Play");
    expect(pause.attributes("aria-pressed")).toBe("true");
    await pause.trigger("click");
    expect(pause.text()).toBe("Pause");
    expect(pause.attributes("aria-pressed")).toBe("false");
  });
});
