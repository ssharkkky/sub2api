#!/usr/bin/env bash

set -euo pipefail

# Contract tests for .github/scripts/release-evidence.sh (pure logic only:
# no network, no GitHub API). Run on ubuntu and macos CI runners, so the
# script must stay portable (bash 3.2 compatible).

REPO_ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
EVIDENCE="$REPO_ROOT/.github/scripts/release-evidence.sh"
TEST_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/sub2api-release-evidence.XXXXXX")

cleanup() {
  case "$TEST_ROOT" in
    *sub2api-release-evidence.*) find "$TEST_ROOT" -depth -delete ;;
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

# epoch_to_iso <epoch> — portable ISO8601 Zulu formatting (GNU/BSD date).
epoch_to_iso() {
  date -u -d "@$1" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null && return 0
  date -u -j -f '%s' "$1" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null && return 0
  return 1
}

NOW=$(date -u +%s)
RECENT_ISO=$(epoch_to_iso "$(( NOW - 3600 ))") || fail "cannot format recent ISO timestamp"
STALE_ISO=$(epoch_to_iso "$(( NOW - 2000000 ))") || fail "cannot format stale ISO timestamp"

PARENT_SHA="d3adb33f$(printf '0%.0s' $(seq 1 34))"
# 40-character lowercase hex identifiers (8 prefix + 32 zeros).
PARENT_SHA="d3adb33f$(printf '0%.0s' $(seq 1 32))"
TARGET_SHA="cafed00d$(printf '0%.0s' $(seq 1 32))"
PARENT_TREE="1$(printf '2%.0s' $(seq 1 39))"
PR_HEAD_SHA="beefcafe$(printf '0%.0s' $(seq 1 32))"

# ---------------------------------------------------------------- classify

REPO="$TEST_ROOT/repo"
mkdir -p "$REPO/backend/cmd/server" "$REPO/docs/releases"
cd "$REPO"
git init -q
git config user.name "evidence-test"
git config user.email "evidence-test@example.com"
git config commit.gpgsign false

write_release() {
  local v="$1"
  echo "$v" > backend/cmd/server/VERSION
  {
    echo "# TokenSupply v$v"
    echo
    echo "## Highlights"
    echo
    echo "- Fixture release notes for $v."
  } > "docs/releases/v$v.md"
}

write_release "0.1.183-ts.2"
git add backend docs
git commit -qm "base release"
BASE_SHA=$(git rev-parse HEAD)

write_release "0.1.183-ts.3"
git add backend docs
git commit -qm "release ts.3"
REL_SHA=$(git rev-parse HEAD)

echo "code change" > backend/code.go
git add backend
git commit -qm "code change"
CODE_SHA=$(git rev-parse HEAD)

echo "0.1.183-ts.4" > backend/cmd/server/VERSION
git add backend
git commit -qm "version bump without notes"
NOMD_SHA=$(git rev-parse HEAD)

# 1) VERSION + notes only => release-only
out=$(bash "$EVIDENCE" classify --parent "$BASE_SHA" --target "$REL_SHA")
[[ "$out" == "release-only" ]] || fail "classify expected release-only, got: $out"

# 2) code change => other
out=$(bash "$EVIDENCE" classify --parent "$REL_SHA" --target "$CODE_SHA")
[[ "$out" == "other" ]] || fail "classify expected other for code change, got: $out"

# 3) VERSION bump whose notes file is missing => other (fail-closed)
out=$(bash "$EVIDENCE" classify --parent "$CODE_SHA" --target "$NOMD_SHA")
[[ "$out" == "other" ]] || fail "classify expected other when notes are missing, got: $out"

# 4) identical parent and target (no diff) => other
out=$(bash "$EVIDENCE" classify --parent "$CODE_SHA" --target "$CODE_SHA")
[[ "$out" == "other" ]] || fail "classify expected other for an empty diff, got: $out"

# 5) unknown commit => hard error
expect_failure "parent commit not found" \
  bash "$EVIDENCE" classify --parent "$PARENT_SHA" --target "$CODE_SHA"

# ----------------------------------------------------------------- decide

DATA="$TEST_ROOT/data"
mkdir -p "$DATA"
: > "$DATA/pr-head-trees.jsonl"
echo '[]' > "$DATA/pr-data.json"

green_jobs() {
  cat > "$1" <<'EOF'
{"total_count":2,"jobs":[
  {"id":1,"name":"Backend Test","status":"completed","conclusion":"success"},
  {"id":2,"name":"Shell","status":"completed","conclusion":"success"}
]}
EOF
}

# Case A: parent has a fresh executed all-green push run => inherit (L1).
cat > "$DATA/parent-runs.json" <<EOF
{"workflow_runs":[{"id":111,"head_sha":"$PARENT_SHA","created_at":"$RECENT_ISO","conclusion":"success"}]}
EOF
green_jobs "$DATA/jobs-111.json"

out=$(bash "$EVIDENCE" decide \
  --data-dir "$DATA" \
  --target-sha "$TARGET_SHA" \
  --parent-sha "$PARENT_SHA" \
  --parent-tree "$PARENT_TREE" \
  --now-epoch "$NOW") || fail "decide (L1 fresh) should succeed: $out"
echo "$out" | jq -e --arg p "$PARENT_SHA" \
  '.evidence == "inherit" and .terminal.run_id == 111 and .terminal.sha == $p' >/dev/null \
  || fail "decide (L1 fresh) produced unexpected JSON: $out"
