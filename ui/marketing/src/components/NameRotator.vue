<script setup lang="ts">
import { onBeforeUnmount, onMounted, shallowRef, useTemplateRef, watch } from "vue";
import { useInView } from "../shared/composables/useInView";
import { useReducedMotion } from "../shared/composables/useReducedMotion";

const props = withDefaults(defineProps<{ names: string[]; intervalMs?: number }>(), {
  intervalMs: 2800,
});

const root = useTemplateRef<HTMLElement>("root");
const inView = useInView(root, { once: true });
const reduced = useReducedMotion();
const last = props.names.length - 1;
const active = shallowRef(last);
let timer: ReturnType<typeof setInterval> | undefined;
let played = false;

function stop() {
  clearInterval(timer);
  timer = undefined;
}

function sync() {
  if (reduced.value) {
    stop();
    active.value = last;
    return;
  }
  if (!inView.value || played || timer) return;
  played = true;
  active.value = 0;
  timer = setInterval(() => {
    active.value += 1;
    if (active.value >= last) stop();
  }, props.intervalMs);
}

onMounted(() => watch([inView, reduced], sync, { immediate: true }));
onBeforeUnmount(stop);
</script>

<template>
  <span ref="root">
    <span class="sr-only">{{ names.join(", ") }}</span>
    <span aria-hidden="true" class="inline-grid">
      <span
        v-for="(name, i) in names"
        :key="name"
        data-name
        class="col-start-1 row-start-1 justify-self-start whitespace-nowrap"
        :class="i === active ? 'animate-rise-in' : 'invisible'"
        >{{ name }}</span
      >
    </span>
  </span>
</template>
