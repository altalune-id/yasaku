import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vite-plus/test";
import CopyPromptBar from "./CopyPromptBar.vue";

const props = {
  prompt: "Connect to App at https://app.example.com/mcp, sign me in, then log what I spent today.",
  label: "Prompt",
  copyLabel: "Copy prompt",
  copiedLabel: "Copied ✓",
  hint: "Paste into your AI.",
  successHint: "Now paste it in your coding tool.",
  manualHint: "Press Ctrl/⌘+C to copy",
};

describe("CopyPromptBar", () => {
  let writeText: ReturnType<typeof vi.fn>;
  const original = Object.getOwnPropertyDescriptor(navigator, "clipboard");

  beforeEach(() => {
    vi.useFakeTimers();
    writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
  });
  afterEach(() => {
    vi.useRealTimers();
    if (original) Object.defineProperty(navigator, "clipboard", original);
    else delete (navigator as { clipboard?: unknown }).clipboard;
  });

  it("is one labeled box: a copy button in the header and the text on one line below", () => {
    const wrapper = mount(CopyPromptBar, { props });
    const buttons = wrapper.findAll("button");
    expect(buttons).toHaveLength(1);
    const button = buttons[0]!;
    expect(button.find("svg").exists()).toBe(true);
    expect(button.text()).toBe("Copy prompt");
    expect(button.element.parentElement!.textContent).toContain(props.label);
    const text = wrapper.findAll("p").find((p) => p.text() === props.prompt)!;
    expect(text.classes()).toEqual(expect.arrayContaining(["truncate", "whitespace-nowrap"]));
    expect(wrapper.find("input, textarea").exists()).toBe(false);
  });

  it("describes the prompt to assistive tech and in the title", () => {
    const wrapper = mount(CopyPromptBar, { props });
    const button = wrapper.find("button");
    expect(button.attributes("title")).toBe(props.prompt);
    const ids = button.attributes("aria-describedby")!.split(" ");
    const described = ids.map((id) => wrapper.find(`#${CSS.escape(id)}`).text()).join(" ");
    expect(described).toContain(props.prompt);
    expect(described).toContain(props.hint);
  });

  it("shows the caption directly under the box", () => {
    const wrapper = mount(CopyPromptBar, { props });
    const hint = wrapper.findAll("p").at(-1)!;
    expect(hint.text()).toBe(props.hint);
    expect(
      wrapper.find("button").element.compareDocumentPosition(hint.element) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
  });

  it("copies the exact prompt with its URL, announces, and resets after 2000ms", async () => {
    const wrapper = mount(CopyPromptBar, { props });
    await wrapper.find("button").trigger("click");
    await flushPromises();
    expect(writeText).toHaveBeenCalledWith(props.prompt);
    expect(writeText.mock.calls[0]![0]).toContain("https://app.example.com/mcp");
    expect(wrapper.find("button").text()).toBe("Copied ✓");
    expect(wrapper.find('[aria-live="polite"]').text()).toBe("Copied ✓");
    expect(wrapper.emitted("copied")).toHaveLength(1);
    vi.advanceTimersByTime(2000);
    await flushPromises();
    expect(wrapper.find("button").text()).toBe("Copy prompt");
  });

  it("swaps the caption to the success hint and keeps it", async () => {
    const wrapper = mount(CopyPromptBar, { props });
    await wrapper.find("button").trigger("click");
    await flushPromises();
    vi.advanceTimersByTime(5000);
    await flushPromises();
    expect(wrapper.find("button").text()).toBe("Copy prompt");
    expect(wrapper.findAll("p").at(-1)!.text()).toBe(props.successHint);
    expect(wrapper.find("button").attributes("data-copy")).toBeDefined();
  });

  it("falls back to execCommand when the clipboard API is missing", async () => {
    Object.defineProperty(navigator, "clipboard", { value: undefined, configurable: true });
    const exec = vi.fn((cmd: string) => {
      expect((document.activeElement as HTMLTextAreaElement).value).toBe(props.prompt);
      return cmd === "copy";
    });
    Object.defineProperty(document, "execCommand", { value: exec, configurable: true });
    const wrapper = mount(CopyPromptBar, { props, attachTo: document.body });
    await wrapper.find("button").trigger("click");
    await flushPromises();
    expect(exec).toHaveBeenCalledWith("copy");
    expect(wrapper.find("button").text()).toBe("Copied ✓");
    expect(document.querySelectorAll("textarea")).toHaveLength(0);
    wrapper.unmount();
  });

  it("shows a selected manual box when both copy paths fail, never claiming success", async () => {
    writeText.mockRejectedValue(new Error("denied"));
    Object.defineProperty(document, "execCommand", {
      value: vi.fn(() => false),
      configurable: true,
    });
    const wrapper = mount(CopyPromptBar, { props, attachTo: document.body });
    await wrapper.find("button").trigger("click");
    await flushPromises();
    expect(wrapper.find("button").text()).toBe("Copy prompt");
    expect(wrapper.find('[aria-live="polite"]').text()).toBe("");
    expect(wrapper.emitted("copied")).toBeUndefined();
    const box = wrapper.find("[data-manual] textarea").element as HTMLTextAreaElement;
    expect(box.value).toBe(props.prompt);
    expect(box.readOnly).toBe(true);
    expect([box.selectionStart, box.selectionEnd]).toEqual([0, props.prompt.length]);
    expect(wrapper.find("[data-manual]").text()).toContain(props.manualHint);
    wrapper.unmount();
  });

  it("gives the button a visible focus ring in every theme", () => {
    expect(mount(CopyPromptBar, { props }).find("button").classes()).toContain(
      "focus-visible:outline-base-content",
    );
  });
});
