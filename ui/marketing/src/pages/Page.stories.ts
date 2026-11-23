import type { Meta, StoryObj } from "@storybook/vue3-vite";
import { copy } from "../content";
import AiSection from "../sections/AiSection.vue";
import FaqSection from "../sections/FaqSection.vue";
import FeaturesSection from "../sections/FeaturesSection.vue";
import FinalCtaSection from "../sections/FinalCtaSection.vue";
import HeroSection from "../sections/HeroSection.vue";
import OpenSourceSection from "../sections/OpenSourceSection.vue";
import PaydaySection from "../sections/PaydaySection.vue";
import HomePage from "./HomePage.vue";

const c = copy.en;
const components = {
  AiSection,
  FaqSection,
  FeaturesSection,
  FinalCtaSection,
  HeroSection,
  OpenSourceSection,
  PaydaySection,
  HomePage,
};

function story(template: string): StoryObj {
  return { render: () => ({ components, setup: () => ({ c }), template }) };
}

const meta: Meta = { title: "Page", parameters: { layout: "fullscreen" } };
export default meta;

export const Hero = story(
  `<div class="pt-20"><HeroSection :copy="c.hero" :mocks="c.mocks" /></div>`,
);
export const Features = story(`<FeaturesSection :copy="c.features" :mocks="c.mocks" />`);
export const Payday = story(
  `<PaydaySection :copy="c.payday" :mocks="c.mocks" :pause-label="c.nav.pauseLabel" :play-label="c.nav.playLabel" />`,
);
export const Ai = story(
  `<AiSection :copy="c.ai" :copy-bar="c.copyBar" :mocks="c.mocks" :pause-label="c.nav.pauseLabel" :play-label="c.nav.playLabel" />`,
);
export const OpenSource = story(`<OpenSourceSection :copy="c.openSource" />`);
export const Faq = story(`<FaqSection :copy="c.faq" />`);
export const FinalCta = story(`<FinalCtaSection :copy="c.finalCta" />`);
export const Full = story(`<HomePage locale="en" />`);
export const FullId = story(`<HomePage locale="id" />`);
