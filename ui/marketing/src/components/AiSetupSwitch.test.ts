import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vite-plus/test";
import AiSetupSwitch from "./AiSetupSwitch.vue";

const props = {
  url: "https://app.example.com/mcp",
  prompt: "Connect to App at https://app.example.com/mcp, sign me in, then log what I spent today.",
  copiedLabel: "Copied ✓",
  manualHint: "Press Ctrl/⌘+C to copy",
  setup: {
    groupLabel: "Where do you use AI?",
    chat: {
      tab: "Chat app",
      label: "Link",
      body: "In your AI's settings, open Connectors, add a custom connector and paste the link.",
      copy: "Copy link",
      success: "Now paste it in Connectors.",
      idle: "Some apps call this Apps. It may need a paid plan.",
    },
    coding: {
      tab: "Coding tool",
      label: "Prompt",
      body: "Paste the prompt into your coding tool's chat.",
      copy: "Copy prompt",
      success: "Now paste it in your coding tool.",
    },
  },
};

describe("AiSetupSwitch", () => {
  let writeText: ReturnType<typeof vi.fn>;
  beforeEach(() => {
    vi.useFakeTimers();
    writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
  });
  afterEach(() => {
    vi.useRealTimers();
    document.body.innerHTML = "";
  });

  const tabs = (w: ReturnType<typeof mount>) => w.findAll('[role="tab"]');
  const copyButtons = (w: ReturnType<typeof mount>) => w.findAll("[data-copy]");

  it("is a labeled tablist with Chat app selected by default and one copy button", () => {
    const wrapper = mount(AiSetupSwitch, { props });
    expect(wrapper.find('[role="tablist"]').attributes("aria-label")).toBe(props.setup.groupLabel);
    expect(tabs(wrapper).map((t) => [t.text(), t.attributes("aria-selected")])).toEqual([
      ["Chat app", "true"],
      ["Coding tool", "false"],
    ]);
    const panel = wrapper.find('[role="tabpanel"]');
    expect(panel.attributes("aria-labelledby")).toBe(tabs(wrapper)[0]!.attributes("id"));
    expect(panel.text()).toContain(props.url);
    expect(copyButtons(wrapper)).toHaveLength(1);
    expect(copyButtons(wrapper)[0]!.text()).toBe("Copy link");
    expect(panel.text()).toContain(props.setup.chat.idle);
  });

  it("keeps the URL on one line without mid-word breaks", () => {
    const url = mount(AiSetupSwitch, { props })
      .findAll('[role="tabpanel"] p')
      .find((p) => p.text() === props.url)!;
    expect(url.classes()).toEqual(
      expect.arrayContaining(["text-xs", "font-mono", "whitespace-nowrap", "truncate"]),
    );
    expect(url.classes()).not.toContain("break-all");
  });

  it("switches with the arrow keys and roves focus", async () => {
    const wrapper = mount(AiSetupSwitch, { props, attachTo: document.body });
    await tabs(wrapper)[0]!.trigger("keydown", { key: "ArrowRight" });
    expect(tabs(wrapper)[1]!.attributes("aria-selected")).toBe("true");
    expect(document.activeElement).toBe(tabs(wrapper)[1]!.element);
    expect(tabs(wrapper).map((t) => t.attributes("tabindex"))).toEqual(["-1", "0"]);
    expect(copyButtons(wrapper)).toHaveLength(1);
    expect(copyButtons(wrapper)[0]!.text()).toBe("Copy prompt");
    await tabs(wrapper)[1]!.trigger("keydown", { key: "Home" });
    expect(tabs(wrapper)[0]!.attributes("aria-selected")).toBe("true");
    wrapper.unmount();
  });

  it("copies only the URL from Chat app and the prompt from Coding tool", async () => {
    const wrapper = mount(AiSetupSwitch, { props });
    await copyButtons(wrapper)[0]!.trigger("click");
    await flushPromises();
    expect(writeText).toHaveBeenLastCalledWith(props.url);
    expect(wrapper.text()).toContain(props.setup.chat.success);
    await tabs(wrapper)[1]!.trigger("click");
    await copyButtons(wrapper)[0]!.trigger("click");
    await flushPromises();
    expect(writeText).toHaveBeenLastCalledWith(props.prompt);
    expect(wrapper.text()).toContain(props.setup.coding.success);
    const preview = wrapper
      .findAll('[role="tabpanel"] p')
      .find((p) => p.text().startsWith("Connect to App"))!;
    expect(preview.classes()).toContain("truncate");
  });

  it("keeps the execCommand fallback for the link", async () => {
    Object.defineProperty(navigator, "clipboard", { value: undefined, configurable: true });
    const exec = vi.fn(() => true);
    Object.defineProperty(document, "execCommand", { value: exec, configurable: true });
    const wrapper = mount(AiSetupSwitch, { props, attachTo: document.body });
    await copyButtons(wrapper)[0]!.trigger("click");
    await flushPromises();
    expect(exec).toHaveBeenCalledWith("copy");
    expect(copyButtons(wrapper)[0]!.text()).toBe("Copied ✓");
    wrapper.unmount();
  });

  it("opens on the hero event: selects Chat app, engages and focuses Copy link", async () => {
    const wrapper = mount(AiSetupSwitch, { props, attachTo: document.body });
    await tabs(wrapper)[1]!.trigger("click");
    window.dispatchEvent(new CustomEvent("ai-setup:open"));
    await flushPromises();
    expect(tabs(wrapper)[0]!.attributes("aria-selected")).toBe("true");
    expect(document.activeElement?.textContent?.trim()).toBe("Copy link");
    expect(wrapper.emitted("engage")!.length).toBeGreaterThanOrEqual(2);
    wrapper.unmount();
  });

  it("tracks the coding tab and the copies", async () => {
    const layer: { event: string }[] = [];
    (window as { dataLayer?: unknown[] }).dataLayer = layer;
    const wrapper = mount(AiSetupSwitch, { props });
    await copyButtons(wrapper)[0]!.trigger("click");
    await flushPromises();
    await tabs(wrapper)[1]!.trigger("click");
    await copyButtons(wrapper)[0]!.trigger("click");
    await flushPromises();
    expect(layer.map((e) => e.event)).toEqual([
      "copy_link_click",
      "tab_coding_select",
      "copy_prompt_click",
    ]);
    delete (window as { dataLayer?: unknown }).dataLayer;
  });
});
