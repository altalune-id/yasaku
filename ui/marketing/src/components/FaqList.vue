<script setup lang="ts">
defineProps<{ title: string; items: { q: string; a: string }[] }>();

defineSlots<Record<`item-${number}`, (() => unknown) | undefined>>();

function reduced(): boolean {
  return (
    document.documentElement.dataset.motion === "reduce" ||
    matchMedia("(prefers-reduced-motion: reduce)").matches
  );
}

const closing = new WeakMap<HTMLDetailsElement, Animation>();

function collapse(item: HTMLDetailsElement) {
  const answer = item.querySelector<HTMLElement>(".faq-answer");
  if (!answer || reduced()) {
    item.open = false;
    return;
  }
  answer.style.overflow = "hidden";
  const animation = answer.animate(
    [
      { height: `${answer.offsetHeight}px`, opacity: 1 },
      { height: "0px", opacity: 0, paddingBottom: "0px" },
    ],
    { duration: 300, easing: "ease" },
  );
  closing.set(item, animation);
  animation.finished
    .then(() => {
      item.open = false;
    })
    .catch(() => {})
    .finally(() => {
      answer.style.overflow = "";
      closing.delete(item);
    });
}

function toggle(event: MouseEvent) {
  const item = (event.currentTarget as HTMLElement).parentElement as HTMLDetailsElement;
  event.preventDefault();
  const running = closing.get(item);
  if (running) return running.cancel();
  if (item.open) return collapse(item);
  for (const other of item.parentElement!.querySelectorAll<HTMLDetailsElement>("details[open]"))
    collapse(other);
  item.open = true;
}
</script>

<template>
  <div class="mx-auto max-w-200">
    <h2 class="font-display text-display-2 font-extrabold">{{ title }}</h2>
    <div class="mt-6 border-t-[1.5px] border-base-300">
      <details
        v-for="(item, i) in items"
        :key="item.q"
        class="group border-b-[1.5px] border-base-300"
        :open="i === 0"
      >
        <summary
          @click="toggle"
          class="flex cursor-pointer list-none items-center justify-between gap-4 px-0.5 py-4.5 text-[1.0625rem] font-semibold focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-base-content [&::-webkit-details-marker]:hidden"
        >
          <span>{{ item.q }}</span>
          <span
            aria-hidden="true"
            class="grid size-7 shrink-0 place-items-center rounded-full border-[1.5px] border-base-300 transition-transform duration-300 group-open:rotate-45"
          >
            <svg
              viewBox="0 0 24 24"
              class="size-3.5"
              fill="none"
              stroke="currentColor"
              stroke-width="2.4"
              stroke-linecap="round"
            >
              <path d="M12 5v14M5 12h14" />
            </svg>
          </span>
        </summary>
        <div
          class="faq-answer pe-11 pb-5 ps-0.5 text-base leading-[1.6] text-base-content/80 [overflow-wrap:anywhere]"
        >
          <slot :name="`item-${i + 1}`">
            <p>{{ item.a }}</p>
          </slot>
        </div>
      </details>
    </div>
  </div>
</template>

<style scoped>
details[open] .faq-answer {
  animation: faq-in 300ms ease both;
}

@keyframes faq-in {
  from {
    opacity: 0;
    transform: translateY(10px);
  }
  to {
    opacity: 1;
    transform: none;
  }
}
</style>
