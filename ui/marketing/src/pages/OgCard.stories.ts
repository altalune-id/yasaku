import type { Meta, StoryObj } from "@storybook/vue3-vite";
import { copy } from "../content";
import BrandMark from "../components/BrandMark.vue";
import ConsoleShowcase from "../mocks/ConsoleShowcase.vue";
import type { Locale } from "../site";

function card(locale: Locale): StoryObj {
  return {
    render: () => ({
      components: { BrandMark, ConsoleShowcase },
      setup: () => ({ c: copy[locale] }),
      template: `
        <div class="relative grid h-[630px] w-[1200px] grid-cols-[1fr_560px] items-center gap-10 overflow-hidden bg-base-300 px-16">
          <div>
            <p class="flex items-center gap-3 font-display text-3xl font-extrabold"><BrandMark class="size-10" />Yasaku</p>
            <h1 class="mt-8 font-display text-[56px] leading-[0.98] font-extrabold tracking-[-0.03em] text-balance">{{ c.hero.title }}</h1>
          </div>
          <div class="origin-left scale-[0.92]"><ConsoleShowcase :copy="c.mocks" compact /></div>
        </div>`,
    }),
  };
}

const meta: Meta = { title: "Page/OgCard", parameters: { layout: "fullscreen" } };
export default meta;

export const En = card("en");
export const Id = card("id");
