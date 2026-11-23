import { resolve } from "node:path";
import { fileURLToPath } from "node:url";
import tailwindcss from "@tailwindcss/vite";
import vue from "@vitejs/plugin-vue";
import { defineConfig, loadEnv } from "vite-plus";
import { writeArtifacts } from "./build/artifacts";
import { fetchStars } from "./src/shared/github";
import { isNoindex, site } from "./src/site";
import oxlintrc from "./.oxlintrc.json" with { type: "json" };
import type { OxlintConfig } from "vite-plus/lint";

const root = fileURLToPath(new URL(".", import.meta.url));

export default defineConfig(async ({ command, mode }) => {
  const stars = command === "build" ? await fetchStars(site.githubRepo) : null;
  const noindex = isNoindex(loadEnv(mode, root, "VITE_").VITE_SITE_NOINDEX);
  return {
    base: site.base,
    plugins: [vue(), tailwindcss()],
    define: {
      __SITE_NOINDEX__: JSON.stringify(noindex),
      __GITHUB_STARS__: JSON.stringify(stars),
      __BUILD_YEAR__: JSON.stringify(new Date().getUTCFullYear()),
    },
    ssgOptions: {
      dirStyle: "nested",
      script: "async",
      onFinished: () => writeArtifacts(resolve(root, "dist"), noindex),
    },
    test: {
      projects: [
        {
          extends: true,
          test: {
            name: "unit",
            environment: "happy-dom",
            include: ["src/**/*.test.ts", "build/**/*.test.ts"],
          },
        },
        {
          extends: true,
          test: {
            name: "build",
            environment: "node",
            include: ["tests/**/*.test.ts"],
          },
        },
      ],
    },
    lint: {
      extends: [oxlintrc as OxlintConfig],
    },
    fmt: {
      ignorePatterns: ["dist/**", "storybook-static/**", ".vite-ssg-temp/**", "pnpm-lock.yaml"],
    },
  };
});
