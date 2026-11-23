import type { Meta, StoryObj } from "@storybook/vue3-vite";
import { en } from "../content/en";
import FeatureVisual from "./FeatureVisual.vue";

const meta: Meta<typeof FeatureVisual> = {
  title: "Mocks/FeatureVisual",
  component: FeatureVisual,
  args: { copy: en.mocks },
  render: (args) => ({
    components: { FeatureVisual },
    setup: () => ({ args }),
    template: `<div class="max-w-sm p-6"><FeatureVisual v-bind="args" /></div>`,
  }),
};
export default meta;
type Story = StoryObj<typeof FeatureVisual>;

export const Wallets: Story = { args: { variant: "wallets" } };
export const Categories: Story = { args: { variant: "categories" } };
export const Transfer: Story = { args: { variant: "transfer" } };
export const Adjust: Story = { args: { variant: "adjust" } };
export const Search: Story = { args: { variant: "search" } };
export const Ledgers: Story = { args: { variant: "ledgers" } };
