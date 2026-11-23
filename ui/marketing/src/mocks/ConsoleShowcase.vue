<script setup lang="ts">
import type { SiteCopy } from "../content";
import BrowserFrame from "./BrowserFrame.vue";
import { moneyIn, moneyOut } from "./data";
import { formatIDR } from "./format";
import MockIcon from "./MockIcon.vue";
import SankeyChart from "./SankeyChart.vue";

withDefaults(defineProps<{ copy: SiteCopy["mocks"]; compact?: boolean }>(), { compact: false });
</script>

<template>
  <BrowserFrame path="/reports" :alt="copy.showcaseAlt">
    <div class="grid min-w-0 grid-cols-1 gap-4">
      <div class="flex flex-wrap items-center justify-between gap-3">
        <p class="flex items-center gap-2 text-2xl font-bold tracking-tight">
          <MockIcon name="chart-pie" class="size-5 text-primary" />{{ copy.reports }}
        </p>
        <div class="join">
          <span class="btn btn-ghost btn-square join-item btn-sm pointer-events-none"
            ><MockIcon name="chevron-left" class="size-4"
          /></span>
          <span class="btn btn-ghost join-item btn-sm pointer-events-none font-semibold">{{
            copy.period
          }}</span>
          <span class="btn btn-ghost btn-square join-item btn-sm pointer-events-none"
            ><MockIcon name="chevron-right" class="size-4"
          /></span>
        </div>
      </div>
      <div
        v-if="!compact"
        class="grid grid-cols-[repeat(auto-fit,minmax(min(100%,11.25rem),1fr))] gap-2.5"
      >
        <div class="grid gap-0.5 rounded-box border border-base-300 px-3.5 py-3">
          <span class="text-[0.8125rem] text-base-content/80">{{ copy.netPeriod }}</span>
          <span data-net class="text-[1.375rem] font-bold whitespace-nowrap tabular-nums">{{
            formatIDR(moneyIn - moneyOut)
          }}</span>
          <span class="text-xs font-semibold text-success">{{ copy.vsLast }}</span>
        </div>
        <div class="grid content-start gap-0.5 rounded-box border border-base-300 px-3.5 py-3">
          <span class="text-[0.8125rem] text-base-content/80">{{ copy.moneyIn }}</span>
          <span class="text-[1.375rem] font-bold whitespace-nowrap text-success tabular-nums">{{
            formatIDR(moneyIn)
          }}</span>
        </div>
        <div class="grid content-start gap-0.5 rounded-box border border-base-300 px-3.5 py-3">
          <span class="text-[0.8125rem] text-base-content/80">{{ copy.moneyOut }}</span>
          <span class="text-[1.375rem] font-bold whitespace-nowrap tabular-nums">{{
            formatIDR(moneyOut)
          }}</span>
        </div>
      </div>
      <SankeyChart :copy="copy" />
    </div>
  </BrowserFrame>
</template>
