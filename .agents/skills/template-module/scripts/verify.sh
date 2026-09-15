#!/usr/bin/env bash
# Run the module gates in dependency order, explaining each failure.
#
#   scripts/verify.sh              unit gates only
#   scripts/verify.sh --integration  also run the Postgres suite
#
# TEST_PG_DSN should point at a throwaway database. Without it every pgtest.New
# starts its own container and the suite takes ~25 minutes instead of ~2.
set -uo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../../.." && pwd)"
cd "$root" || exit 1
fail=0

step() { printf '\n=== %s\n' "$1"; }
why()  { printf '    ↳ %s\n' "$1"; fail=1; }

step "generated code is current"
make generate >/dev/null 2>&1
make tenant-tables >/dev/null 2>&1
make config-examples >/dev/null 2>&1
if ! git diff --quiet -- schema/tenant_tables_gen.go .env.example config.example.yaml 2>/dev/null; then
  printf '    regenerated files changed — stage them\n'
  git diff --stat -- schema/tenant_tables_gen.go .env.example config.example.yaml
fi

step "make check (fmt + vet + unit tests)"
if ! make check >/tmp/template-module-check.log 2>&1; then
  tail -20 /tmp/template-module-check.log
  why "a failing unit test, or gofmt/vet. Full log: /tmp/template-module-check.log"
fi

step "make lint"
if ! make lint >/tmp/template-module-lint.log 2>&1; then
  grep -E "^(internal|schema|cmd)/" /tmp/template-module-lint.log | head -20
  why "depguard here usually means an aggregate file imported something outside the allow list"
fi

step "error codes documented both ways"
if ! go test ./internal/apperror/ >/dev/null 2>&1; then
  why "a Code* constant with no docs/ERROR_CODES.md row, or a row with no constant"
fi

step "tenant-scoped upserts guarded"
if ! go test ./schema/ -run TestStoreUpserts >/tmp/template-module-upsert.log 2>&1; then
  grep -o "no OrgID in its conflict clause" /tmp/template-module-upsert.log | head -1
  grep -oE "\.\./internal/[a-z/]+\.go:[0-9]+:[0-9]+" /tmp/template-module-upsert.log | head -5
  why "an ON_CONFLICT ... DO_UPDATE without a tenant predicate is a cross-tenant write"
fi

step "i18n keys present in every locale"
if ! go tool i18n-lint -check >/tmp/template-module-i18n.log 2>&1; then
  tail -10 /tmp/template-module-i18n.log
  why "add the key to all five locales by hand — do NOT run i18n-lint --fix, it rewrites them"
fi

step "routes registered are routes probed"
if ! go test ./internal/boot/ -run TestRoutes_ListCoversEveryRegisteredRoute >/dev/null 2>&1; then
  why "add the new route to probeRoutes(), and register it as a literal mux.HandleFunc call"
fi

if [ "${1:-}" = "--integration" ]; then
  step "integration suite"
  if [ -z "${TEST_PG_DSN:-}" ]; then
    printf '    TEST_PG_DSN unset — this will start a container per pgtest.New and take ~25 min\n'
  fi
  if ! make test-integration >/tmp/template-module-integration.log 2>&1; then
    grep -E "^(FAIL|--- FAIL)" /tmp/template-module-integration.log | head -10
    why "full log: /tmp/template-module-integration.log"
  fi
fi

printf '\n'
if [ "$fail" -eq 0 ]; then
  printf 'all gates passed.\n\nStill required, and not checkable here:\n'
  printf '  - open the page; type checking passing is not evidence a surface works\n'
  printf '  - for each security guard, revert it and confirm a test fails\n'
  exit 0
fi
printf 'gates failed — see above.\n'
exit 1
