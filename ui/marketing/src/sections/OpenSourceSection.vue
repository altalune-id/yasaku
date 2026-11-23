<script setup lang="ts">
import type { SiteCopy } from "../content";
import MockIcon from "../mocks/MockIcon.vue";
import GitHubMark from "../shared/components/GitHubMark.vue";
import { site } from "../site";

defineProps<{ copy: SiteCopy["openSource"] }>();

const packageUrl = `${site.githubUrl}/pkgs/container/yasaku`;
const docsUrl = `${site.githubUrl}/blob/main/docs/deployment/README.md`;
</script>

<template>
  <section id="open-source" class="py-16 md:py-24">
    <div class="mx-auto max-w-6xl px-4 md:px-6">
      <div class="flex flex-wrap items-end justify-between gap-x-10 gap-y-4">
        <div class="max-w-[47.5rem] min-w-0 flex-[1_1_28rem]">
          <p class="text-eyebrow font-semibold text-primary uppercase">{{ copy.license }}</p>
          <h2 class="mt-3 font-display text-display-2 font-extrabold text-balance">
            {{ copy.title }}
          </h2>
        </div>
        <p class="max-w-[21.25rem] text-base-content/80">{{ copy.body }}</p>
      </div>
      <div class="mt-10 grid grid-cols-[repeat(auto-fit,minmax(min(100%,20rem),1fr))] gap-5">
        <article
          class="grid content-start gap-3.5 rounded-box border border-base-300 bg-base-100 p-5 shadow-card md:p-6.5"
        >
          <h3 class="font-display text-[1.375rem] font-extrabold">{{ copy.cloud.title }}</h3>
          <p class="text-[0.9375rem] text-base-content/80">{{ copy.cloud.body }}</p>
          <p
            class="flex h-9.5 items-center overflow-hidden rounded-field border border-base-300 bg-base-200 px-3 font-mono text-[0.8125rem]"
          >
            <span class="truncate">yasaku.altalune.id/login</span>
          </p>
          <ul class="grid gap-2 text-[0.9375rem]">
            <li v-for="point in copy.cloud.points" :key="point" class="flex items-start gap-2.5">
              <MockIcon name="check" class="mt-0.5 size-4.5 shrink-0 text-primary" />{{ point }}
            </li>
          </ul>
          <a class="btn btn-accent btn-pop h-11.5 justify-self-start px-5" :href="site.loginUrl">{{
            copy.cloud.cta
          }}</a>
        </article>
        <article
          class="grid min-w-0 content-start gap-3.5 rounded-box border border-base-300 bg-base-100 p-5 shadow-card md:p-6.5"
        >
          <h3 class="font-display text-[1.375rem] font-extrabold">{{ copy.selfHost.title }}</h3>
          <p class="text-[0.9375rem] text-base-content/80">{{ copy.selfHost.body }}</p>
          <ul class="divide-y divide-base-300 overflow-hidden rounded-box border border-base-300">
            <li>
              <a
                :href="packageUrl"
                class="grid gap-0.5 px-3.5 py-3 transition-colors hover:bg-base-200"
                ><span class="font-semibold">{{ copy.selfHost.items[0]!.title }}</span
                ><code class="font-mono text-[0.8125rem] break-all text-base-content/80">{{
                  copy.selfHost.items[0]!.detail
                }}</code></a
              >
            </li>
            <li v-for="item in copy.selfHost.items.slice(1)" :key="item.title">
              <a
                :href="item.detailHref"
                class="grid gap-0.5 px-3.5 py-3 transition-colors hover:bg-base-200"
                ><span class="font-semibold">{{ item.title }}</span
                ><span class="text-[0.8125rem] font-semibold text-primary"
                  >{{ item.detail }} →</span
                ></a
              >
            </li>
          </ul>
          <p class="text-sm text-base-content/80">{{ copy.selfHost.note }}</p>
          <div class="flex flex-wrap gap-2">
            <a
              class="btn h-10.5 gap-2 border-base-300 bg-transparent px-4 hover:border-base-content"
              :href="site.githubUrl"
              ><GitHubMark class="size-4" />{{ copy.github }}</a
            >
            <a class="btn btn-ghost h-10.5 px-4 text-primary" :href="docsUrl"
              >{{ copy.selfHost.docs }} →</a
            >
          </div>
        </article>
      </div>
    </div>
  </section>
</template>
