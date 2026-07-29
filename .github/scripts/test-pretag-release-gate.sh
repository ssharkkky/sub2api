#!/usr/bin/env bash

set -euo pipefail

REPO_ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
CI_WORKFLOW="$REPO_ROOT/.github/workflows/backend-ci.yml"
PRETAG_WORKFLOW="$REPO_ROOT/.github/workflows/release-preflight.yml"
RELEASE_WORKFLOW="$REPO_ROOT/.github/workflows/release.yml"

fail() {
  echo "$*" >&2
  exit 1
}

require_literal() {
  local file="$1"
  local expected="$2"
  grep -Fq -- "$expected" "$file" || fail "$file is missing required release preflight contract: $expected"
}

DRY_RUN=$(make -n -C "$REPO_ROOT" test-frontend-release)
for expected in \
  'pnpm --dir frontend run lint:check' \
  'pnpm --dir frontend run test:run' \
  'pnpm --dir frontend run build'; do
  [[ "$DRY_RUN" == *"$expected"* ]] || fail "test-frontend-release is missing: $expected"
done

require_literal "$CI_WORKFLOW" 'bash .github/scripts/test-pretag-release-gate.sh'
require_literal "$PRETAG_WORKFLOW" 'name: Release Preflight'
require_literal "$PRETAG_WORKFLOW" 'backend/cmd/server/VERSION'
require_literal "$PRETAG_WORKFLOW" "'docs/releases/**'"
require_literal "$PRETAG_WORKFLOW" 'run: make test-frontend-release'
require_literal "$PRETAG_WORKFLOW" 'backend/scripts/check-release-migrations-history.sh'
require_literal "$PRETAG_WORKFLOW" 'go mod tidy'
require_literal "$PRETAG_WORKFLOW" 'package-sub2api-deployer-bundles.sh'
require_literal "$RELEASE_WORKFLOW" 'bash .github/scripts/test-pretag-release-gate.sh'
require_literal "$RELEASE_WORKFLOW" 'run: pnpm run test:run'
require_literal "$RELEASE_WORKFLOW" 'run: pnpm run build'

echo "Pre-tag release gate contract passed"
