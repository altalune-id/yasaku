<script setup lang="ts">
defineProps<{
  label: string;
  locales: { code: string; href: string; hreflang: string; name: string; current: boolean }[];
}>();
</script>

<template>
  <div role="group" :aria-label="label" class="flex rounded-full border border-base-300 p-0.75">
    <a
      v-for="l in locales"
      :key="l.code"
      :href="l.href"
      :hreflang="l.hreflang"
      :lang="l.hreflang"
      :title="l.name"
      :aria-current="l.current ? 'true' : undefined"
      class="btn btn-xs h-6.5 min-w-9 rounded-full border-0 px-2.5 font-bold"
      :class="l.current ? 'btn-neutral' : 'btn-ghost'"
      >{{ l.code }}</a
    >
  </div>
</template>
