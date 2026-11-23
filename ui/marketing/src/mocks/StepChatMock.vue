<script setup lang="ts">
import BrandMark from "../components/BrandMark.vue";
import type { SiteCopy } from "../content";
import { formatIDR } from "./format";

const props = withDefaults(
  defineProps<{ copy: SiteCopy["mocks"]; stage?: number; full?: boolean }>(),
  { stage: 3, full: false },
);

const reply = props.copy.reply.replace("{amount}", formatIDR(25_000));
</script>

<template>
  <div
    aria-hidden="true"
    class="pointer-events-none flex flex-col select-none"
    :class="full ? 'gap-3 text-[0.9375rem]' : 'gap-2.5 text-sm'"
  >
    <span
      class="max-w-[85%] self-end rounded-[1.125rem] rounded-br-sm bg-primary px-3.5 py-2.5 font-medium text-primary-content"
      >{{ copy.userMessage }}</span
    >
    <div v-if="stage >= 1" class="grid max-w-[21.25rem] animate-rise-in gap-2">
      <span
        v-if="full"
        class="badge h-6.5 gap-1.5 justify-self-start rounded-full border-base-300 bg-transparent px-2.5 text-xs font-semibold text-base-content/80"
        ><BrandMark class="size-3.5" />Yasaku · {{ copy.toolLabel }}</span
      >
      <div
        class="grid gap-2.5 rounded-box border border-base-300 bg-base-100 px-3.5 py-3"
        :class="full && 'shadow-card'"
      >
        <span class="text-xs font-semibold text-primary">{{ copy.preview }}</span>
        <div class="flex items-center gap-2.5">
          <span
            v-if="full"
            class="grid size-8.5 shrink-0 place-items-center rounded-[0.625rem] bg-primary/15 font-bold text-primary"
            >{{ copy.tx.coffee.charAt(0) }}</span
          >
          <span class="grid min-w-0 flex-1">
            <span class="font-semibold">{{ copy.tx.coffee }}</span>
            <span class="text-xs text-base-content/80"
              >{{ copy.expense }} · {{ copy.categories.food }} · {{ copy.wallets.cash }}</span
            >
          </span>
          <span class="font-bold whitespace-nowrap tabular-nums">{{ formatIDR(25_000) }}</span>
        </div>
        <div class="flex gap-2">
          <span
            class="btn btn-primary btn-sm transition-[transform,box-shadow] duration-200"
            :class="stage === 2 && 'scale-95 ring-4 ring-primary/30'"
            >{{ copy.confirm }}</span
          >
          <span class="btn btn-outline btn-sm border-base-300">{{ copy.cancel }}</span>
        </div>
      </div>
    </div>
    <span
      v-if="stage >= 3"
      class="flex max-w-[85%] animate-rise-in items-center gap-2 self-start rounded-[1.125rem] rounded-bl-sm bg-base-200 px-3.5 py-2.5"
      ><span class="grid size-4.5 shrink-0 place-items-center rounded-full bg-success"
        ><svg
          viewBox="0 0 24 24"
          class="size-3 text-base-100"
          fill="none"
          stroke="currentColor"
          stroke-width="3.2"
          stroke-linecap="round"
          stroke-linejoin="round"
        >
          <path d="M20 6 9 17l-5-5" /></svg></span
      >{{ reply }}</span
    >
  </div>
</template>
