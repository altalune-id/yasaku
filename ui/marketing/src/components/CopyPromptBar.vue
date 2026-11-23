<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, shallowRef, useId, useTemplateRef } from "vue";

const props = defineProps<{
  prompt: string;
  label: string;
  copyLabel: string;
  copiedLabel: string;
  manualHint: string;
  hint?: string;
  successHint?: string;
}>();

const emit = defineEmits<{ copied: [] }>();

const hintId = useId();
const promptId = useId();
const copied = shallowRef(false);
const manual = shallowRef(false);
const done = shallowRef(false);
const announcement = shallowRef("");
const manualBox = useTemplateRef<HTMLTextAreaElement>("manualBox");
let timer: ReturnType<typeof setTimeout> | undefined;
const caption = computed(() => (done.value && props.successHint ? props.successHint : props.hint));

async function viaClipboardApi(text: string): Promise<boolean> {
  if (!navigator.clipboard?.writeText) return false;
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    return false;
  }
}

function viaExecCommand(text: string): boolean {
  const back = document.activeElement as HTMLElement | null;
  const area = document.createElement("textarea");
  area.value = text;
  area.readOnly = true;
  area.setAttribute("aria-hidden", "true");
  area.style.cssText = "position:fixed;top:0;left:0;opacity:0;pointer-events:none";
  document.body.append(area);
  area.focus();
  area.select();
  area.setSelectionRange(0, text.length);
  let ok = false;
  try {
    ok = document.execCommand("copy");
  } catch {
    ok = false;
  }
  area.remove();
  back?.focus();
  return ok;
}

async function copy() {
  const ok = (await viaClipboardApi(props.prompt)) || viaExecCommand(props.prompt);
  if (!ok) {
    manual.value = true;
    await nextTick();
    manualBox.value?.focus();
    manualBox.value?.setSelectionRange(0, props.prompt.length);
    return;
  }
  manual.value = false;
  done.value = true;
  copied.value = true;
  announcement.value = props.copiedLabel;
  emit("copied");
  clearTimeout(timer);
  timer = setTimeout(() => {
    copied.value = false;
    announcement.value = "";
  }, 2000);
}

onBeforeUnmount(() => clearTimeout(timer));
</script>

<template>
  <div class="grid w-full gap-2">
    <div class="min-w-0 overflow-hidden rounded-field border border-base-300 bg-base-100">
      <div
        class="flex items-center justify-between gap-2 border-b border-base-300 bg-base-200 py-1 ps-3 pe-1"
      >
        <span class="text-xs font-semibold text-base-content/80">{{ label }}</span>
        <button
          type="button"
          class="btn btn-ghost btn-sm shrink-0 gap-1.5 px-2.5 text-[0.8125rem] focus-visible:outline-base-content"
          :class="copied && 'text-success'"
          data-copy
          :title="prompt"
          :aria-describedby="caption ? `${promptId} ${hintId}` : promptId"
          @click="copy"
        >
          <svg
            v-if="copied"
            viewBox="0 0 24 24"
            class="size-3.5"
            fill="none"
            stroke="currentColor"
            stroke-width="2.6"
            stroke-linecap="round"
            stroke-linejoin="round"
            aria-hidden="true"
          >
            <path d="M20 6 9 17l-5-5" />
          </svg>
          <svg
            v-else
            viewBox="0 0 24 24"
            class="size-3.5"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            stroke-linecap="round"
            stroke-linejoin="round"
            aria-hidden="true"
          >
            <rect width="14" height="14" x="8" y="8" rx="2" ry="2" />
            <path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2" />
          </svg>
          {{ copied ? copiedLabel : copyLabel }}
        </button>
      </div>
      <p
        :id="promptId"
        :title="prompt"
        class="truncate px-3 py-2.5 font-mono text-xs whitespace-nowrap sm:text-[0.8125rem]"
      >
        {{ prompt }}
      </p>
    </div>
    <p v-if="caption" :id="hintId" class="min-h-5 text-[0.8125rem] text-base-content/80">
      {{ caption }}
    </p>
    <div v-if="manual" data-manual>
      <textarea
        ref="manualBox"
        readonly
        rows="3"
        class="textarea textarea-sm w-full border-primary font-mono text-base-content"
        :aria-label="copyLabel"
        :value="prompt"
      ></textarea>
      <p class="mt-1 text-sm font-semibold">{{ manualHint }}</p>
    </div>
    <span aria-live="polite" class="sr-only">{{ announcement }}</span>
  </div>
</template>
