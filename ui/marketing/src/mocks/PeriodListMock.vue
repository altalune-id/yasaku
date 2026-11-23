<script setup lang="ts">
import { computed } from "vue";
import type { SiteCopy } from "../content";
import { cashflow, wallets } from "./data";
import { formatIDR } from "./format";
import MockIcon from "./MockIcon.vue";

const props = withDefaults(
  defineProps<{ copy: SiteCopy["mocks"]; view?: "list" | "next"; bare?: boolean }>(),
  { view: "list", bare: false },
);

const rows = computed(() => {
  const past = [...props.copy.periodRanges].reverse().map((range, i) => {
    const flow = cashflow[cashflow.length - 1 - i]!;
    return {
      range,
      detail: `${props.copy.net} ${formatIDR(flow.in - flow.out)}`,
      open: i === 0 && props.view === "list",
      fresh: false,
    };
  });
  if (props.view === "list") return past;
  const start = wallets.reduce((sum, w) => sum + w.balance, 0);
  return [
    {
      range: props.copy.nextPeriod,
      detail: `${props.copy.startingBalances} ${formatIDR(start)}`,
      open: true,
      fresh: true,
    },
    ...past.slice(0, 3),
  ];
});
</script>

<template>
  <div
    :role="bare ? undefined : 'img'"
    :aria-label="bare ? undefined : copy.periodsAlt"
    :class="!bare && 'p-4 sm:p-6'"
  >
    <div :aria-hidden="bare ? undefined : 'true'" class="space-y-3">
      <div class="flex flex-wrap items-center justify-between gap-3">
        <p class="text-lg font-semibold">{{ copy.periodsTitle }}</p>
        <span
          data-close
          class="btn btn-primary btn-sm pointer-events-none gap-1.5"
          :class="view === 'list' && 'ring-4 ring-primary/25'"
          ><MockIcon name="lock" class="size-3.5" />{{ copy.closeMonth }}</span
        >
      </div>
      <ul
        class="divide-y divide-base-300 overflow-hidden rounded-box border border-base-300 bg-base-100"
      >
        <li
          v-for="row in rows"
          :key="row.range"
          class="flex items-center gap-3 px-4 py-3"
          :class="[row.open && 'bg-primary/5', row.fresh && 'animate-rise-in']"
        >
          <span
            class="flex size-9 shrink-0 items-center justify-center rounded-full bg-base-200 text-primary"
          >
            <MockIcon :name="row.open ? 'calendar-check' : 'lock'" class="size-4" />
          </span>
          <span class="min-w-0 flex-1">
            <span class="block font-medium whitespace-nowrap">{{ row.range }}</span>
            <span class="block truncate text-xs text-base-content/80 tabular-nums">{{
              row.detail
            }}</span>
          </span>
          <span v-if="row.open" class="badge badge-success badge-soft">{{ copy.open }}</span>
          <span v-else class="badge badge-ghost">{{ copy.closed }}</span>
        </li>
      </ul>
    </div>
  </div>
</template>
