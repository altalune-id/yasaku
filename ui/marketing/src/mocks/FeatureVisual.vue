<script setup lang="ts">
import type { FeatureVariant, SiteCopy } from "../content/types";
import { slot, type Slot, wallets } from "./data";
import { formatIDR } from "./format";
import MockIcon from "./MockIcon.vue";
import { signed } from "./money";

defineProps<{ copy: SiteCopy["mocks"]; variant: FeatureVariant }>();

const tiles: { key: keyof SiteCopy["mocks"]["categories"]; slot: Slot }[] = [
  { key: "food", slot: "primary" },
  { key: "transport", slot: "info" },
  { key: "shopping", slot: "success" },
  { key: "bills", slot: "accent" },
  { key: "phone", slot: "error" },
  { key: "health", slot: "secondary" },
  { key: "salary", slot: "success" },
  { key: "freelance", slot: "info" },
  { key: "other", slot: "secondary" },
];
const searchAmounts = [25_000, 28_000, 25_000];
const total = wallets.reduce((sum, w) => sum + w.balance, 0);
</script>

<template>
  <div aria-hidden="true" class="grid h-full min-w-0 content-start gap-2 text-sm">
    <template v-if="variant === 'wallets'">
      <p class="flex flex-wrap items-baseline justify-between gap-2">
        <span class="text-[0.8125rem] text-base-content/80">{{ copy.totalAcross }}</span>
        <span class="text-xl font-bold whitespace-nowrap tabular-nums">{{ formatIDR(total) }}</span>
      </p>
      <ul
        class="divide-y divide-base-300 overflow-hidden rounded-xl border-[1.5px] border-base-300"
      >
        <li v-for="w in wallets" :key="w.key" class="flex justify-between gap-2 px-3 py-2">
          <span>{{ copy.wallets[w.key] }}</span>
          <span class="font-semibold whitespace-nowrap tabular-nums">{{
            formatIDR(w.balance)
          }}</span>
        </li>
      </ul>
      <div class="flex flex-wrap gap-1.5">
        <span
          class="btn btn-xs pointer-events-none h-7 border-[1.5px] border-base-300 bg-transparent px-2.5"
          >{{ copy.adjustBalance }}</span
        >
        <span class="btn btn-primary btn-xs pointer-events-none h-7 px-2.5">{{
          copy.newWallet
        }}</span>
      </div>
    </template>

    <div v-else-if="variant === 'categories'" class="flex flex-wrap gap-1.5">
      <span
        v-for="c in tiles"
        :key="c.key"
        class="badge h-7.5 gap-1.5 rounded-full border-[1.5px] border-base-300 bg-transparent px-2.75 text-[0.8125rem] font-semibold whitespace-nowrap"
        ><span class="size-2 rounded-full" :class="slot[c.slot].dot"></span
        >{{ copy.categories[c.key] }}</span
      >
    </div>

    <template v-else-if="variant === 'search'">
      <p
        class="flex h-9.5 items-center gap-2 rounded-field border-[1.5px] border-primary px-3 ring-3 ring-primary/18"
      >
        <svg
          viewBox="0 0 24 24"
          class="size-3.75 shrink-0"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          stroke-linecap="round"
        >
          <circle cx="11" cy="11" r="8" />
          <path d="m21 21-4.3-4.3" />
        </svg>
        <span class="flex-1">{{ copy.searchQuery }}</span>
        <span class="text-xs whitespace-nowrap text-base-content/80">{{ copy.searchCount }}</span>
      </p>
      <ul
        class="divide-y divide-base-300 overflow-hidden rounded-xl border-[1.5px] border-base-300"
      >
        <li
          v-for="(day, i) in copy.searchDays"
          :key="day"
          class="flex items-center justify-between gap-2 px-3 py-1.75"
        >
          <span class="grid">
            <span class="font-semibold">{{ copy.tx.coffee }}</span>
            <span class="text-xs text-base-content/80">{{ day }} · {{ copy.wallets.cash }}</span>
          </span>
          <span class="font-semibold whitespace-nowrap tabular-nums">{{
            signed(searchAmounts[i]!, "expense")
          }}</span>
        </li>
      </ul>
    </template>

    <template v-else-if="variant === 'transfer'">
      <div class="grid gap-1.5 rounded-xl border-[1.5px] border-base-300 px-3 py-2.5">
        <p class="flex justify-between gap-2">
          <span class="grid">
            <span class="font-semibold">{{ copy.tx.topUp }}</span>
            <span class="text-xs text-base-content/80"
              >{{ copy.wallets.bank }} → {{ copy.wallets.cash }}</span
            >
          </span>
          <span class="font-bold whitespace-nowrap tabular-nums">{{
            signed(500_000, "transfer")
          }}</span>
        </p>
        <p class="flex justify-between gap-2 text-[0.8125rem] text-base-content/80">
          <span>{{ copy.wallets.bank }}</span
          ><span class="font-semibold whitespace-nowrap text-base-content tabular-nums">{{
            signed(500_000, "expense")
          }}</span>
        </p>
        <p class="flex justify-between gap-2 text-[0.8125rem] text-base-content/80">
          <span>{{ copy.wallets.cash }}</span
          ><span class="font-semibold whitespace-nowrap text-success tabular-nums">{{
            signed(500_000, "income")
          }}</span>
        </p>
      </div>
      <p class="flex items-center justify-between gap-2 rounded-xl bg-base-200 px-3 py-2">
        <span class="text-[0.8125rem]">{{ copy.moneyOut }}</span>
        <span class="font-bold whitespace-nowrap tabular-nums">{{ formatIDR(0) }}</span>
      </p>
    </template>

    <div
      v-else-if="variant === 'adjust'"
      class="grid gap-2 rounded-xl border-[1.5px] border-base-300 p-3"
    >
      <span class="font-bold">{{ copy.adjustBalance }} · {{ copy.wallets.cash }}</span>
      <span class="text-xs text-base-content/80">{{ copy.realBalance }}</span>
      <span
        class="flex h-9.5 items-center rounded-field border-[1.5px] border-base-300 px-3 font-semibold tabular-nums"
        >{{ formatIDR(1_200_000) }}</span
      >
      <p class="flex justify-between gap-2 text-[0.8125rem]">
        <span class="text-base-content/80">{{ copy.balanceFix }}</span>
        <span class="font-semibold whitespace-nowrap tabular-nums">{{
          signed(25_000, "expense")
        }}</span>
      </p>
    </div>

    <ul v-else class="grid gap-0.5 rounded-xl border-[1.5px] border-base-300 p-1.5">
      <li
        v-for="(name, i) in copy.ledgers"
        :key="name"
        class="flex items-center gap-2.5 rounded-[0.5625rem] px-2 py-1.75"
        :class="i === 0 && 'bg-base-200'"
      >
        <span
          class="grid size-7.5 shrink-0 place-items-center rounded-[0.5625rem] text-[0.8125rem] font-bold"
          :class="i === 0 ? 'bg-primary text-primary-content' : 'bg-base-200'"
          >{{ name.slice(0, 1) }}</span
        >
        <span class="grid min-w-0 flex-1">
          <span class="font-semibold">{{ name }}</span>
          <span class="text-xs text-base-content/80">{{ copy.ledgerMembers[i] }}</span>
        </span>
        <MockIcon v-if="i === 0" name="check" class="size-4 text-primary" />
      </li>
    </ul>
  </div>
</template>
