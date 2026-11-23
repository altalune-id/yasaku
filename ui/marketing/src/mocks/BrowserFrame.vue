<script setup lang="ts">
defineProps<{ path: string; alt: string }>();
</script>

<template>
  <div role="img" :aria-label="alt" class="min-w-0">
    <div
      aria-hidden="true"
      class="mockup-browser h-full border border-base-300 bg-base-200 text-start shadow-pop"
    >
      <div class="mockup-browser-toolbar">
        <div class="input h-8 w-full max-w-sm min-w-0 flex-nowrap">
          <span class="min-w-0 truncate whitespace-nowrap"
            >yasaku.altalune.id<span data-path class="hidden sm:inline">{{ path }}</span></span
          >
        </div>
      </div>
      <div class="min-w-0 bg-base-100 p-3 sm:p-5"><slot /></div>
    </div>
  </div>
</template>
