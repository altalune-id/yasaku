<script setup lang="ts">
import { computed } from "vue";
import { starsLabel } from "../github";
import GitHubMark from "./GitHubMark.vue";

const props = defineProps<{ href: string; label: string; stars: number | null }>();

const count = computed(() => starsLabel(props.stars));
</script>

<template>
  <a :href="href" :aria-label="label" class="btn btn-ghost btn-sm gap-1.5 px-2">
    <GitHubMark class="size-5" />
    <span v-if="count" data-stars aria-hidden="true" class="text-sm font-semibold tabular-nums">{{
      count
    }}</span>
  </a>
</template>
