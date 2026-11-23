import type { Meta, StoryObj } from "@storybook/vue3-vite";
import { en } from "../content/en";
import PeriodCloseMock from "./PeriodCloseMock.vue";

const meta: Meta<typeof PeriodCloseMock> = {
  title: "Mocks/PeriodCloseMock",
  component: PeriodCloseMock,
  args: { copy: en.mocks },
  render: (args) => ({
    components: { PeriodCloseMock },
    setup: () => ({ args }),
    template: `<div class="max-w-xl p-6"><PeriodCloseMock v-bind="args" /></div>`,
  }),
};
export default meta;

export const Default: StoryObj<typeof PeriodCloseMock> = {};
