<script setup lang="ts">
import { usePreferredReducedMotion } from "@vueuse/core";
import { computed, onMounted, shallowRef } from "vue";
import { setReducedMotion, useReducedMotion } from "../composables/useReducedMotion";

defineProps<{ pauseLabel: string; playLabel: string }>();

const mounted = shallowRef(false);
const reduced = useReducedMotion();
const preferred = usePreferredReducedMotion();
const visible = computed(() => mounted.value && preferred.value !== "reduce");

onMounted(() => {
  mounted.value = true;
});
</script>

<template>
  <button
    v-if="visible"
    type="button"
    class="btn btn-circle btn-sm border-base-300 bg-base-100/85 shadow-card backdrop-blur focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-base-content"
    :aria-label="reduced ? playLabel : pauseLabel"
    :aria-pressed="reduced ? 'true' : 'false'"
    @click="setReducedMotion(!reduced)"
  >
    <svg v-if="reduced" viewBox="0 0 24 24" class="size-4" fill="currentColor" aria-hidden="true">
      <path d="M8 5.5v13a1 1 0 0 0 1.52.85l10.4-6.5a1 1 0 0 0 0-1.7l-10.4-6.5A1 1 0 0 0 8 5.5Z" />
    </svg>
    <svg v-else viewBox="0 0 24 24" class="size-4" fill="currentColor" aria-hidden="true">
      <rect x="6" y="5" width="4" height="14" rx="1" />
      <rect x="14" y="5" width="4" height="14" rx="1" />
    </svg>
  </button>
</template>
