// @vitest-environment node
import { createHead } from "@unhead/vue/server";
import { createSSRApp, h } from "vue";
import { renderToString } from "vue/server-renderer";
import { describe, expect, it } from "vite-plus/test";
import { copy } from "../content";
import HomePage from "./HomePage.vue";

describe("HomePage renders on the server", () => {
  it.each(["en", "id"] as const)("%s", async (locale) => {
    const app = createSSRApp({ render: () => h(HomePage, { locale }) });
    app.use(createHead());
    const html = await renderToString(app);
    expect(html).toContain(copy[locale].hero.title);
    expect(html).not.toContain('data-inview="false"');
  });
});
