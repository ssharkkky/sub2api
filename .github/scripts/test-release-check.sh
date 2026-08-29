#!/usr/bin/env bash

set -euo pipefail

# Contract tests for .github/scripts/release-check.sh (pure decision logic,
# no network). Portable across the ubuntu and macos CI runners (bash 3.2).

REPO_ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
CHECK="$REPO_ROOT/.github/scripts/release-check.sh"
TEST_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/sub2api-release-check.XXXXXX")

cleanup() {
  case "$TEST_ROOT" in
    *sub2api-release-check.*) find "$TEST_ROOT" -depth -delete ;;
    *) echo "Refusing to clean unexpected test path: $TEST_ROOT" >&2 ;;
  esac
}
trap cleanup EXIT

fail() {
  echo "$*" >&2
  exit 1
}

expect_failure() {
  local expected="$1"
  shift
  local output
  if output=$("$@" 2>&1); then
    fail "Command unexpectedly succeeded: $*"
  fi
  [[ "$output" == *"$expected"* ]] || \
    fail "Failure did not contain '$expected': $output"
}

FILES="$TEST_ROOT/files.txt"
TAGS="$TEST_ROOT/tags.txt"
BODY="$TEST_ROOT/body.md"

# ------------------------------------------------------------ containment

cat > "$FILES" <<'EOF'
backend/cmd/server/VERSION
docs/releases/v1.2.3-ts.2.md
EOF
bash "$CHECK" containment --files "$FILES" --version "1.2.3-ts.2" \
  | grep -q "containment ok" || fail "containment (both files) should pass"

echo "backend/cmd/server/VERSION" > "$FILES"
bash "$CHECK" containment --files "$FILES" --version "1.2.3-ts.2" \
  | grep -q "containment ok" || fail "containment (VERSION only) should pass"

cat > "$FILES" <<'EOF'
backend/cmd/server/VERSION
docs/releases/v1.2.3-ts.2.md
backend/internal/deployer/manager.go
EOF
expect_failure "outside VERSION and release notes" \
  bash "$CHECK" containment --files "$FILES" --version "1.2.3-ts.2"

: > "$FILES"
expect_failure "changed files file is empty" \
  bash "$CHECK" containment --files "$FILES" --version "1.2.3-ts.2"

# ------------------------------------------------------ version-increment

cat > "$TAGS" <<'EOF'
v1.2.2-ts.5
v1.2.3-ts.1
EOF
bash "$CHECK" version-increment --version "1.2.3-ts.2" --existing-tags "$TAGS" \
  | grep -q "version-increment ok" || fail "version-increment (ts.2 > ts.1) should pass"

expect_failure "does not exceed" \
  bash "$CHECK" version-increment --version "1.2.3-ts.1" --existing-tags "$TAGS"

cat > "$TAGS" <<'EOF'
v1.2.2-ts.5
EOF
bash "$CHECK" version-increment --version "1.2.3-ts.1" --existing-tags "$TAGS" \
  | grep -q "version-increment ok" || fail "version-increment (new base ts.1) should pass"

expect_failure "must be ts.1" \
  bash "$CHECK" version-increment --version "1.2.3-ts.2" --existing-tags "$TAGS"

cat > "$TAGS" <<'EOF'
v1.2.3
EOF
bash "$CHECK" version-increment --version "1.2.3-ts.1" --existing-tags "$TAGS" \
  | grep -q "version-increment ok" || fail "version-increment (plain upstream tag only) should pass"

# -------------------------------------------------------------- high-risk

cat > "$FILES" <<'EOF'
backend/cmd/server/VERSION
docs/releases/v1.2.3-ts.2.md
EOF
echo "plain release body without links" > "$BODY"
out=$(bash "$CHECK" high-risk --release-files "$FILES" --pr-body "$BODY")
echo "$out" | grep -q "audit not required" || fail "high-risk (no hit) should not require audit: $out"

cat > "$FILES" <<'EOF'
backend/internal/deployer/manager.go
docs/releases/v1.2.3-ts.2.md
EOF
expect_failure "no github.com/pull/ audit link" \
  bash "$CHECK" high-risk --release-files "$FILES" --pr-body "$BODY"

echo "Audit conclusion: https://github.com/ssharkkky/sub2api/pull/139" > "$BODY"
out=$(bash "$CHECK" high-risk --release-files "$FILES" --pr-body "$BODY")
echo "$out" | grep -q "high-risk hit" || fail "high-risk (deployer + link) should pass: $out"

cat > "$FILES" <<'EOF'
backend/internal/db/migrations/0001_init.sql
EOF
echo "body" > "$BODY"
expect_failure "no github.com/pull/ audit link" \
  bash "$CHECK" high-risk --release-files "$FILES" --pr-body "$BODY"

cat > "$FILES" <<'EOF'
backend/internal/ratelimit/policy.go
EOF
echo "See https://github.com/ssharkkky/sub2api/pull/138 for the audit" > "$BODY"
out=$(bash "$CHECK" high-risk --release-files "$FILES" --pr-body "$BODY")
echo "$out" | grep -q "high-risk hit" || fail "high-risk (ratelimit + link) should pass: $out"

echo "release-check contract tests passed"
