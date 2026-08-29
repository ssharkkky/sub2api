#!/usr/bin/env bash

# release-evidence-data.sh — fetch the Actions API evidence data package.
#
# Thin network glue used by backend-ci.yml ("Verify SHA Evidence") and
# promote-release.yml. All judgment stays in release-evidence.sh (pure).
#
# Usage:
#   release-evidence-data.sh --repo <owner/name> --parent-sha <sha> \
#      --out <dir> [--max-prs 8]
#
# Requires: GH_TOKEN, gh, jq, a full git checkout of the repository (so PR
# head refs can be fetched and tree IDs computed).

set -euo pipefail

fail() {
  echo "release-evidence-data: $*" >&2
  exit 1
}

: "${GH_TOKEN:?GH_TOKEN is required}"
export GH_TOKEN

repo=""
parent_sha=""
out=""
max_prs=8
while [[ $# -gt 0 ]]; do
  case "$1" in
    --repo) repo="${2:?}"; shift 2 ;;
    --parent-sha) parent_sha="${2:?}"; shift 2 ;;
    --out) out="${2:?}"; shift 2 ;;
    --max-prs) max_prs="${2:?}"; shift 2 ;;
    *) fail "unknown argument: $1" ;;
  esac
done
[[ "$repo" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]] || fail "--repo must be owner/name"
[[ "$parent_sha" =~ ^[0-9a-f]{40}$ ]] || fail "--parent-sha must be a 40-hex SHA"
[[ -n "$out" ]] || fail "--out is required"
[[ "$max_prs" =~ ^[0-9]+$ ]] || fail "--max-prs must be a non-negative integer"
mkdir -p "$out"

# 1) Parent's Backend CI push runs (completed, any conclusion, newest first).
gh api "repos/${repo}/actions/workflows/backend-ci.yml/runs" \
  -f branch=main \
  -f event=push \
  -f head_sha="$parent_sha" \
  -f status=completed \
  -f per_page=5 \
  > "$out/parent-runs.json"
jq -e . "$out/parent-runs.json" >/dev/null 2>&1 || fail "parent-runs.json is not valid JSON"

# 2) Job-level results for each parent run (terminal runs must be proven at
#    job level: skipped jobs do not count as executed evidence).
run_id=""
while IFS= read -r run_id; do
  [[ -n "$run_id" ]] || continue
  gh api "repos/${repo}/actions/runs/${run_id}/jobs" -f per_page=100 \
    > "$out/jobs-${run_id}.json"
  jq -e . "$out/jobs-${run_id}.json" >/dev/null 2>&1 || fail "jobs-${run_id}.json is not valid JSON"
done < <(jq -r '.workflow_runs[].id // empty' "$out/parent-runs.json")

# 3) Recently merged PRs (newest first). The L2 chain may only terminate at
#    one of these, so a bounded window is enough for the 7-day freshness cap.
gh api "repos/${repo}/pulls" \
  -f state=closed \
  -f sort=updated \
  -f direction=desc \
  -f per_page=50 \
  > "$out/prs.json"
jq -c '[.[] | select(.merged_at != null)][0:'"$max_prs"']' "$out/prs.json" \
  > "$out/pr-data.json"

# 4) For each merged PR: fetch the head ref, record its tree, and fetch the
#    PR-context Backend CI runs with job-level results.
: > "$out/pr-head-trees.jsonl"
line=""
while IFS= read -r line; do
  [[ -n "$line" ]] || continue
  number=$(jq -r '.number' <<<"$line")
  pr_head=$(jq -r '.head.sha' <<<"$line")
  [[ -n "$number" && "$pr_head" =~ ^[0-9a-f]{40}$ ]] || continue
  tree=""
  if git fetch --no-tags --quiet origin "refs/pull/${number}/head" 2>/dev/null; then
    tree=$(git rev-parse "FETCH_HEAD^{tree}" 2>/dev/null) || tree=""
  fi
  jq -cn --argjson number "$number" --arg sha "$pr_head" --arg tree "$tree" \
    '{number: $number, sha: $sha, tree: $tree}' >> "$out/pr-head-trees.jsonl"

  gh api "repos/${repo}/actions/workflows/backend-ci.yml/runs" \
    -f event=pull_request \
    -f head_sha="$pr_head" \
    -f status=completed \
    -f per_page=3 \
    > "$out/pr-runs-${pr_head}.json" 2>/dev/null || echo '{"workflow_runs":[]}' > "$out/pr-runs-${pr_head}.json"
  jq -e . "$out/pr-runs-${pr_head}.json" >/dev/null 2>&1 || echo '{"workflow_runs":[]}' > "$out/pr-runs-${pr_head}.json"

  pr_run_id=""
  while IFS= read -r pr_run_id; do
    [[ -n "$pr_run_id" ]] || continue
    gh api "repos/${repo}/actions/runs/${pr_run_id}/jobs" -f per_page=100 \
      > "$out/pr-jobs-${pr_run_id}.json"
    jq -e . "$out/pr-jobs-${pr_run_id}.json" >/dev/null 2>&1 || fail "pr-jobs-${pr_run_id}.json is not valid JSON"
  done < <(jq -r '.workflow_runs[].id // empty' "$out/pr-runs-${pr_head}.json")
done < "$out/pr-data.json"

echo "release-evidence-data: data package ready in $out"
