<script setup lang="ts">
defineProps<{
  apps: { name: string; href: string }[];
  legal: { label: string; href: string }[];
  resources: { label: string; href: string }[];
  tagline: string;
  copyright: string;
  note?: string;
  companyLogoSrc?: string;
  localeHref: string;
  localeLabel: string;
  columnTitles: { apps: string; legal: string; resources: string };
}>();

defineSlots<{ brand?(): unknown }>();
</script>

<template>
  <footer class="border-t-[1.5px] border-base-300 bg-base-200">
    <div class="mx-auto grid max-w-6xl gap-10 px-4 pt-14 pb-8 md:px-6">
      <div
        class="grid grid-cols-2 gap-x-8 gap-y-10 sm:grid-cols-3 lg:grid-cols-[minmax(0,1fr)_auto_auto_auto] lg:gap-x-16"
      >
        <aside
          class="col-span-2 grid max-w-[520px] content-start gap-2.5 sm:col-span-3 lg:col-span-1"
        >
          <slot name="brand" />
          <p class="text-[1.0625rem] leading-[1.55] text-base-content/80">{{ tagline }}</p>
        </aside>
        <nav class="grid content-start gap-3 text-[1.0625rem]">
          <h2
            data-footer-title
            class="mb-1 text-[0.8125rem] font-bold tracking-[0.06em] uppercase text-base-content/80"
          >
            {{ columnTitles.apps }}
          </h2>
          <a v-for="app in apps" :key="app.href" :href="app.href" class="link link-hover">{{
            app.name
          }}</a>
        </nav>
        <nav class="grid content-start gap-3 text-[1.0625rem]">
          <h2
            data-footer-title
            class="mb-1 text-[0.8125rem] font-bold tracking-[0.06em] uppercase text-base-content/80"
          >
            {{ columnTitles.legal }}
          </h2>
          <a v-for="item in legal" :key="item.href" :href="item.href" class="link link-hover">{{
            item.label
          }}</a>
        </nav>
        <nav class="grid content-start gap-3 text-[1.0625rem]">
          <h2
            data-footer-title
            class="mb-1 text-[0.8125rem] font-bold tracking-[0.06em] uppercase text-base-content/80"
          >
            {{ columnTitles.resources }}
          </h2>
          <a v-for="item in resources" :key="item.href" :href="item.href" class="link link-hover">{{
            item.label
          }}</a>
          <a :href="localeHref" class="link link-hover">{{ localeLabel }}</a>
        </nav>
      </div>
      <div
        class="flex flex-wrap justify-between gap-x-6 gap-y-2 border-t border-base-300 pt-5 text-sm text-base-content/80"
      >
        <p class="inline-flex items-center gap-2">
          <img
            v-if="companyLogoSrc"
            :src="companyLogoSrc"
            alt=""
            width="18"
            height="18"
            class="size-4.5 shrink-0"
          />{{ copyright }}
        </p>
        <p v-if="note" data-note>{{ note }}</p>
      </div>
    </div>
  </footer>
</template>
