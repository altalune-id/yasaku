import type { Meta, StoryObj } from "@storybook/vue3-vite";
import CopyPromptBar from "./CopyPromptBar.vue";

const meta: Meta<typeof CopyPromptBar> = {
  title: "Components/CopyPromptBar",
  component: CopyPromptBar,
  args: {
    prompt: "https://app.example.com/mcp",
    label: "Link",
    copyLabel: "Copy prompt",
    copiedLabel: "Copied ✓",
    manualHint: "Press Ctrl/⌘+C to copy",
    hint: "Paste this link into your AI assistant's connector settings.",
  },
  render: (args) => ({
    components: { CopyPromptBar },
    setup: () => ({ args }),
    template: `<div class="max-w-xl p-6"><CopyPromptBar v-bind="args" /></div>`,
  }),
};
export default meta;
type Story = StoryObj<typeof CopyPromptBar>;

export const Idle: Story = {};
export const Copied: Story = {
  play: async ({ canvasElement }) => {
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText: () => Promise.resolve() },
      configurable: true,
    });
    canvasElement.querySelector("button")?.click();
  },
};
export const LongPrompt: Story = {
  args: {
    prompt:
      "https://app.example.com/mcp?workspace=a-very-long-workspace-name-that-overflows-the-field",
  },
};
