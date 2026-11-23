// @vitest-environment node
import { createSSRApp, h, type Component } from "vue";
import { renderToString } from "vue/server-renderer";
import { describe, expect, it } from "vite-plus/test";
import CopyPromptBar from "./CopyPromptBar.vue";
import FaqList from "./FaqList.vue";
import PlugInStory from "./PlugInStory.vue";
import SpotlightLoop from "./SpotlightLoop.vue";

async function render(
  component: Component,
  props: Record<string, unknown> = {},
  slots: Record<string, () => unknown> = {},
) {
  return renderToString(createSSRApp({ render: () => h(component, props, slots) }));
}

describe("components render on the server", () => {
  it("SpotlightLoop renders on the server", async () => {
    const html = await render(SpotlightLoop, { count: 2 }, { default: () => h("i", "slot") });
    expect(html).toContain("slot");
    expect(html).not.toContain("data-inview");
  });

  it("PlugInStory renders on the server", async () => {
    const steps = [1, 2, 3].map((n) => ({ title: `t${n}`, body: `b${n}`, typed: `x${n}` }));
    const html = await render(PlugInStory, { steps });
    expect(html).toContain("x3");
    expect(html).not.toContain("data-inview");
  });

  it("CopyPromptBar renders on the server", async () => {
    const html = await render(CopyPromptBar, {
      prompt: "p://x",
      copyLabel: "C",
      copiedLabel: "D",
      manualHint: "M",
      hint: "H",
    });
    expect(html).toContain("p://x");
    expect(html).not.toContain("data-inview");
  });

  it("FaqList renders on the server", async () => {
    const faq = await render(FaqList, { title: "F", items: [{ q: "Q", a: "A" }] });
    expect(faq).not.toContain("data-inview");
    expect(faq).toContain("<details");
  });
});
