<script setup lang="ts">
import { track } from "../shared/track";
import type { SiteCopy } from "../content";
import BrowserFrame from "../mocks/BrowserFrame.vue";
import ConsoleShowcase from "../mocks/ConsoleShowcase.vue";
import HomeMock from "../mocks/HomeMock.vue";
import { site } from "../site";

defineProps<{ copy: SiteCopy["hero"]; mocks: SiteCopy["mocks"] }>();

function openSetup() {
  track("hero_add_to_ai_click");
  window.dispatchEvent(new CustomEvent("ai-setup:open"));
}
</script>

<template>
  <section id="top" class="relative -mt-20 px-4 pt-28 md:px-6 md:pt-36">
    <div
      aria-hidden="true"
      class="hero-glow pointer-events-none absolute inset-x-0 top-0 bottom-1/3"
    ></div>
    <div class="relative mx-auto grid max-w-6xl gap-6 md:gap-8">
      <div class="grid animate-rise-in gap-5">
        <p
          class="badge badge-outline badge-primary justify-self-start bg-base-100 text-eyebrow font-semibold uppercase"
        >
          {{ copy.eyebrow }}
        </p>
        <h1 class="max-w-5xl font-display text-hero font-extrabold text-balance">
          {{ copy.title }}
        </h1>
      </div>
      <div
        class="flex animate-rise-in flex-wrap items-center justify-between gap-x-10 gap-y-5 [animation-delay:120ms]"
      >
        <p class="max-w-lg text-lead text-pretty">{{ copy.sub }}</p>
        <div class="flex w-full flex-col gap-3 sm:w-auto sm:flex-row">
          <a class="btn btn-accent btn-pop h-13 px-6 text-[1.0625rem]" :href="site.loginUrl">{{
            copy.primaryCta
          }}</a>
          <a
            class="btn btn-pop bg-base-100 h-13 px-6 text-[1.0625rem]"
            href="#connect"
            @click="openSetup"
            >{{ copy.secondaryCta }}</a
          >
        </div>
      </div>
    </div>
    <div
      id="product"
      class="relative mx-auto mt-10 max-w-6xl animate-rise-in scroll-mt-24 [animation-delay:250ms] md:mt-16"
    >
      <div class="min-[1080px]:me-[22%]"><ConsoleShowcase :copy="mocks" /></div>
      <BrowserFrame
        path="/"
        :alt="mocks.homeAlt"
        class="absolute end-0 top-[clamp(60px,10%,120px)] hidden w-[min(34%,380px)] min-[1080px]:block"
        ><HomeMock :copy="mocks"
      /></BrowserFrame>
    </div>
  </section>
</template>

<style scoped>
.hero-glow {
  background:
    radial-gradient(
      60% 70% at 85% 10%,
      color-mix(in oklch, var(--color-primary) 16%, transparent),
      transparent 70%
    ),
    radial-gradient(
      40% 50% at 10% 40%,
      color-mix(in oklch, var(--brand) 12%, transparent),
      transparent 70%
    );
}
</style>
