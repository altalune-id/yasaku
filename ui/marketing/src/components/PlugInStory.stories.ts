import type { Meta, StoryObj } from "@storybook/vue3-vite";
import PlugInStory from "./PlugInStory.vue";

const meta: Meta<typeof PlugInStory> = {
  title: "Components/PlugInStory",
  component: PlugInStory,
  args: {
    steps: [
      {
        title: "Copy the link",
        body: "Paste the connector link into your AI assistant.",
        typed: "https://app.example.com/mcp",
      },
      {
        title: "Sign in once",
        body: "Approve access with your account.",
        typed: "Sign in with your account",
      },
      {
        title: "Just say it",
        body: "Tell your assistant what happened.",
        typed: "I spent 25k on coffee.",
      },
    ],
  },
  render: (args) => ({
    components: { PlugInStory },
    setup: () => ({ args }),
    template: `<div class="mx-auto max-w-6xl p-6"><PlugInStory v-bind="args" /></div>`,
  }),
};
export default meta;
type Story = StoryObj<typeof PlugInStory>;

export const Default: Story = {};
export const ReducedMotion: Story = { globals: { motion: "reduce" } };
