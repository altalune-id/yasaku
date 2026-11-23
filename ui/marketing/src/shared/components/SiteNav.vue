<script setup lang="ts">
import { onClickOutside, onKeyStroke } from "@vueuse/core";
import { useTemplateRef } from "vue";
import GitHubMark from "./GitHubMark.vue";
import GitHubStars from "./GitHubStars.vue";
import LocaleSwitch from "./LocaleSwitch.vue";
import ThemeToggle from "./ThemeToggle.vue";

withDefaults(
  defineProps<{
    appName: string;
    logoSrc: string;
    links: { label: string; href: string }[];
    ctaLabel: string;
    ctaHref: string;
    locales: { code: string; href: string; hreflang: string; name: string; current: boolean }[];
    localeLabel: string;
    themeLabel: string;
    menuLabel: string;
    github: { href: string; label: string; stars: number | null };
    ctaClass?: string;
    widthClass?: string;
  }>(),
  { ctaClass: "btn btn-accent btn-brutal", widthClass: "max-w-6xl" },
);

defineSlots<{ logo?(): unknown }>();

const menu = useTemplateRef<HTMLDetailsElement>("menu");

function closeOnLink(event: MouseEvent) {
  if ((event.target as Element).closest("a") && menu.value) menu.value.open = false;
}

function dismiss() {
  if (!menu.value?.open) return;
  menu.value.open = false;
  menu.value.querySelector("summary")?.focus();
}

onKeyStroke("Escape", dismiss);
onClickOutside(menu, dismiss);
</script>

<template>
  <header class="sticky top-0 z-50 px-3 pt-3.5 md:px-6">
    <nav
      :class="widthClass"
      class="navbar relative mx-auto min-h-14.5 gap-2 rounded-full border border-base-300 bg-base-100/80 ps-2 pe-2.5 backdrop-blur-md lg:ps-3.5"
    >
      <div class="navbar-start w-auto min-w-0 flex-1 gap-1 lg:flex-none">
        <details ref="menu" class="dropdown static lg:hidden" @click="closeOnLink">
          <summary class="btn btn-ghost btn-circle btn-sm size-9" :aria-label="menuLabel">
            <svg
              class="size-5"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
              stroke-linecap="round"
              aria-hidden="true"
            >
              <path d="M4 7h16M4 12h16M4 17h16" />
            </svg>
          </summary>
          <ul
            class="menu dropdown-content inset-x-0 top-full z-10 mt-2 w-auto gap-0.5 rounded-box border border-base-300 bg-base-100 p-2 pb-3 shadow-pop"
          >
            <li v-for="link in links" :key="link.href">
              <a :href="link.href" class="py-3 font-medium">{{ link.label }}</a>
            </li>
            <li class="min-[420px]:hidden">
              <a :href="github.href" class="py-3 font-medium"
                ><GitHubMark class="size-5" />{{ github.label }}</a
              >
            </li>
            <li class="mt-1.5 min-[560px]:hidden">
              <a :href="ctaHref" :class="ctaClass" class="h-11 w-full">{{ ctaLabel }}</a>
            </li>
          </ul>
        </details>
        <a
          href="#top"
          data-brand
          class="flex min-w-0 items-center gap-2 rounded-full font-display text-xl font-extrabold focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-base-content"
        >
          <slot name="logo"
            ><img :src="logoSrc" alt="" width="28" height="28" class="size-7 shrink-0"
          /></slot>
          <span data-brand-name class="min-w-0 truncate">{{ appName }}</span>
        </a>
      </div>
      <div class="navbar-center mx-auto hidden lg:flex">
        <ul class="menu menu-horizontal menu-md gap-0.5 p-0">
          <li v-for="link in links" :key="link.href">
            <a
              :href="link.href"
              class="rounded-full font-medium whitespace-nowrap focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-base-content xl:px-3.5 xl:text-[0.9375rem]"
              >{{ link.label }}</a
            >
          </li>
        </ul>
      </div>
      <div class="navbar-end w-auto shrink-0 gap-1 lg:flex-none">
        <div data-cluster class="flex items-center gap-1">
          <GitHubStars
            class="max-[419px]:hidden"
            :href="github.href"
            :label="github.label"
            :stars="github.stars"
          />
          <LocaleSwitch :label="localeLabel" :locales="locales" />
          <ThemeToggle :label="themeLabel" />
        </div>
        <a :href="ctaHref" :class="ctaClass" class="ms-1 max-[559px]:hidden">{{ ctaLabel }}</a>
      </div>
    </nav>
  </header>
</template>
