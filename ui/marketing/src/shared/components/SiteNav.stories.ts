import type { Meta, StoryObj } from "@storybook/vue3-vite";
import SiteNav from "./SiteNav.vue";

const meta: Meta<typeof SiteNav> = {
  title: "Shared/SiteNav",
  component: SiteNav,
  args: {
    appName: "App",
    logoSrc: "favicon-32x32.png",
    links: [
      { label: "Product", href: "#product" },
      { label: "Features", href: "#features" },
      { label: "FAQ", href: "#faq" },
    ],
    ctaLabel: "Get started",
    ctaHref: "https://app.example.com/login",
    locales: [
      { code: "EN", href: "#", hreflang: "en", name: "English", current: true },
      { code: "ID", href: "#", hreflang: "id", name: "Bahasa Indonesia", current: false },
    ],
    localeLabel: "Language",
    themeLabel: "Toggle theme",
    menuLabel: "Menu",
    github: { href: "https://github.com/example/app", label: "App on GitHub", stars: 1234 },
  },
  render: (args) => ({
    components: { SiteNav },
    setup: () => ({ args }),
    template: `<div><SiteNav v-bind="args" /><div class="mx-auto max-w-6xl space-y-4 p-6"><p v-for="n in 40" :key="n">Page content line {{ n }}.</p></div></div>`,
  }),
};
export default meta;
type Story = StoryObj<typeof SiteNav>;

export const Default: Story = {};
export const LowStars: Story = {
  args: { github: { href: "https://github.com/example/app", label: "App on GitHub", stars: 12 } },
};
export const MobileMenuOpen: Story = {
  globals: { viewport: { value: "mobile", isRotated: false } },
  play: async ({ canvasElement }) => {
    canvasElement.querySelector("details")?.setAttribute("open", "");
  },
};
export const LongName: Story = {
  args: { appName: "Longername Studio" },
  globals: { viewport: { value: "mobile", isRotated: false } },
};