echo "$out" | jq -e '.chain | length == 2' >/dev/null || fail "decide (L1 fresh) chain must have exactly 2 steps: $out"

# Case B: same run but stale => no inheritance (exit 3).
cat > "$DATA/parent-runs.json" <<EOF
{"workflow_runs":[{"id":111,"head_sha":"$PARENT_SHA","created_at":"$STALE_ISO","conclusion":"success"}]}
EOF
status=0
out=$(bash "$EVIDENCE" decide \
  --data-dir "$DATA" \
  --target-sha "$TARGET_SHA" \
  --parent-sha "$PARENT_SHA" \
  --parent-tree "$PARENT_TREE" \
  --now-epoch "$NOW" 2>&1) || status=$?
[[ "$status" -eq 3 ]] || fail "decide (stale) should exit 3, got $status: $out"
echo "$out" | jq -e '.evidence == "none"' >/dev/null || fail "decide (stale) should report none: $out"

# Case C: parent run has the test job skipped => not executed evidence.
cat > "$DATA/parent-runs.json" <<EOF
{"workflow_runs":[{"id":222,"head_sha":"$PARENT_SHA","created_at":"$RECENT_ISO","conclusion":"success"}]}
EOF
cat > "$DATA/jobs-222.json" <<'EOF'
{"total_count":2,"jobs":[
  {"id":1,"name":"Backend Test","status":"completed","conclusion":"skipped"},
  {"id":2,"name":"Shell","status":"completed","conclusion":"success"}
]}
EOF
if bash "$EVIDENCE" decide \
  --data-dir "$DATA" \
  --target-sha "$TARGET_SHA" \
  --parent-sha "$PARENT_SHA" \
  --parent-tree "$PARENT_TREE" \
  --now-epoch "$NOW" > "$TEST_ROOT/case-c.json" 2>&1; then
  fail "decide (skipped test job) should not inherit"
fi
jq -e '.evidence == "none"' "$TEST_ROOT/case-c.json" >/dev/null \
  || fail "decide (skipped test job) should report none"

# Case D: L2 — parent run absent, but a merged PR head tree equals the parent
# tree and that PR has a fresh executed all-green PR-context run.
echo '{"workflow_runs":[]}' > "$DATA/parent-runs.json"
echo "{\"number\": 42, \"sha\": \"$PR_HEAD_SHA\", \"tree\": \"$PARENT_TREE\"}" > "$DATA/pr-head-trees.jsonl"
cat > "$DATA/pr-runs-${PR_HEAD_SHA}.json" <<EOF
{"workflow_runs":[{"id":333,"head_sha":"$PR_HEAD_SHA","created_at":"$RECENT_ISO","conclusion":"success"}]}
EOF
green_jobs "$DATA/pr-jobs-333.json"

out=$(bash "$EVIDENCE" decide \
  --data-dir "$DATA" \
  --target-sha "$TARGET_SHA" \
  --parent-sha "$PARENT_SHA" \
  --parent-tree "$PARENT_TREE" \
  --now-epoch "$NOW") || fail "decide (L2) should succeed: $out"
echo "$out" | jq -e --arg h "$PR_HEAD_SHA" \
  '.evidence == "inherit" and .terminal.run_id == 333 and .terminal.sha == $h' >/dev/null \
  || fail "decide (L2) produced unexpected JSON: $out"
echo "$out" | jq -e '.chain[1].step == "merged-pr-tree-equality" and .chain[1].pull_request == 42' >/dev/null \
  || fail "decide (L2) chain must record the merged PR step: $out"

# Case E: L2 PR run is stale => no inheritance.
cat > "$DATA/pr-runs-${PR_HEAD_SHA}.json" <<EOF
{"workflow_runs":[{"id":333,"head_sha":"$PR_HEAD_SHA","created_at":"$STALE_ISO","conclusion":"success"}]}
EOF
if bash "$EVIDENCE" decide \
  --data-dir "$DATA" \
  --target-sha "$TARGET_SHA" \
  --parent-sha "$PARENT_SHA" \
  --parent-tree "$PARENT_TREE" \
  --now-epoch "$NOW" > "$TEST_ROOT/case-e.json" 2>&1; then
  fail "decide (stale L2) should not inherit"
fi
jq -e '.evidence == "none"' "$TEST_ROOT/case-e.json" >/dev/null \
  || fail "decide (stale L2) should report none"

# Case F: malformed parent-runs.json => hard error (exit 1), fail-closed.
echo 'not json' > "$DATA/parent-runs.json"
expect_failure "malformed parent-runs.json" \
  bash "$EVIDENCE" decide \
    --data-dir "$DATA" \
    --target-sha "$TARGET_SHA" \
    --parent-sha "$PARENT_SHA" \
    --parent-tree "$PARENT_TREE" \
    --now-epoch "$NOW"

# Case G: missing data file => hard error (exit 1), fail-closed.
cat > "$DATA/parent-runs.json" <<EOF
{"workflow_runs":[]}
EOF
if out=$(bash "$EVIDENCE" decide \
  --data-dir "$TEST_ROOT/does-not-exist" \
  --target-sha "$TARGET_SHA" \
  --parent-sha "$PARENT_SHA" \
  --parent-tree "$PARENT_TREE" \
  --now-epoch "$NOW" 2>&1); then
  fail "decide (missing data dir) should fail: $out"
fi
[[ "$out" == *"data dir not found"* ]] || fail "decide (missing data dir) wrong error: $out"

echo "release-evidence contract tests passed"
