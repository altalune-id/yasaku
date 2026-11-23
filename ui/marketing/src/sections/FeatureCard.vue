<script setup lang="ts">
import { useTemplateRef } from "vue";
import type { SiteCopy } from "../content";
import FeatureVisual from "../mocks/FeatureVisual.vue";
import { useInView } from "../shared/composables/useInView";

defineProps<{
  card: SiteCopy["features"]["cards"][number];
  mocks: SiteCopy["mocks"];
  index: number;
}>();

const root = useTemplateRef<HTMLElement>("root");
useInView(root, { once: true });
</script>

<template>
  <article
    ref="root"
    data-feature
    class="reveal row-span-2 grid min-w-0 grid-rows-subgrid gap-y-4 rounded-box border-[1.5px] border-base-300 bg-base-100 p-5 shadow-card"
    :style="{ '--reveal-index': index }"
  >
    <div class="text-[0.9375rem] leading-normal text-pretty text-base-content/80">
      <h3 class="inline font-bold text-base-content">{{ card.title }}</h3>
      {{ " " }}
      <p class="inline">{{ card.body }}</p>
    </div>
    <div data-visual><FeatureVisual :copy="mocks" :variant="card.visual" /></div>
  </article>
</template>
