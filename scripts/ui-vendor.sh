#!/usr/bin/env bash
# Vendors the pinned static assets into internal/web/static/, the build-only daisyUI
# plugins into internal/web/ui/, and compiles app.css.
# The fetched assets are COMMITTED; their sha256 is pinned here AND in
# internal/web/vendor_test.go — bump both together.
# Flags: --css-only (recompile app.css alone), --force (re-download cached assets),
#        --verify (offline: check on-disk assets against the pins, fetch nothing).
# Pinned versions:
#   htmx        v4.0.0  (Sep 2026)
#   easymde     v2.21.0 (MIT; CodeMirror bundled)
#   echarts     v6.1.0  (yasaku charts)
#   cally       v0.9.2  (MIT; date picker web component, daisyUI calendar)
#   daisyui     v5.8.0  (daisyui.mjs + daisyui-theme.mjs, build-only, not embedded)
#   tailwind    v4 CLI standalone binary (build tool, not embedded, not committed)
set -euo pipefail

CSS_ONLY=0
FORCE=0
VERIFY_ONLY=0
for arg in "$@"; do
	case "$arg" in
		--css-only) CSS_ONLY=1 ;;
		--force) FORCE=1 ;;
		--verify) VERIFY_ONLY=1 ;;
		*) echo "unknown argument: $arg" >&2; exit 2 ;;
	esac
done

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
STATIC="$ROOT/internal/web/static"
UI="$ROOT/internal/web/ui"
mkdir -p "$STATIC" "$UI"

HTMX_VERSION="${HTMX_VERSION:-4.0.0}"
EASYMDE_VERSION="${EASYMDE_VERSION:-2.21.0}"
ECHARTS_VERSION="${ECHARTS_VERSION:-6.1.0}"
CALLY_VERSION="${CALLY_VERSION:-0.9.2}"
DAISYUI_VERSION="${DAISYUI_VERSION:-5.8.0}"
TAILWIND_VERSION="${TAILWIND_VERSION:-v4.3.5}"
FIGTREE_VERSION="${FIGTREE_VERSION:-5.3.0}"
PLEX_MONO_VERSION="${PLEX_MONO_VERSION:-5.3.0}"

VENDORED=(
	"htmx.min.js	e484d9171a9db30a39c8f16e3d709d4137f3211c659f8e6125816635033d593f	https://unpkg.com/htmx.org@${HTMX_VERSION}/dist/htmx.min.js"
	"hx-csp.min.js	279f659b9ec8658e3d47230cc545b0bb4e3efa4c2d780454d54db104a5257b7d	https://unpkg.com/htmx.org@${HTMX_VERSION}/dist/ext/hx-csp.min.js"
	"easymde.min.js	2c06bddfd0c89176db08ccf9d42e2beaa9b4f1a4ff8fb2ec0bf1bed25ce08e05	https://cdn.jsdelivr.net/npm/easymde@${EASYMDE_VERSION}/dist/easymde.min.js"
	"easymde.min.css	6eee36340432776d682e7372ec4a7eb29be4fdb3ffada32c0bfa5e31a5ef34a2	https://cdn.jsdelivr.net/npm/easymde@${EASYMDE_VERSION}/dist/easymde.min.css"
	"echarts.min.js	b66b25aeb4df84e33199dc21694014d336d222cbd9deb0e5a7c14bd6aa0d0fd0	https://cdn.jsdelivr.net/npm/echarts@${ECHARTS_VERSION}/dist/echarts.min.js"
	"cally.js	4dcba6a3b8ec63b66ff2fbfc6cf47b7dee1eb4896eac89fe7487d82e28e15870	https://cdn.jsdelivr.net/npm/cally@${CALLY_VERSION}/dist/cally.js"
	"figtree-latin-wght-normal.woff2	4ba7d3d096695818fe0686be4f1e82c6b05134e18a22260336130335027462dd	https://cdn.jsdelivr.net/npm/@fontsource-variable/figtree@${FIGTREE_VERSION}/files/figtree-latin-wght-normal.woff2"
	"figtree-latin-ext-wght-normal.woff2	bf7828e2c258cffcfa50a048ee388a36b95bc16b452e8d36fa797635dbe15965	https://cdn.jsdelivr.net/npm/@fontsource-variable/figtree@${FIGTREE_VERSION}/files/figtree-latin-ext-wght-normal.woff2"
	"ibm-plex-mono-latin-400-normal.woff2	08949f728dc52d528e69b1667d15c89a5686a4ee9a296ff90983985f99c380f7	https://cdn.jsdelivr.net/npm/@fontsource/ibm-plex-mono@${PLEX_MONO_VERSION}/files/ibm-plex-mono-latin-400-normal.woff2"
	"ibm-plex-mono-latin-500-normal.woff2	01d285447409c8a588692162439a038b8cbd7871309ee20267b0d2d91c6e8e22	https://cdn.jsdelivr.net/npm/@fontsource/ibm-plex-mono@${PLEX_MONO_VERSION}/files/ibm-plex-mono-latin-500-normal.woff2"
)

PLUGINS=(
	"daisyui.mjs	15b69906afa8334e0e04e7da072394999e9c9d21b23451576f25417a95dcdc14	https://github.com/saadeghi/daisyui/releases/download/v${DAISYUI_VERSION}/daisyui.mjs"
	"daisyui-theme.mjs	b643f5d69bb4db8ba9332c83a8db6a9e65ed49ca8bd8a4d74391b26027708feb	https://github.com/saadeghi/daisyui/releases/download/v${DAISYUI_VERSION}/daisyui-theme.mjs"
)

