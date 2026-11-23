import { ViteSSG } from "vite-ssg";
import App from "./App.vue";
import { routes } from "./routes";

export const createApp = ViteSSG(App, { routes, base: import.meta.env.BASE_URL }, undefined, {
  hydration: true,
});
