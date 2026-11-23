<script setup lang="ts">
import type { SiteCopy } from "../content";
import FeatureCard from "./FeatureCard.vue";

defineProps<{ copy: SiteCopy["features"]; mocks: SiteCopy["mocks"] }>();
</script>

<template>
  <section id="features" class="bg-base-200 py-16 md:py-24">
    <div class="mx-auto max-w-6xl px-4 md:px-6">
      <p class="text-eyebrow font-semibold text-primary uppercase">
        {{ copy.eyebrow }}
      </p>
      <h2 class="mt-3 mb-10 max-w-2xl font-display text-display-2 font-extrabold text-balance">
        {{ copy.title }}
      </h2>
      <div
        class="grid grid-cols-[repeat(auto-fit,minmax(min(100%,18.75rem),1fr))] gap-3.5 md:gap-5"
      >
        <FeatureCard
          v-for="(card, i) in copy.cards"
          :key="card.visual"
          :card="card"
          :mocks="mocks"
          :index="i"
        />
      </div>
    </div>
  </section>
</template>
