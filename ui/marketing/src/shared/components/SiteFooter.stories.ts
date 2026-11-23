import type { Meta, StoryObj } from "@storybook/vue3-vite";
import SiteFooter from "./SiteFooter.vue";

const meta: Meta<typeof SiteFooter> = {
  title: "Shared/SiteFooter",
  component: SiteFooter,
  args: {
    apps: ["One", "Two", "Three", "Four", "Five"].map((name) => ({
      name,
      href: `https://example.com/${name.toLowerCase()}/`,
    })),
    legal: [
      { label: "Terms", href: "https://app.example.com/terms" },
      { label: "Privacy", href: "https://app.example.com/privacy" },
    ],
    resources: [{ label: "GitHub", href: "https://github.com/example/app" }],
    tagline: "A short sentence about what the app does for you.",
    copyright: "© Example",
    localeHref: "#",
    localeLabel: "Other language",
    columnTitles: { apps: "Apps", legal: "Legal", resources: "Resources" },
  },
  render: (args) => ({
    components: { SiteFooter },
    setup: () => ({ args }),
    template: `<SiteFooter v-bind="args"><template #brand><p class="font-display text-lg font-extrabold">App</p></template></SiteFooter>`,
  }),
};
export default meta;

export const Default: StoryObj<typeof SiteFooter> = {};
