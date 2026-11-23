import type { Meta, StoryObj } from "@storybook/vue3-vite";
import { en } from "../content/en";
import ChatPreviewMock from "./ChatPreviewMock.vue";

const meta: Meta<typeof ChatPreviewMock> = {
  title: "Mocks/ChatPreviewMock",
  component: ChatPreviewMock,
  args: { copy: en.mocks },
  render: (args) => ({
    components: { ChatPreviewMock },
    setup: () => ({ args }),
    template: `<div class="bg-neutral p-6"><ChatPreviewMock v-bind="args" /></div>`,
  }),
};
export default meta;

export const Default: StoryObj<typeof ChatPreviewMock> = {};
