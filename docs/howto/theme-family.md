# Add a theme family

Use this when the console needs another color family. A family is a pair of native daisyUI theme
blocks in `internal/web/ui/themes.css`. The registry, the picker, the allowlist and the compiled
CSS all read that file, so nothing else is wired by hand.

## Steps

1. **Pick a hue** (OKLCH degrees). Its primary must sit at least 0.07 OKLab away from `info`,
   `success`, `warning`, `error` and `accent`, in both modes. Status colors never move to fit a
   family: drop the hue instead.
2. **Copy the default family's two blocks** (the pair on bare `:root`) to the end of the file.
3. **Rename** them `<family>-light` and `<family>-dark`, and set both `root:` values to
   `":root[data-family='<family>']"`. Keep `default: true` on light, `prefersdark: true` on dark,
   and `themecontroller: false`.
4. **Re-hue the listed tokens** below: set each one's hue to the family hue. Listed tokens keep the
   default family's lightness and chroma, unless that chroma falls outside sRGB at that lightness
   and hue; then use the largest in-gamut chroma, rounded down to 3 decimals.
5. **Leave every other token unchanged**: accent, the four semantic pairs, radii, sizes, `--border`,
   `--depth`, `--noise`, `--brand`.
6. **Add 1 key in 5 locales**: `theme.<family>`, with a real translation in each file under
   `internal/i18n/locales/`.
7. **Run `make ui-vendor`**, then `go test ./internal/web/...`.

To hide a family without losing it, wrap both blocks in one `/* TODO: <reason> */` comment.

## Listed tokens

Listed tokens keep the default family's lightness and chroma; only the hue changes:
`base-100`, `base-200`, `base-300`, `base-content`, `primary`, `primary-content`, `secondary`,
`secondary-content`, `neutral`, `neutral-content`.

## What fails if you skip a step

| Test                                        | Catches                                           |
| ------------------------------------------- | ------------------------------------------------- |
| `TestThemes_FamiliesFollowFormula`          | a token off the formula, or a fixed token changed |
| `TestThemes_PrimaryClearOfSemanticColors`   | a primary too close to a status color             |
| `TestThemes_ContrastAA`                     | a content pair under 4.5:1                        |
| `TestThemes_RootScopesEachFamily`           | a wrong `root:` or flag                           |
| `TestThemes_RegistryEqualsCompiledCSS`      | `app.css` not rebuilt                             |
| `TestThemeFamilies_TranslatedInEveryLocale` | a missing `theme.<family>` translation            |
