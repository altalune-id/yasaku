<script setup lang="ts">
import type { PropType } from "vue";
import SpotlightLoop from "./SpotlightLoop.vue";

interface Step {
  title: string;
  body: string;
  typed: string;
}

defineProps({
  steps: {
    type: Array as PropType<Step[]>,
    required: true,
    validator: (value: Step[]) => value.length === 3,
  },
  intervalMs: { type: Number, default: 3000 },
  frozen: { type: Number, default: undefined },
});

defineSlots<{
  "step-visual-1"?(): unknown;
  "step-visual-2"?(): unknown;
  "step-visual-3"?(): unknown;
}>();
</script>

<template>
  <SpotlightLoop
    v-slot="{ index, active }"
    :count="steps.length"
    :interval-ms="intervalMs"
    :frozen="frozen"
    class="grid grid-cols-1 items-start gap-4 md:gap-5.5 lg:grid-cols-3"
  >
    <div
      data-step
      class="card card-border transition-[transform,box-shadow,border-color,opacity] duration-300 ease-out"
      :class="
        active
          ? 'border-primary bg-[color-mix(in_oklab,var(--color-primary)_8%,var(--color-base-100))] ring-2 ring-primary/30 shadow-pop -translate-y-1'
          : 'border-base-300 bg-base-100 opacity-90 shadow-card'
      "
    >
      <div class="card-body gap-3">
        <span
          class="badge badge-primary font-display size-8 rounded-full font-extrabold"
          :class="active && 'animate-button-pulse'"
          >{{ index + 1 }}</span
        >
        <h3 class="card-title text-[1.1875rem] font-bold">{{ steps[index]!.title }}</h3>
        <p v-if="steps[index]!.body" class="text-[0.9375rem] text-pretty">
          {{ steps[index]!.body }}
        </p>
        <p v-if="steps[index]!.typed" class="font-mono text-sm text-base-content/80">
          <span
            data-typed
            class="inline-block max-w-full overflow-hidden align-bottom whitespace-nowrap"
            :class="active && 'animate-type-on'"
            :style="{
              '--type-steps': steps[index]!.typed.length,
              '--type-width': `${steps[index]!.typed.length}ch`,
            }"
            >{{ steps[index]!.typed }}</span
          ><span v-if="active" aria-hidden="true" class="motion-cursor animate-cursor-blink"
            >|</span
          >
        </p>
        <slot :name="`step-visual-${index + 1}` as 'step-visual-1'" />
      </div>
    </div>
  </SpotlightLoop>
</template>
