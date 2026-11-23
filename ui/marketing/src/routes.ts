import type { RouteRecordRaw } from "vue-router";
import type { Locale } from "./site";
import HomePage from "./pages/HomePage.vue";
import { site } from "./site";

declare module "vue-router" {
  interface RouteMeta {
    locale: Locale;
  }
}

export const routes: RouteRecordRaw[] = site.pages.map((page) => ({
  path: page.path === "/" ? "/" : page.path.replace(/\/$/, ""),
  component: HomePage,
  meta: { locale: page.locale },
  props: (route) => ({ locale: route.meta.locale }),
}));
