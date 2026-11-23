<script setup lang="ts">
import type { SiteCopy } from "../content";
import { moneyIn, moneyOut, wallets } from "./data";
import { formatIDR } from "./format";
import MockIcon from "./MockIcon.vue";

withDefaults(defineProps<{ copy: SiteCopy["mocks"]; bare?: boolean }>(), { bare: false });

const opening = wallets.reduce((sum, w) => sum + w.balance, 0) - (moneyIn - moneyOut);
</script>

<template>
  <div
    :role="bare ? undefined : 'img'"
    :aria-label="bare ? undefined : copy.periodAlt"
    :class="!bare && 'p-4 sm:p-6'"
  >
    <div
      :aria-hidden="bare ? undefined : 'true'"
      class="grid gap-3 text-sm"
      :class="!bare && 'rounded-box border border-base-300 bg-base-100 p-4'"
    >
      <div class="flex flex-wrap items-center justify-between gap-2.5">
        <p class="text-xl font-bold whitespace-nowrap">{{ copy.period }}</p>
        <span class="badge badge-soft gap-1.5 font-semibold"
          ><MockIcon name="lock" class="lock size-3" />{{ copy.closed }}</span
        >
      </div>
      <div class="flex items-center gap-2.5">
        <span
          class="grid size-13 shrink-0 place-items-center rounded-field border border-base-300 font-display text-2xl font-extrabold"
          >25</span
        >
        <span class="h-0.5 flex-1 overflow-hidden rounded-full bg-base-300"
          ><span class="period-fill block h-full bg-primary"></span
        ></span>
        <span
          class="grid size-13 shrink-0 place-items-center rounded-field bg-primary font-display text-2xl font-extrabold text-primary-content"
          >24</span
        >
      </div>
      <div class="grid gap-2 rounded-box border border-base-300 px-3.5 py-3">
        <p class="text-[0.8125rem] font-semibold text-base-content/80">
          {{ copy.whatGetsLocked }}
        </p>
        <p class="flex justify-between gap-2">
          <span>{{ copy.moneyIn }}</span
          ><span class="font-semibold whitespace-nowrap text-success tabular-nums">{{
            formatIDR(moneyIn)
          }}</span>
        </p>
        <p class="flex justify-between gap-2">
          <span>{{ copy.moneyOut }}</span
          ><span class="font-semibold whitespace-nowrap tabular-nums">{{
            formatIDR(moneyOut)
          }}</span>
        </p>
        <p class="flex justify-between gap-2 border-t border-base-300 pt-2 font-bold">
          <span>{{ copy.net }}</span
          ><span class="whitespace-nowrap tabular-nums">{{ formatIDR(moneyIn - moneyOut) }}</span>
        </p>
        <p class="flex flex-wrap justify-between gap-x-2 text-[0.8125rem] text-base-content/80">
          <span>{{ copy.entries }}</span
          ><span class="whitespace-nowrap tabular-nums"
            >{{ copy.startingBalances }} {{ formatIDR(opening) }}</span
          >
        </p>
      </div>
      <div class="grid gap-2 rounded-box border border-base-300 px-3.5 py-3">
        <p class="text-[0.8125rem] font-semibold text-base-content/80">
          {{ copy.walletBalances }}
        </p>
        <p v-for="w in wallets" :key="w.key" class="flex justify-between gap-2">
          <span>{{ copy.wallets[w.key] }}</span
          ><span class="font-semibold whitespace-nowrap tabular-nums">{{
            formatIDR(w.balance)
          }}</span>
        </p>
      </div>
      <div class="flex flex-wrap items-center gap-2.5">
        <span class="btn btn-outline btn-sm pointer-events-none border-base-300">{{
          copy.reopen
        }}</span>
        <span class="min-w-40 flex-1 text-[0.8125rem] text-base-content/80">{{
          copy.reopenHint
        }}</span>
      </div>
    </div>
  </div>
</template>

<style scoped>
.period-fill {
  transform-origin: left;
  animation: period-fill 1600ms cubic-bezier(0.65, 0, 0.35, 1) both;
}

.lock {
  animation: lock-snap 300ms cubic-bezier(0.22, 1, 0.36, 1) 1600ms both;
}

@keyframes period-fill {
  0% {
    transform: scaleX(0);
  }
  100% {
    transform: scaleX(1);
  }
}

@keyframes lock-snap {
  0% {
    transform: translateY(-3px) scale(1.2);
  }
  100% {
    transform: none;
  }
}
</style>
