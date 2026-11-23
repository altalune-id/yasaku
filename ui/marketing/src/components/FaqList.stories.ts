import type { Meta, StoryObj } from "@storybook/vue3-vite";
import FaqList from "./FaqList.vue";

const meta: Meta<typeof FaqList> = {
  title: "Components/FaqList",
  component: FaqList,
  args: {
    title: "Questions",
    items: [
      { q: "Is it free?", a: "Yes. It is free and open source." },
      {
        q: "Which assistants does it work with?",
        a: "Any assistant that supports remote connectors.",
      },
      { q: "Where is my data stored?", a: "On the service, under your account only." },
    ],
  },
  render: (args) => ({
    components: { FaqList },
    setup: () => ({ args }),
    template: `<div class="px-4 py-16"><FaqList v-bind="args" /></div>`,
  }),
};
export default meta;
type Story = StoryObj<typeof FaqList>;

export const AllClosed: Story = {};
export const OneOpen: Story = {
  play: async ({ canvasElement }) => {
    canvasElement.querySelector("details")?.setAttribute("open", "");
  },
};
