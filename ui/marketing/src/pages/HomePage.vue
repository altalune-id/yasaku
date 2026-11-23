<script setup lang="ts">
import { useHead } from "@unhead/vue";
import { computed } from "vue";
import BrandMark from "../components/BrandMark.vue";
import { copy as allCopy } from "../content";
import AiSection from "../sections/AiSection.vue";
import FaqSection from "../sections/FaqSection.vue";
import FeaturesSection from "../sections/FeaturesSection.vue";
import FinalCtaSection from "../sections/FinalCtaSection.vue";
import HeroSection from "../sections/HeroSection.vue";
import OpenSourceSection from "../sections/OpenSourceSection.vue";
import PaydaySection from "../sections/PaydaySection.vue";
import SiteFooter from "../shared/components/SiteFooter.vue";
import SiteNav from "../shared/components/SiteNav.vue";
import { pageHead } from "../seo";
import { type Locale, site } from "../site";

const props = defineProps<{ locale: Locale }>();

const c = computed(() => allCopy[props.locale]);
const other = computed(() => site.pages.find((page) => page.locale !== props.locale)!);
const localeHref = computed(() => site.base + other.value.path.slice(1));
const localeNames = { en: "English", id: "Bahasa Indonesia" } as const;
const locales = computed(() =>
  site.pages.map((page) => ({
    code: page.locale.toUpperCase(),
    href: site.base + page.path.slice(1),
    hreflang: page.hreflang,
    name: localeNames[page.locale],
    current: page.locale === props.locale,
  })),
);
const copyright = computed(() =>
  c.value.footer.copyright.replace("{year}", String(__BUILD_YEAR__)),
);
const github = computed(() => ({
  href: site.githubUrl,
  label: c.value.nav.githubLabel,
  stars: __GITHUB_STARS__,
}));
const mark = `${import.meta.env.BASE_URL}yasaku-mark.svg`;
const companyLogo = `${import.meta.env.BASE_URL}altalune-logo.png`;

useHead(pageHead(props.locale, __SITE_NOINDEX__));
</script>

<template>
  <a
    href="#main"
    class="btn btn-sm sr-only z-[60] focus:not-sr-only focus:min-h-8 focus:px-4 focus:fixed focus:top-4 focus:left-4"
    >{{ c.nav.skipLabel }}</a
  >
  <SiteNav
    app-name="Yasaku"
    :logo-src="mark"
    :links="c.nav.links"
    :cta-label="c.nav.cta"
    :cta-href="site.loginUrl"
    cta-class="btn btn-accent btn-pop px-4 text-[0.9375rem] [--pop:2px]"
    :locales="locales"
    :locale-label="c.nav.languageLabel"
    :theme-label="c.nav.themeLabel"
    :menu-label="c.nav.menuLabel"
    :github="github"
  >
    <template #logo><BrandMark class="size-7" /></template>
  </SiteNav>
  <main id="main" tabindex="-1" class="outline-none">
    <HeroSection :copy="c.hero" :mocks="c.mocks" />
    <PaydaySection
      :copy="c.payday"
      :mocks="c.mocks"
      :pause-label="c.nav.pauseLabel"
      :play-label="c.nav.playLabel"
    />
    <FeaturesSection :copy="c.features" :mocks="c.mocks" />
    <AiSection
      :copy="c.ai"
      :copy-bar="c.copyBar"
      :mocks="c.mocks"
      :pause-label="c.nav.pauseLabel"
      :play-label="c.nav.playLabel"
    />
    <OpenSourceSection :copy="c.openSource" />
    <FaqSection :copy="c.faq" />
    <FinalCtaSection :copy="c.finalCta" />
  </main>
  <SiteFooter
    :apps="[...site.apps]"
    :legal="[
      { label: c.footer.terms, href: site.termsUrl },
      { label: c.footer.privacy, href: site.privacyUrl },
    ]"
    :resources="[{ label: c.footer.github, href: site.githubUrl }]"
    :tagline="c.footer.tagline"
    :copyright="copyright"
    :note="c.footer.trademarks"
    :company-logo-src="companyLogo"
    :locale-href="localeHref"
    :locale-label="c.nav.localeLabel"
    :column-titles="c.footer.columnTitles"
  >
    <template #brand>
      <p class="mb-2 flex items-center gap-2 font-display text-2xl font-extrabold">
        <BrandMark class="size-7" />Yasaku
      </p>
    </template>
  </SiteFooter>
</template>
