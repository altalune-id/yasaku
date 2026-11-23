<script setup lang="ts">
import { onMounted, shallowRef } from "vue";

defineProps<{ label: string }>();

const STORAGE_KEY = "altalune-theme";
const dark = shallowRef(false);

function apply(isDark: boolean) {
  dark.value = isDark;
  const theme = isDark ? "site-dark" : "site-light";
  document.documentElement.dataset.theme = theme;
  try {
    localStorage.setItem(STORAGE_KEY, theme);
  } catch {}
}

onMounted(() => {
  let stored: string | null = null;
  try {
    stored = localStorage.getItem(STORAGE_KEY);
  } catch {}
  if (stored === "site-light" || stored === "site-dark") {
    document.documentElement.dataset.theme = stored;
    dark.value = stored === "site-dark";
    return;
  }
  const current = document.documentElement.dataset.theme;
  dark.value = current
    ? current === "site-dark"
    : window.matchMedia("(prefers-color-scheme: dark)").matches;
});
</script>

<template>
  <label
    class="swap swap-rotate btn btn-ghost btn-circle btn-sm has-[input:focus-visible]:outline-2 has-[input:focus-visible]:outline-solid has-[input:focus-visible]:outline-offset-2 has-[input:focus-visible]:outline-base-content"
  >
    <input
      type="checkbox"
      :aria-label="label"
      :checked="dark"
      @change="apply(($event.target as HTMLInputElement).checked)"
    />
    <svg
      class="swap-on size-5"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      stroke-width="2"
      aria-hidden="true"
    >
      <circle cx="12" cy="12" r="4" />
      <path
        d="M12 2v2M12 20v2M4.93 4.93l1.41 1.41M17.66 17.66l1.41 1.41M2 12h2M20 12h2M6.34 17.66l-1.41 1.41M19.07 4.93l-1.41 1.41"
      />
    </svg>
    <svg
      class="swap-off size-5"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      stroke-width="2"
      aria-hidden="true"
    >
      <path d="M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z" />
    </svg>
  </label>
</template>
