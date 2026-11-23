<script setup lang="ts">
import { onBeforeUnmount, onMounted, shallowRef, useTemplateRef, watch } from "vue";
import type { SiteCopy } from "../content";
import { useInView } from "../shared/composables/useInView";
import { useReducedMotion } from "../shared/composables/useReducedMotion";
import StepChatMock from "./StepChatMock.vue";

defineProps<{ copy: SiteCopy["mocks"] }>();

const HOLD_MS = [1100, 1500, 700, 3000];
const root = useTemplateRef<HTMLElement>("root");
const inView = useInView(root);
const reduced = useReducedMotion();
const stage = shallowRef(3);
let timer: ReturnType<typeof setTimeout> | undefined;

function tick() {
  timer = setTimeout(() => {
    stage.value = (stage.value + 1) % HOLD_MS.length;
    tick();
  }, HOLD_MS[stage.value]);
}

function sync() {
  clearTimeout(timer);
  timer = undefined;
  if (reduced.value || !inView.value) {
    stage.value = 3;
    return;
  }
  stage.value = 0;
  tick();
}

onMounted(() => watch([inView, reduced], sync, { immediate: true }));
onBeforeUnmount(() => clearTimeout(timer));
</script>

<template>
  <div ref="root" role="img" :aria-label="copy.chatAlt" class="w-full min-w-0">
    <div
      aria-hidden="true"
      class="overflow-hidden rounded-box border border-base-300 bg-base-100 text-start text-base-content shadow-pop"
    >
      <div class="flex h-11 items-center gap-1.5 bg-base-200 px-3.5">
        <span v-for="i in 3" :key="i" class="size-2.5 rounded-full bg-base-300"></span>
      </div>
      <div class="min-h-75 p-4 sm:p-5.5">
        <StepChatMock :copy="copy" :stage="stage" full />
      </div>
    </div>
  </div>
</template>
