import type { Meta, StoryObj } from "@storybook/vue3-vite";

const colors = [
  "base-100",
  "base-200",
  "base-300",
  "base-content",
  "primary",
  "secondary",
  "accent",
  "neutral",
  "info",
  "success",
  "warning",
  "error",
];
const scale = [
  { token: "display-1", className: "font-display text-display-1 font-extrabold" },
  { token: "display-2", className: "font-display text-display-2 font-extrabold" },
  { token: "h3", className: "text-h3 font-semibold" },
  { token: "lead", className: "text-lead" },
  { token: "body", className: "text-body" },
  { token: "eyebrow", className: "text-eyebrow font-semibold uppercase" },
  { token: "mono", className: "font-mono" },
];

const meta: Meta = { title: "Tokens" };
export default meta;

export const Tokens: StoryObj = {
  render: () => ({
    setup: () => ({ colors, scale }),
    template: `
      <div class="space-y-10 p-6">
        <div class="grid grid-cols-2 gap-4 md:grid-cols-4">
          <div v-for="c in colors" :key="c" class="rounded-box border border-base-300 p-3">
            <div class="h-12 rounded-field" :style="{ background: 'var(--color-' + c + ')' }"></div>
            <p class="mt-2 font-mono text-sm">{{ c }}</p>
          </div>
        </div>
        <div class="space-y-4">
          <p v-for="s in scale" :key="s.token" :class="s.className">{{ s.token }}: The quick brown fox</p>
        </div>
        <div class="flex gap-4">
          <a class="btn btn-accent btn-lg btn-brutal" href="#">Primary</a>
          <a class="btn btn-lg bg-base-100 btn-brutal" href="#">Secondary</a>
        </div>
        <div class="card bg-base-100 shadow-card p-6">shadow-card</div>
        <div class="card bg-base-100 shadow-pop p-6">shadow-pop</div>
      </div>`,
  }),
};
