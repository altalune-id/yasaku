<script setup lang="ts">
import BrandMark from "../components/BrandMark.vue";
import type { SiteCopy } from "../content";

defineProps<{ copy: SiteCopy["mocks"] }>();
</script>

<template>
  <div
    aria-hidden="true"
    class="pointer-events-none overflow-hidden rounded-box border border-base-300 bg-base-100 select-none"
  >
    <p class="flex h-8 items-center gap-1.5 bg-base-200 px-2.5 text-xs text-base-content/80">
      <span class="size-1.75 rounded-full bg-base-300"></span
      ><span class="size-1.75 rounded-full bg-base-300"></span
      ><span class="ms-1.5 whitespace-nowrap">yasaku.altalune.id</span>
    </p>
    <div class="grid justify-items-center gap-3 px-4 py-4.5">
      <p class="flex items-center gap-2 font-display text-lg font-extrabold">
        <BrandMark class="size-5.5" />Yasaku
      </p>
      <p
        class="grid h-9.5 w-full place-items-center rounded-field bg-primary text-sm font-semibold text-primary-content"
      >
        {{ copy.continueWith }}
      </p>
    </div>
  </div>
</template>
