<script setup lang="ts">
import { useTemplateRef } from "vue";
import type { SiteCopy } from "../content";
import { useInView } from "../shared/composables/useInView";
import { incomeFlows, moneyIn, moneyOut, share, slot, spending } from "./data";
import { formatIDR } from "./format";
import { SANKEY_BOX, sankeyLayout } from "./sankeyModel";

const props = defineProps<{ copy: SiteCopy["mocks"] }>();

const { width: W, height: H, nodeWidth: NODE } = SANKEY_BOX;
const layout = sankeyLayout();

const tone = (id: string) => {
  if (id.startsWith("in:") || id === "saved") return "text-success";
  if (id.startsWith("w:")) return "text-primary";
  const s = spending.find((c) => `c:${c.key}` === id)!;
  return slot[s.slot].text;
};
const label = (id: string) => {
  if (id === "saved") return props.copy.saved;
  const [kind, key] = id.split(":") as [string, string];
  if (kind === "w") return props.copy.wallets[key as keyof typeof props.copy.wallets];
  return props.copy.categories[key as keyof typeof props.copy.categories];
};

const root = useTemplateRef<HTMLElement>("root");
useInView(root, { once: true });
</script>

<template>
  <div ref="root" class="card min-w-0 border border-base-300 bg-base-100">
    <div class="card-body gap-3 p-4">
      <p class="card-title text-base">{{ copy.whereMoneyWent }}</p>

      <svg :viewBox="`0 0 ${W} ${H}`" class="sankey hidden w-full lg:block" role="presentation">
        <g class="ribbons">
          <path
            v-for="l in layout.links"
            :key="`${l.source}-${l.target}`"
            :d="l.d"
            fill="currentColor"
            :class="tone(l.target)"
            opacity="0.3"
          />
        </g>
        <rect
          v-for="n in layout.nodes"
          :key="n.id"
          :x="n.x"
          :y="n.y"
          :width="NODE"
          :height="Math.max(n.h, 2)"
          rx="3"
          fill="currentColor"
          :class="tone(n.id)"
        />
        <g
          class="fill-base-content"
          font-size="12"
          paint-order="stroke"
          stroke="var(--color-base-100)"
          stroke-width="3"
          stroke-linejoin="round"
        >
          <template v-for="n in layout.nodes" :key="`t-${n.id}`">
            <text v-if="n.column === 0" :x="n.x - 8" :y="n.y + n.h / 2" text-anchor="end">
              <tspan font-weight="600">{{ label(n.id) }}</tspan>
              <tspan :x="n.x - 8" dy="15">{{ formatIDR(n.value) }}</tspan>
            </text>
            <text v-else-if="n.column === 1" :x="n.x" :y="n.y - 8">
              <tspan font-weight="600">{{ label(n.id) }}</tspan> {{ formatIDR(n.value) }}
            </text>
            <text v-else-if="n.column === 2" :x="n.x + NODE + 8" :y="n.y + n.h / 2 - 3">
              <tspan font-weight="600">{{ label(n.id) }}</tspan>
              <tspan :x="n.x + NODE + 8" dy="15">{{ formatIDR(n.value) }}</tspan>
            </text>
            <text v-else :x="n.x + NODE + 8" :y="n.y + n.h / 2" dominant-baseline="middle">
              <tspan font-weight="600">{{ label(n.id) }}</tspan> {{ formatIDR(n.value) }}
            </text>
          </template>
        </g>
      </svg>

      <div class="space-y-3 text-sm lg:hidden">
        <p class="flex justify-between gap-2">
          <span class="font-semibold">{{ copy.moneyIn }}</span>
          <span class="font-semibold tabular-nums text-success">{{ formatIDR(moneyIn) }}</span>
        </p>
        <ul class="space-y-1.5 text-base-content/80">
          <li v-for="f in incomeFlows" :key="f.from" class="flex justify-between gap-2">
            <span>{{ copy.categories[f.from] }} → {{ copy.wallets[f.wallet] }}</span>
            <span class="tabular-nums">{{ formatIDR(f.amount) }}</span>
          </li>
        </ul>
        <p class="flex justify-between gap-2 border-t border-base-300 pt-3">
          <span class="font-semibold">{{ copy.moneyOut }}</span>
          <span class="font-semibold tabular-nums">{{ formatIDR(moneyOut) }}</span>
        </p>
        <ul class="space-y-2.5">
          <li v-for="s in spending" :key="s.key">
            <span class="flex justify-between gap-2">
              <span>{{ copy.categories[s.key] }}</span>
              <span class="tabular-nums">{{ formatIDR(s.amount) }}</span>
            </span>
            <progress
              class="progress h-1.5 w-full"
              :class="slot[s.slot].progress"
              :value="share(s.amount)"
              max="100"
            ></progress>
          </li>
        </ul>
        <p class="flex justify-between gap-2 border-t border-base-300 pt-3">
          <span class="font-semibold">{{ copy.saved }}</span>
          <span class="font-semibold tabular-nums text-success">{{
            formatIDR(moneyIn - moneyOut)
          }}</span>
        </p>
      </div>
    </div>
  </div>
</template>

<style scoped>
.ribbons {
  animation: sankey-draw 1200ms cubic-bezier(0.22, 1, 0.36, 1) both;
}

[data-inview="false"] .ribbons {
  animation-play-state: paused;
}

@keyframes sankey-draw {
  0% {
    clip-path: inset(0 100% 0 0);
  }
  100% {
    clip-path: inset(0 0 0 0);
  }
}
</style>
