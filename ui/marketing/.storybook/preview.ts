import type { Preview } from "@storybook/vue3-vite";
import { headSymbol } from "@unhead/vue";
import { createHead } from "@unhead/vue/client";
import { provide } from "vue";
import "../src/styles/main.css";

const head = createHead();

const preview: Preview = {
  globalTypes: {
    theme: {
      description: "Theme",
      toolbar: {
        title: "Theme",
        icon: "mirror",
        items: ["site-light", "site-dark"],
        dynamicTitle: true,
      },
    },
    motion: {
      description: "Motion",
      toolbar: { title: "Motion", icon: "play", items: ["full", "reduce"], dynamicTitle: true },
    },
  },
  initialGlobals: { theme: "site-light", motion: "full" },
  loaders: [
    async () => {
      await Promise.all([
        document.fonts.load('800 1em "Bricolage Grotesque Variable"'),
        document.fonts.load('400 1em "Figtree Variable"'),
        document.fonts.load('400 1em "IBM Plex Mono"'),
      ]);
      return {};
    },
  ],
  decorators: [
    (story, context) => {
      const root = document.documentElement;
      root.dataset.theme = context.globals.theme as string;
      root.dataset.motion = context.globals.motion as string;
      const key = `${root.dataset.theme}-${root.dataset.motion}`;
      return {
        components: { story },
        setup: () => provide(headSymbol, head),
        template: `<div key="${key}" class="min-h-screen bg-base-100 text-base-content"><story /></div>`,
      };
    },
  ],
  parameters: {
    backgrounds: { disable: true },
    viewport: {
      options: {
        mobile: { name: "390", styles: { width: "390px", height: "844px" } },
        tablet: { name: "768", styles: { width: "768px", height: "1024px" } },
        desktop: { name: "1440", styles: { width: "1440px", height: "900px" } },
      },
    },
  },
};

export default preview;
