# yasaku marketing site

A prerendered, bilingual marketing site for yasaku: English at `https://altalune.id/yasaku/`, Indonesian at
`https://altalune.id/yasaku/id/`. Vue 3 on Vite+, prerendered by vite-ssg, served by Cloudflare Workers static assets.

## Commands

Run from this folder. `pnpm run <name>` works the same when the global `vp` is not installed.

| Command                  | Does                                                  |
| ------------------------ | ----------------------------------------------------- |
| `pnpm install`           | installs this package only (it has its own lockfile)  |
| `vp run dev`             | dev server                                            |
| `vp run build`           | prerenders `dist/index.html` and `dist/id/index.html` |
| `vp run preview`         | serves `dist/` at `http://localhost:4173/yasaku/`     |
| `vp run lint`            | format check, lint and `vue-tsc`                      |
| `vp run test`            | unit tests                                            |
| `vp run test:build`      | build, then check the files in `dist/`                |
| `vp run storybook`       | Storybook at `http://localhost:6006`                  |
| `vp run build-storybook` | static Storybook in `storybook-static/`               |
| `vp exec wrangler dev`   | serves `dist/` through the Worker locally, no deploy  |

Shared, app-agnostic code lives in `src/shared/`. It holds no app name, domain or currency.

## Noindex switch

The site ships with `noindex` on: a `robots` meta tag in both pages, and `X-Robots-Tag: noindex` for `/yasaku/*`
in `dist/_headers`.

- `VITE_SITE_NOINDEX` unset, or any value except `false`: noindex on.
- `VITE_SITE_NOINDEX=false vp run build`: both signals are gone.

Precondition for turning it off: `https://altalune.id/robots.txt` returns 200 or 404. Any other status, such as a
5xx, makes Google treat the whole host as disallowed.

## Sitemap

The build writes `dist/sitemap.xml` with both pages and their hreflang alternates. There is no `robots.txt` under
`/yasaku/`: crawlers read it only at the host root.

1. At launch, submit `https://altalune.id/yasaku/sitemap.xml` in Search Console.
2. When the hub ships, add `Sitemap: https://altalune.id/yasaku/sitemap.xml` to its `robots.txt`.

## Deploy (owner only, not yet)

Prerequisite: a proxied DNS record for the apex `altalune.id`. Worker routes run only on proxied traffic.

1. `vp run build`
2. `vp exec wrangler deploy`
