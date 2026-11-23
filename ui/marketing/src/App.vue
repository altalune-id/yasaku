<script setup lang="ts">
import { useHead } from "@unhead/vue";
import bricolage from "@fontsource-variable/bricolage-grotesque/files/bricolage-grotesque-latin-wght-normal.woff2?url";
import figtree from "@fontsource-variable/figtree/files/figtree-latin-wght-normal.woff2?url";
import "./styles/main.css";

useHead({
  link: [bricolage, figtree].map((href) => ({
    rel: "preload",
    as: "font",
    type: "font/woff2",
    href,
    crossorigin: "",
  })),
});
</script>

<template>
  <RouterView />
</template>
