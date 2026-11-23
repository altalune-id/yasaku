<script setup lang="ts">
import { computed } from "vue";
import type { SiteCopy } from "../content";
import FaqList from "../components/FaqList.vue";
import { site } from "../site";

const props = defineProps<{ copy: SiteCopy["faq"] }>();

const lastAnswer = computed(() => props.copy.items[6]!.a.replace(site.githubUrl, ""));
</script>

<template>
  <section id="faq" class="pt-10 pb-16 md:pt-16 md:pb-28">
    <div class="px-4 md:px-6">
      <FaqList :title="copy.title" :items="copy.items">
        <template #item-7>
          <p>
            {{ lastAnswer
            }}<a class="link link-primary font-semibold" :href="site.githubUrl">{{
              site.githubUrl
            }}</a>
          </p>
        </template>
      </FaqList>
    </div>
  </section>
</template>
