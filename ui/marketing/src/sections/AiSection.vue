<script setup lang="ts">
import { shallowRef } from "vue";
import { site } from "../site";
import AiSetupSwitch from "../components/AiSetupSwitch.vue";
import NameRotator from "../components/NameRotator.vue";
import PlugInStory from "../components/PlugInStory.vue";
import type { SiteCopy } from "../content";
import ChatPreviewMock from "../mocks/ChatPreviewMock.vue";
import StepChatMock from "../mocks/StepChatMock.vue";
import StepSignInMock from "../mocks/StepSignInMock.vue";
import StagePauseButton from "../shared/components/StagePauseButton.vue";

defineProps<{
  copy: SiteCopy["ai"];
  copyBar: SiteCopy["copyBar"];
  mocks: SiteCopy["mocks"];
  pauseLabel: string;
  playLabel: string;
}>();

const pinned = shallowRef<number | undefined>(undefined);
</script>

<template>
  <section id="ai" data-theme="site-dark" class="bg-neutral py-16 text-base-content md:py-28">
    <div class="mx-auto grid max-w-6xl gap-10 px-4 md:gap-20 md:px-6">
      <div class="grid grid-cols-1 items-center gap-10 lg:grid-cols-2 lg:gap-16">
        <div class="grid gap-4.5">
          <p class="text-eyebrow font-semibold text-primary uppercase">{{ copy.eyebrow }}</p>
          <h2 class="font-display text-display-2 font-extrabold text-balance">
            {{ copy.title }}
          </h2>
          <p
            class="flex flex-wrap items-center gap-2.5 font-display text-[clamp(1.375rem,1rem+1vw,1.875rem)] font-bold"
          >
            <span>{{ copy.rotatorPrefix }}</span>
            <NameRotator :names="copy.rotatorNames" class="name-pill" />
            <StagePauseButton :pause-label="pauseLabel" :play-label="playLabel" />
          </p>
          <p class="max-w-xl text-base-content/80">{{ copy.body }} {{ copy.note }}</p>
        </div>
        <div class="relative min-w-0">
          <ChatPreviewMock :copy="mocks" />
          <div class="absolute end-2 top-1.5">
            <StagePauseButton :pause-label="pauseLabel" :play-label="playLabel" />
          </div>
        </div>
      </div>
      <div class="grid gap-4.5">
        <div class="flex items-center justify-between gap-3">
          <h3 class="font-display text-[clamp(1.375rem,1rem+1vw,1.75rem)] font-extrabold">
            {{ copy.stepsTitle }}
          </h3>
          <StagePauseButton :pause-label="pauseLabel" :play-label="playLabel" />
        </div>
        <PlugInStory :steps="copy.steps" :frozen="pinned">
          <template #step-visual-1>
            <div id="connect" class="scroll-mt-28">
              <AiSetupSwitch
                :url="site.mcpUrl"
                :prompt="copyBar.value"
                :copied-label="copyBar.copied"
                :manual-hint="copyBar.manual"
                :setup="copy.setup"
                @engage="pinned = 0"
              />
            </div>
          </template>
          <template #step-visual-2><StepSignInMock :copy="mocks" /></template>
          <template #step-visual-3><StepChatMock :copy="mocks" /></template>
        </PlugInStory>
      </div>
    </div>
  </section>
</template>

<style scoped>
.name-pill :deep([data-name]) {
  border-radius: 0.2em;
  background: var(--highlight);
  padding-inline: 0.18em;
}
</style>
