import { type Component, createSSRApp, h } from "vue";
import { renderToString } from "vue/server-renderer";
import { vi } from "vite-plus/test";

export async function hydrationWarnings(
  component: Component,
  props: Record<string, unknown>,
): Promise<string[]> {
  const html = await renderToString(createSSRApp({ render: () => h(component, props) }));
  const el = document.createElement("div");
  el.innerHTML = html;
  document.body.append(el);
  const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
  const error = vi.spyOn(console, "error").mockImplementation(() => {});
  const app = createSSRApp({ render: () => h(component, props) });
  app.mount(el);
  const messages = [...warn.mock.calls, ...error.mock.calls]
    .flat()
    .map(String)
    .filter((m) => m.includes("Hydration"));
  app.unmount();
  el.remove();
  warn.mockRestore();
  error.mockRestore();
  return messages;
}
