import type { Meta, StoryObj } from "@storybook/vue3-vite";
import SpotlightLoop from "./SpotlightLoop.vue";

const meta: Meta<typeof SpotlightLoop> = {
  title: "Components/SpotlightLoop",
  component: SpotlightLoop,
  args: { count: 3 },
  render: (args) => ({
    components: { SpotlightLoop },
    setup: () => ({ args }),
    template: `
      <SpotlightLoop v-bind="args" v-slot="{ index, active }" class="grid gap-4 p-6 md:grid-cols-3">
        <div class="card card-border bg-base-100 p-6 transition" :class="active ? 'border-primary shadow-pop' : 'opacity-90'">
          Step {{ index + 1 }}{{ active ? ' (active)' : '' }}
        </div>
      </SpotlightLoop>`,
  }),
};
export default meta;
type Story = StoryObj<typeof SpotlightLoop>;

export const Step1: Story = { args: { frozen: 0 } };
export const Step2: Story = { args: { frozen: 1 } };
export const Step3: Story = { args: { frozen: 2 } };
export const Looping: Story = {};
