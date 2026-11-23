<script setup lang="ts">
import {
  computed,
  onBeforeUnmount,
  onMounted,
  type Ref,
  shallowRef,
  useTemplateRef,
  watch,
} from "vue";
import { useInView } from "../shared/composables/useInView";
import { useReducedMotion } from "../shared/composables/useReducedMotion";

const props = withDefaults(defineProps<{ count: number; intervalMs?: number; frozen?: number }>(), {
  intervalMs: 3000,
  frozen: undefined,
});

defineSlots<{ default(props: { index: number; active: boolean }): unknown }>();

const root = useTemplateRef<HTMLElement>("root");
const inView = useInView(root);
const reduced = useReducedMotion();
const step = shallowRef(0);
const active = computed(() => props.frozen ?? step.value);
let timer: ReturnType<typeof setInterval> | undefined;

function stop() {
  clearInterval(timer);
  timer = undefined;
}

function sync() {
  if (props.frozen !== undefined || reduced.value || !inView.value || props.count < 2)
    return stop();
  if (timer) return;
  timer = setInterval(() => {
    step.value = (step.value + 1) % props.count;
  }, props.intervalMs);
}

onMounted(() => watch([inView, reduced, () => props.frozen], sync, { immediate: true }));
onBeforeUnmount(stop);

defineExpose({ active: active as Readonly<Ref<number>> });
</script>

<template>
  <div ref="root">
    <template v-for="index in count" :key="index">
      <slot :index="index - 1" :active="reduced || active === index - 1" />
    </template>
  </div>
</template>
