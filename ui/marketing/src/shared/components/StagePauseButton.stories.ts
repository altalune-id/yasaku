import type { Meta, StoryObj } from "@storybook/vue3-vite";
import StagePauseButton from "./StagePauseButton.vue";

const meta: Meta<typeof StagePauseButton> = {
  title: "Shared/StagePauseButton",
  component: StagePauseButton,
  args: { pauseLabel: "Pause animation", playLabel: "Play animation" },
  render: (args) => ({
    components: { StagePauseButton },
    setup: () => ({ args }),
    template: `<div class="relative h-40 rounded-box bg-base-300 p-6"><div class="absolute end-3 top-3"><StagePauseButton v-bind="args" /></div></div>`,
  }),
};
export default meta;

export const Default: StoryObj<typeof StagePauseButton> = {};
