import { mount } from "@vue/test-utils";
import { nextTick } from "vue";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import { setReducedMotion } from "../composables/useReducedMotion";
import StagePauseButton from "./StagePauseButton.vue";

const props = { pauseLabel: "Pause animation", playLabel: "Play animation" };

function stubMatchMedia(reduce: boolean) {
  vi.stubGlobal("matchMedia", (query: string) => ({
    matches: reduce && query.includes("reduce"),
    media: query,
    addEventListener() {},
    removeEventListener() {},
  }));
}

describe("StagePauseButton", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    localStorage.clear();
    delete document.documentElement.dataset.motion;
  });

  it("pauses and plays the shared motion state", async () => {
    stubMatchMedia(false);
    setReducedMotion(false);
    const wrapper = mount(StagePauseButton, { props });
    await nextTick();
    const button = wrapper.find("button");
    expect(button.attributes("aria-label")).toBe("Pause animation");
    await button.trigger("click");
    expect(document.documentElement.dataset.motion).toBe("reduce");
    expect(button.attributes("aria-label")).toBe("Play animation");
    await button.trigger("click");
    expect(document.documentElement.dataset.motion).toBe("full");
  });

  it("does not render when the system asks for reduced motion", async () => {
    stubMatchMedia(true);
    const wrapper = mount(StagePauseButton, { props });
    await nextTick();
    expect(wrapper.find("button").exists()).toBe(false);
  });

  it("renders nothing on the server pass before mount", () => {
    stubMatchMedia(false);
    const wrapper = mount(StagePauseButton, { props, attachTo: undefined });
    expect(wrapper.html()).toBe("<!--v-if-->");
  });
});
