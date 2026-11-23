<script setup lang="ts">
import { usePreferredReducedMotion } from "@vueuse/core";
import { computed, onBeforeUnmount, onMounted, shallowRef, useTemplateRef, watch } from "vue";
import type { SiteCopy } from "../content";
import BrowserFrame from "../mocks/BrowserFrame.vue";
import PeriodCloseMock from "../mocks/PeriodCloseMock.vue";
import PeriodListMock from "../mocks/PeriodListMock.vue";
import { useInView } from "../shared/composables/useInView";
import { useReducedMotion } from "../shared/composables/useReducedMotion";

const props = defineProps<{
  copy: SiteCopy["payday"];
  mocks: SiteCopy["mocks"];
  pauseLabel: string;
  playLabel: string;
}>();

const STEP_MS = 3600;
const root = useTemplateRef<HTMLElement>("root");
const inView = useInView(root);
const reduced = useReducedMotion();
const step = shallowRef(0);
const pinned = shallowRef(false);
const mounted = shallowRef(false);
const preferred = usePreferredReducedMotion();
const showPause = computed(() => mounted.value && preferred.value !== "reduce");
const running = shallowRef(false);
let timer: ReturnType<typeof setInterval> | undefined;

const active = computed(() => (reduced.value && !pinned.value ? 1 : step.value));
const alt = computed(() => (active.value === 1 ? props.mocks.periodAlt : props.mocks.periodsAlt));

function sync() {
  const want = inView.value && !reduced.value && !pinned.value;
  if (want === running.value) return;
  running.value = want;
  clearInterval(timer);
  if (!want) return;
  timer = setInterval(() => {
    step.value = (step.value + 1) % props.copy.steps.length;
  }, STEP_MS);
}

function pick(i: number) {
  step.value = i;
  pinned.value = true;
}

onMounted(() => {
  mounted.value = true;
  watch([inView, reduced, pinned], sync, { immediate: true });
});
onBeforeUnmount(() => clearInterval(timer));
</script>

<template>
  <section id="payday" ref="root" class="py-16 md:py-28">
    <div
      class="mx-auto grid max-w-6xl grid-cols-1 items-center gap-10 px-4 md:px-6 lg:grid-cols-2 lg:gap-16"
    >
      <div>
        <p class="text-eyebrow font-semibold text-primary uppercase">{{ copy.eyebrow }}</p>
        <h2 class="mt-3 font-display text-display-2 font-extrabold text-balance">
          {{ copy.title }}
        </h2>
        <p class="mt-4 max-w-xl text-pretty text-base-content/80">{{ copy.body }}</p>
        <ol class="mt-6 grid gap-2">
          <li v-for="(s, i) in copy.steps" :key="s.title">
            <button
              type="button"
              data-payday-step
              class="relative grid w-full grid-cols-[auto_1fr] items-baseline gap-x-3.5 gap-y-1 overflow-hidden rounded-box border px-4 py-3.5 text-start transition-colors duration-300 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-base-content"
              :class="
                active === i
                  ? 'border-primary bg-primary/8'
                  : 'border-base-300 bg-base-100 hover:border-base-content/40'
              "
              :aria-current="active === i ? 'step' : undefined"
              @click="pick(i)"
            >
              <span
                class="row-span-2 w-6 shrink-0 font-display text-2xl font-extrabold"
                :class="active === i ? 'text-primary' : 'text-base-content/70'"
                >{{ i + 1 }}</span
              >
              <span class="text-lg font-bold">{{ s.title }}</span>
              <span class="text-sm text-base-content/80">{{ s.body }}</span>
              <span
                v-if="active === i && running"
                :key="step"
                aria-hidden="true"
                class="step-progress absolute inset-x-0 bottom-0 h-0.75 bg-primary"
              ></span>
            </button>
          </li>
        </ol>
        <div class="mt-3">
          <button
            v-if="showPause"
            type="button"
            data-story-pause
            class="btn btn-sm h-8 gap-2 rounded-full border-base-300 bg-base-100 ps-2 pe-3 text-[0.8125rem] font-semibold text-base-content/80 hover:border-base-content hover:text-base-content focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-base-content"
            :aria-pressed="pinned ? 'true' : 'false'"
            @click="pinned = !pinned"
          >
            <span class="grid size-5 place-items-center" aria-hidden="true">
              <svg v-if="pinned" viewBox="0 0 24 24" class="size-3" fill="currentColor">
                <path d="M7 4v16l13-8z" />
              </svg>
              <svg v-else viewBox="0 0 24 24" class="size-3" fill="currentColor">
                <rect x="6" y="4" width="4" height="16" rx="1" />
                <rect x="14" y="4" width="4" height="16" rx="1" />
              </svg>
            </span>
            {{ pinned ? playLabel : pauseLabel }}
          </button>
        </div>
      </div>
      <BrowserFrame path="/periods" :alt="alt" class="mx-auto w-full max-w-lg">
        <div class="grid grid-cols-[minmax(0,1fr)]">
          <PeriodListMock
            :copy="mocks"
            bare
            class="col-start-1 row-start-1"
            :class="active === 0 ? 'animate-rise-in' : 'invisible'"
          />
          <PeriodCloseMock
            v-if="active === 1"
            :copy="mocks"
            bare
            class="col-start-1 row-start-1 animate-rise-in"
          />
          <PeriodCloseMock v-else :copy="mocks" bare class="invisible col-start-1 row-start-1" />
          <PeriodListMock
            :copy="mocks"
            view="next"
            bare
            class="col-start-1 row-start-1"
            :class="active === 2 ? 'animate-rise-in' : 'invisible'"
          />
        </div>
      </BrowserFrame>
    </div>
  </section>
</template>

<style scoped>
.step-progress {
  transform-origin: left;
  animation: step-progress 3600ms linear both;
}

@keyframes step-progress {
  from {
    transform: scaleX(0);
  }
  to {
    transform: scaleX(1);
  }
}
</style>
