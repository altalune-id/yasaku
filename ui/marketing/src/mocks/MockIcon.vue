<script setup lang="ts">
import { type IconName, icons } from "./icons";

defineProps<{ name: IconName }>();
</script>

<template>
  <component :is="icons[name]" aria-hidden="true" />
</template>
