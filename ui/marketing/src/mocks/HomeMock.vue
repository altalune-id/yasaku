<script setup lang="ts">
import type { SiteCopy } from "../content";
import { homeDays, moneyIn, moneyOut, slot, spending, type Slot } from "./data";
import { formatIDR } from "./format";
import MockIcon from "./MockIcon.vue";
import { signed } from "./money";

defineProps<{ copy: SiteCopy["mocks"] }>();

const tiles: { key: keyof SiteCopy["mocks"]["categories"]; slot: Slot }[] = [
  { key: "food", slot: "primary" },
  { key: "transport", slot: "info" },
  { key: "shopping", slot: "success" },
  { key: "bills", slot: "accent" },
];

const slotOf: Partial<Record<keyof SiteCopy["mocks"]["categories"], Slot>> = {
  ...Object.fromEntries(spending.map((s) => [s.key, s.slot])),
  salary: "success",
};

const dayTotal = (rows: (typeof homeDays)[number]["rows"]) => {
  const net = rows.reduce((sum, r) => sum + (r.income ? r.amount : -r.amount), 0);
  return signed(Math.abs(net), net >= 0 ? "income" : "expense");
};
</script>

<template>
  <div class="grid min-w-0 grid-cols-1 gap-3.5">
    <p class="flex items-center gap-2 text-xl font-bold">
      <MockIcon name="house" class="size-5 text-primary" />{{ copy.home }}
    </p>
    <div class="grid gap-0.5 rounded-box bg-primary px-4 py-3.5 text-primary-content">
      <span class="text-[0.8125rem] opacity-85">{{ copy.netPeriod }}</span>
      <span class="text-[1.625rem] font-bold whitespace-nowrap tabular-nums">{{
        formatIDR(moneyIn - moneyOut)
      }}</span>
      <span class="text-xs opacity-85">{{ copy.period }}</span>
    </div>
    <div class="grid gap-2">
      <p class="text-[0.8125rem] font-semibold text-base-content/80">{{ copy.quickAdd }}</p>
      <div class="flex flex-wrap gap-1.5">
        <span
          v-for="t in tiles"
          :key="t.key"
          class="badge h-7.5 gap-1.5 rounded-full border-base-300 bg-transparent px-3 text-[0.8125rem] font-semibold whitespace-nowrap"
          ><span class="size-2 rounded-full" :class="slot[t.slot].dot"></span
          >{{ copy.categories[t.key] }}</span
        >
      </div>
    </div>
    <div v-for="day in homeDays" :key="day.label" class="grid gap-1">
      <p class="flex justify-between gap-2 px-0.5 text-xs font-semibold text-base-content/80">
        <span>{{ copy[day.label] }}</span
        ><span class="whitespace-nowrap tabular-nums">{{ dayTotal(day.rows) }}</span>
      </p>
      <ul class="divide-y divide-base-300 overflow-hidden rounded-box border border-base-300">
        <li v-for="row in day.rows" :key="row.title" class="flex items-center gap-2.5 px-3 py-2.5">
          <span
            class="grid size-8 shrink-0 place-items-center rounded-[0.625rem] text-[0.8125rem] font-bold"
            :class="[
              slot[slotOf[row.category] ?? 'secondary'].tint,
              slot[slotOf[row.category] ?? 'secondary'].text,
            ]"
            >{{ copy.tx[row.title].charAt(0) }}</span
          >
          <span class="grid min-w-0 flex-1">
            <span class="truncate text-sm font-semibold">{{ copy.tx[row.title] }}</span>
            <span class="truncate text-xs text-base-content/80"
              >{{ copy.categories[row.category] }} · {{ copy.wallets[row.wallet] }}</span
            >
          </span>
          <span
            class="shrink-0 text-sm font-semibold whitespace-nowrap tabular-nums"
            :class="row.income && 'text-success'"
            >{{ signed(row.amount, row.income ? "income" : "expense") }}</span
          >
        </li>
      </ul>
    </div>
  </div>
</template>
