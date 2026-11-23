import type { Meta, StoryObj } from "@storybook/vue3-vite";
import { en } from "../content/en";
import { id } from "../content/id";
import ConsoleShowcase from "./ConsoleShowcase.vue";

const meta: Meta<typeof ConsoleShowcase> = {
  title: "Mocks/ConsoleShowcase",
  component: ConsoleShowcase,
  args: { copy: en.mocks },
  render: (args) => ({
    components: { ConsoleShowcase },
    setup: () => ({ args }),
    template: `<div class="mx-auto max-w-6xl p-4 md:p-6"><ConsoleShowcase v-bind="args" /></div>`,
  }),
};
export default meta;
type Story = StoryObj<typeof ConsoleShowcase>;

export const Default: Story = {};
export const Indonesian: Story = { args: { copy: id.mocks } };
export const Compact: Story = { args: { compact: true } };
