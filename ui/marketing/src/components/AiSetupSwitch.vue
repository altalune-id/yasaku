<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, shallowRef, useId, useTemplateRef } from "vue";
import { track } from "../shared/track";
import CopyPromptBar from "./CopyPromptBar.vue";

type Tab = "chat" | "coding";

defineProps<{
  url: string;
  prompt: string;
  copiedLabel: string;
  manualHint: string;
  setup: {
    groupLabel: string;
    chat: { tab: string; label: string; body: string; copy: string; success: string; idle: string };
    coding: { tab: string; label: string; body: string; copy: string; success: string };
  };
}>();

const emit = defineEmits<{ engage: [] }>();

const order: Tab[] = ["chat", "coding"];
const active = shallowRef<Tab>("chat");
const ids = { chat: useId(), coding: useId() };
const panelId = useId();
const root = useTemplateRef<HTMLElement>("root");

function select(tab: Tab, focusTab = false) {
  if (tab !== active.value && tab === "coding") track("tab_coding_select");
  active.value = tab;
  emit("engage");
  if (focusTab)
    nextTick(() => root.value?.querySelector<HTMLElement>(`#${CSS.escape(ids[tab])}`)?.focus());
}

function onKey(event: KeyboardEvent) {
  const i = order.indexOf(active.value);
  const next: Record<string, number> = {
    ArrowRight: i + 1,
    ArrowLeft: i - 1,
    Home: 0,
    End: order.length - 1,
  };
  if (!(event.key in next)) return;
  event.preventDefault();
  select(order[(next[event.key]! + order.length) % order.length]!, true);
}

async function open() {
  select("chat");
  await nextTick();
  root.value?.querySelector<HTMLElement>("[data-copy]")?.focus({ preventScroll: true });
}

onMounted(() => window.addEventListener("ai-setup:open", open));
onBeforeUnmount(() => window.removeEventListener("ai-setup:open", open));
</script>

<template>
  <div ref="root" class="space-y-3">
    <div
      role="tablist"
      :aria-label="setup.groupLabel"
      class="tabs tabs-box w-full flex-nowrap rounded-full border border-base-300 bg-base-200 p-1"
    >
      <button
        v-for="tab in order"
        :id="ids[tab]"
        :key="tab"
        type="button"
        role="tab"
        class="tab h-8.5 flex-1 rounded-full font-semibold whitespace-nowrap"
        :class="active === tab && 'tab-active'"
        :aria-selected="active === tab ? 'true' : 'false'"
        :aria-controls="panelId"
        :tabindex="active === tab ? 0 : -1"
        @click="select(tab)"
        @keydown="onKey"
      >
        {{ setup[tab].tab }}
      </button>
    </div>
    <div :id="panelId" role="tabpanel" :aria-labelledby="ids[active]" class="space-y-3">
      <p class="text-[0.9375rem] text-pretty">{{ setup[active].body }}</p>
      <CopyPromptBar
        v-if="active === 'chat'"
        key="chat"
        :prompt="url"
        :label="setup.chat.label"
        :copy-label="setup.chat.copy"
        :copied-label="copiedLabel"
        :manual-hint="manualHint"
        :hint="setup.chat.idle"
        :success-hint="setup.chat.success"
        @copied="track('copy_link_click')"
      />
      <CopyPromptBar
        v-else
        key="coding"
        :prompt="prompt"
        :label="setup.coding.label"
        :copy-label="setup.coding.copy"
        :copied-label="copiedLabel"
        :manual-hint="manualHint"
        :success-hint="setup.coding.success"
        @copied="track('copy_prompt_click')"
      />
    </div>
  </div>
</template>
