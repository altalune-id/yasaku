<script setup lang="ts">
import type { SiteCopy } from "../content";
import { site } from "../site";

defineProps<{ copy: SiteCopy["finalCta"] }>();
</script>

<template>
  <section id="get-started" class="px-4 pb-16 md:px-6 md:pb-28">
    <div
      class="mx-auto grid max-w-6xl justify-items-center gap-7 rounded-[2rem] bg-primary px-5 py-12 text-center text-primary-content md:px-16 md:py-20"
    >
      <h2 class="max-w-3xl font-display text-display-2 font-extrabold text-balance">
        {{ copy.title }}
      </h2>
      <div class="grid w-full justify-items-center gap-3">
        <a
          class="btn btn-accent btn-pop h-14 w-full px-7 text-lg [--pop:4px] sm:w-auto"
          :href="site.loginUrl"
          >{{ copy.cta }}</a
        >
        <p class="text-sm opacity-90">{{ copy.note }}</p>
      </div>
    </div>
  </section>
</template>