TW_BIN="$ROOT/bin/tailwindcss"

sha256_of() {
	if command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | cut -d' ' -f1
		return
	fi
	sha256sum "$1" | cut -d' ' -f1
}

ensure() {
	local url="$1" out="$2" want="$3" got tmp
	if [ "$FORCE" -eq 0 ] && [ -f "$out" ]; then
		got="$(sha256_of "$out")"
		if [ "$got" = "$want" ]; then
			echo "  ok $(basename "$out") cached sha256 $got"
			return
		fi
	fi
	echo "  → $url"
	tmp="$(mktemp "${out}.XXXXXX")"
	if ! curl -sSfL "$url" -o "$tmp"; then
		rm -f "$tmp"
		echo "FATAL: $(basename "$out") download failed from $url" >&2
		exit 1
	fi
	got="$(sha256_of "$tmp")"
	if [ "$got" != "$want" ]; then
		rm -f "$tmp"
		echo "FATAL: $(basename "$out") sha256 $got, expected $want" >&2
		echo "The source served different bytes. Re-verify the artifact before committing." >&2
		exit 1
	fi
	mv "$tmp" "$out"
	echo "  ok $(basename "$out") sha256 $got"
}

verify_dir() {
	local dir="$1" entry name want got rc=0
	shift
	for entry in "$@"; do
		IFS=$'\t' read -r name want _ <<<"$entry"
		if [ ! -f "$dir/$name" ]; then
			echo "FATAL: $dir/$name is missing" >&2
			rc=1
			continue
		fi
		got="$(sha256_of "$dir/$name")"
		if [ "$got" != "$want" ]; then
			echo "FATAL: $dir/$name sha256 $got, expected $want" >&2
			rc=1
			continue
		fi
		echo "  ok $name sha256 $got"
	done
	return "$rc"
}

fetch_dir() {
	local dir="$1" entry name want url
	shift
	for entry in "$@"; do
		IFS=$'\t' read -r name want url <<<"$entry"
		echo "==> $name"
		ensure "$url" "$dir/$name" "$want"
	done
}

tailwind_platform() {
	local os arch
	os="$(uname -s | tr '[:upper:]' '[:lower:]')"
	arch="$(uname -m)"
	case "$arch" in
		arm64|aarch64) arch="arm64" ;;
		x86_64|amd64) arch="x64" ;;
	esac
	case "$os" in
		darwin) os="macos" ;;
		linux) os="linux" ;;
		*) echo "unsupported OS: $os" >&2; return 1 ;;
	esac
	printf "%s-%s" "$os" "$arch"
}

# https://github.com/tailwindlabs/tailwindcss/releases/download/v4.3.5/sha256sums.txt
tailwind_sha256() {
	case "$1" in
		macos-arm64) echo "f3f4f5d1727e2b693b8f1c9a1492b5dd87008c86ea286bee1589cb458e6235fc" ;;
		macos-x64)   echo "a63702686a3ed048b3135d84a73be49ea9d47f880c9b20e54b3ef60934ad8cdf" ;;
		linux-arm64) echo "ffcd0d21dbd7c605dc12743ff0c0b47debc963c86b399674084c1c8f2c96ecad" ;;
		linux-x64)   echo "e47695cee3f0d90623a91f3939b08bdee610eb5ad99340adff9d4145b284d6cc" ;;
		*) return 1 ;;
	esac
}

fetch_vendor() {
	local platform tw_want
	fetch_dir "$STATIC" "${VENDORED[@]}"
	fetch_dir "$UI" "${PLUGINS[@]}"

	platform="$(tailwind_platform)"
	if ! tw_want="$(tailwind_sha256 "$platform")"; then
		echo "FATAL: no pinned tailwindcss $TAILWIND_VERSION digest for $platform" >&2
		echo "Add it from the release's sha256sums.txt before building." >&2
		exit 1
	fi
	echo "==> tailwindcss CLI $TAILWIND_VERSION ($platform)"
	mkdir -p "$ROOT/bin"
	ensure "https://github.com/tailwindlabs/tailwindcss/releases/download/${TAILWIND_VERSION}/tailwindcss-${platform}" "$TW_BIN" "$tw_want"
	chmod +x "$TW_BIN"
}

if [ "$VERIFY_ONLY" -eq 1 ]; then
	echo "==> verifying committed assets in $STATIC and $UI"
	rc=0
	verify_dir "$STATIC" "${VENDORED[@]}" || rc=1
	verify_dir "$UI" "${PLUGINS[@]}" || rc=1
	[ "$rc" -eq 0 ] || exit 1
	echo "==> ok — every committed asset matches its pin"
	exit 0
fi

if [ "$CSS_ONLY" -eq 0 ] || [ ! -x "$TW_BIN" ] || [ ! -f "$UI/daisyui.mjs" ] || [ ! -f "$UI/daisyui-theme.mjs" ] ||
	! "$TW_BIN" --help 2>&1 | grep -qF "$TAILWIND_VERSION"; then
	fetch_vendor
fi

"$TW_BIN" -i "$UI/app.tailwind.css" -o "$STATIC/app.css"

echo "==> done — vendored assets in $STATIC"
