#!/usr/bin/env bash
# mcp-ui-vendor.sh — download pinned MCP Apps assets.
# These are COMMITTED: internal/mcp/ui embeds them by NAME, so a fresh clone
# cannot `go build` without them. Do NOT add them to .gitignore by analogy with
# scripts/ui-vendor.sh.
#
# Pinned versions (bump here, re-run, update the //go:embed names and the sums):
#   @modelcontextprotocol/ext-apps  2.0.0
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ASSETS="$ROOT/internal/mcp/ui/assets"
SCHEMA_DIR="$ROOT/mcp/testdata"
mkdir -p "$ASSETS" "$SCHEMA_DIR"

EXT_APPS_VERSION="${EXT_APPS_VERSION:-2.0.0}"
BASE="https://cdn.jsdelivr.net/npm/@modelcontextprotocol/ext-apps@${EXT_APPS_VERSION}/dist/src"

BRIDGE_SHA256="fb56376b7583ecafb4820bdebc150abee18feb6258ff84b83c2c944ebd9c3602"

fetch() {
	local url="$1" out="$2"
	echo "  → $url"
	curl -sSfL "$url" -o "$out"
}

verify() {
	local file="$1" want="$2"
	local got
	got="$(shasum -a 256 "$file" | cut -d' ' -f1)"
	if [ "$got" != "$want" ]; then
		echo "FATAL: $file sha256 $got, expected $want" >&2
		echo "The CDN served different bytes. Re-verify the artefact before committing." >&2
		exit 1
	fi
	echo "  ok sha256 $got"
}

echo "==> ext-apps $EXT_APPS_VERSION"
fetch "$BASE/app-with-deps.js" "$ASSETS/ext-apps-${EXT_APPS_VERSION}.js"
verify "$ASSETS/ext-apps-${EXT_APPS_VERSION}.js" "$BRIDGE_SHA256"

# NOTE: the schema lands in mcp/testdata, not internal/, because mcp/ is copied
# verbatim into forks and platform-boundary-root forbids it importing internal/.
fetch "$BASE/generated/schema.json" "$SCHEMA_DIR/ext-apps-schema-${EXT_APPS_VERSION}.json"

echo "==> done; review the diff and run: go test ./mcp/..."
