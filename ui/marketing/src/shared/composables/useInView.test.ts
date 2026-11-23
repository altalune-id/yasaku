import { mount } from "@vue/test-utils";
import { defineComponent, h, nextTick, useTemplateRef } from "vue";
import { afterEach, beforeEach, describe, expect, it } from "vite-plus/test";
import { FakeIntersectionObserver, installIntersectionObserver } from "../testing/fakes";
import { useInView } from "./useInView";

function harness(once = false) {
  return defineComponent({
    setup() {
      const el = useTemplateRef<HTMLElement>("el");
      const inView = useInView(el, { once });
      return () => h("div", { ref: "el", "data-state": String(inView.value) });
    },
  });
}

describe("useInView", () => {
  let restore: () => void;
  beforeEach(() => {
    restore = installIntersectionObserver();
  });
  afterEach(() => restore());

  it("sets no attribute in the render before mount", async () => {
    const { renderToString } = await import("vue/server-renderer");
    const { createSSRApp } = await import("vue");
    const html = await renderToString(createSSRApp(harness()));
    expect(html).not.toContain("data-inview");
  });

  it("starts at false after mount and follows the observer", async () => {
    const wrapper = mount(harness());
    await nextTick();
    expect(wrapper.attributes("data-inview")).toBe("false");
    FakeIntersectionObserver.trigger(true);
    await nextTick();
    expect(wrapper.attributes("data-inview")).toBe("true");
    expect(wrapper.attributes("data-state")).toBe("true");
    FakeIntersectionObserver.trigger(false);
    await nextTick();
    expect(wrapper.attributes("data-inview")).toBe("false");
  });

  it("stays true after leaving view with once", async () => {
    const wrapper = mount(harness(true));
    await nextTick();
    FakeIntersectionObserver.trigger(true);
    await nextTick();
    FakeIntersectionObserver.trigger(false);
    await nextTick();
    expect(wrapper.attributes("data-inview")).toBe("true");
  });
});
