#!/usr/bin/env bash

# release-evidence.sh — machine-verifiable test evidence decision.
#
# Implements the SOP sec 2 "test evidence inheritance" rules used by:
#   - backend-ci.yml job "Verify SHA Evidence" (push context, main only)
#   - promote-release.yml job-level assertion (independent re-verification)
#
# The script is pure: no network access. Callers fetch the Actions API data
# (see release-evidence-data.sh) and hand it to the `decide` subcommand.
#
# Subcommands:
#   classify --parent <sha> --target <sha>
#       Pure-git decision. Prints "release-only" when the target's diff
#       against its first parent touches only backend/cmd/server/VERSION and
#       docs/releases/v<target VERSION>.md (notes must exist at target),
#       otherwise prints "other". Any ambiguity prints "other" (fail-closed).
#
#   decide --data-dir <dir> --target-sha <sha> --parent-sha <sha> \
#          --parent-tree <tree-sha> [--max-age-seconds <n>] [--now-epoch <n>]
#       Pure JSON decision over the fetched data. The chain must terminate at
#       an executed, job-level all-green Backend CI run within max age:
#         L1: the parent has an executed all-green push run.
#         L2: the parent's tree equals the head tree of a recently merged PR
#             that has an executed all-green PR-context run (tree equality).
#       Prints one JSON line. Exit codes: 0 = inherit, 3 = none, 1 = error.

set -euo pipefail

fail() {
  echo "release-evidence: $*" >&2
  exit 1
}

# iso_to_epoch <iso8601-zulu> — portable across GNU/BSD date (CI runs on
# ubuntu and macos runners). Returns non-zero on failure; callers fail closed.
iso_to_epoch() {
  local ts="$1"
  date -u -d "$ts" +%s 2>/dev/null && return 0
  date -u -j -f '%Y-%m-%dT%H:%M:%SZ' "$ts" +%s 2>/dev/null && return 0
  return 1
}

cmd_classify() {
  local parent="" target=""
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --parent) parent="${2:?}"; shift 2 ;;
      --target) target="${2:?}"; shift 2 ;;
      *) fail "classify: unknown argument: $1" ;;
    esac
  done
  [[ "$parent" =~ ^[0-9a-f]{40}$ ]] || fail "classify: parent must be a 40-hex SHA"
  [[ "$target" =~ ^[0-9a-f]{40}$ ]] || fail "classify: target must be a 40-hex SHA"
  git rev-parse --verify --quiet "${parent}^{commit}" >/dev/null || fail "classify: parent commit not found: $parent"
  git rev-parse --verify --quiet "${target}^{commit}" >/dev/null || fail "classify: target commit not found: $target"

  local version
  version=$(git show "${target}:backend/cmd/server/VERSION" 2>/dev/null | tr -d '\r\n') || {
    echo "classify: target has no VERSION file; treating as non-release diff" >&2
    echo "other"
    return 0
  }
  if [[ ! "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-ts\.[1-9][0-9]*)?$ ]]; then
    echo "classify: target VERSION is not a release candidate: $version" >&2
    echo "other"
    return 0
  fi
  local notes="docs/releases/v${version}.md"
  if ! git cat-file -e "${target}:${notes}" 2>/dev/null; then
    echo "classify: release notes missing at target: $notes" >&2
    echo "other"
    return 0
  fi

  local changed
  changed=$(git diff --name-only "$parent" "$target")
  if [[ -z "$changed" ]]; then
    echo "classify: no diff between parent and target; nothing to inherit" >&2
    echo "other"
    return 0
  fi
  local f
  while IFS= read -r f; do
    if [[ "$f" != "backend/cmd/server/VERSION" && "$f" != "$notes" ]]; then
      echo "other"
      return 0
    fi
  done <<<"$changed"
  echo "release-only"
}

# run_jobs_all_green <jobs-file> <test-job-name>
# Terminal runs must be executed: every job completed with conclusion success
# (conditionally skipped jobs, e.g. an already-signed CLA, are allowed), and
# the heavy test job must exist and have run to success (a skipped test job
# means the suite did not actually execute).
run_jobs_all_green() {
  local jobs_file="$1" test_job_name="$2"
  [[ -f "$jobs_file" ]] || return 1
  jq -e . "$jobs_file" >/dev/null 2>&1 || return 1
  local bad
  bad=$(jq -r '[.jobs[]? | select(.conclusion != "success" and .conclusion != "skipped")] | length' "$jobs_file")
  [[ "$bad" == "0" ]] || return 1
  local test_jobs
  test_jobs=$(jq -r --arg name "$test_job_name" \
    '[.jobs[]? | select(.name == $name and .status == "completed" and .conclusion == "success")] | length' "$jobs_file")
  [[ "$test_jobs" == "1" ]] || return 1
  return 0
}

# emit_inherit <terminal-json> <chain-step-json>
emit_inherit() {
  local terminal="$1" step="$2"
  jq -cn \
    --arg evidence "inherit" \
    --arg target "$DECIDE_TARGET" \
    --arg parent "$DECIDE_PARENT" \
    --argjson terminal "$terminal" \
    --argjson step "$step" \
    '{evidence: $evidence,
      target_sha: $target,
      parent_sha: $parent,
      chain: [
        {step: "diff", from: $target, to: $parent, detail: "VERSION and release notes only"},
        $step
      ],
      terminal: $terminal}'
}

emit_none() {
  local reason="$1"
  jq -cn --arg evidence "none" --arg target "$DECIDE_TARGET" --arg parent "$DECIDE_PARENT" --arg reason "$reason" \
    '{evidence: $evidence, target_sha: $target, parent_sha: $parent, reason: $reason}'
}

cmd_decide() {
  local data_dir="" target_sha="" parent_sha="" parent_tree=""
  local max_age=604800 now_epoch=""
  local test_job_name="test"
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --data-dir) data_dir="${2:?}"; shift 2 ;;
      --target-sha) target_sha="${2:?}"; shift 2 ;;
      --parent-sha) parent_sha="${2:?}"; shift 2 ;;
      --parent-tree) parent_tree="${2:?}"; shift 2 ;;
      --max-age-seconds) max_age="${2:?}"; shift 2 ;;
      --now-epoch) now_epoch="${2:?}"; shift 2 ;;
      --test-job-name) test_job_name="${2:?}"; shift 2 ;;
      *) fail "decide: unknown argument: $1" ;;
    esac
  done
  DECIDE_TARGET="$target_sha"
  DECIDE_PARENT="$parent_sha"
  [[ "$target_sha" =~ ^[0-9a-f]{40}$ ]] || fail "decide: target-sha must be a 40-hex SHA"
  [[ "$parent_sha" =~ ^[0-9a-f]{40}$ ]] || fail "decide: parent-sha must be a 40-hex SHA"
  [[ "$parent_tree" =~ ^[0-9a-f]{4,64}$ ]] || fail "decide: parent-tree must be a hex SHA"
  [[ -d "$data_dir" ]] || fail "decide: data dir not found: $data_dir"
  if [[ -z "$now_epoch" ]]; then
    now_epoch=$(date -u +%s) || fail "decide: cannot determine current time"
  fi
  [[ "$max_age" =~ ^[1-9][0-9]*$ ]] || fail "decide: max-age-seconds must be a positive integer"

  local f
  for f in parent-runs.json pr-data.json pr-head-trees.jsonl; do
    [[ -f "$data_dir/$f" ]] || fail "decide: missing data file: $f"
  done
  jq -e . "$data_dir/parent-runs.json" >/dev/null 2>&1 || fail "decide: malformed parent-runs.json"
  jq -e . "$data_dir/pr-data.json" >/dev/null 2>&1 || fail "decide: malformed pr-data.json"

  # L1: the parent has an executed, job-level all-green Backend CI push run
  # (newest first) whose evidence is fresh enough.
  local run_id jobs_file created_at created_epoch age
  while IFS= read -r run_id; do
    [[ -n "$run_id" ]] || continue
    jobs_file="$data_dir/jobs-${run_id}.json"
    [[ -f "$jobs_file" ]] || continue
    created_at=$(jq -r --arg id "$run_id" \
      '.workflow_runs[]? | select((.id | tostring) == $id) | .created_at // empty' \
      "$data_dir/parent-runs.json" | head -n 1)
    if [[ -z "$created_at" ]] || ! run_jobs_all_green "$jobs_file" "$test_job_name"; then
      continue
    fi
    created_epoch=$(iso_to_epoch "$created_at") || continue
    age=$(( now_epoch - created_epoch ))
    if [[ $age -ge 0 && $age -le $max_age ]]; then
      emit_inherit \
        "$(jq -cn --argjson id "$run_id" --arg sha "$parent_sha" --arg at "$created_at" --argjson age "$age" \
          '{run_id: $id, sha: $sha, created_at: $at, age_seconds: $age}')" \
        "$(jq -cn --argjson id "$run_id" --arg sha "$parent_sha" \
          '{step: "executed-run", run_id: $id, sha: $sha, detail: "executed all-green Backend CI push run"}')"
      return 0
    fi
  done < <(jq -r '[.workflow_runs[]? | select(.conclusion == "success")] | sort_by(.created_at // "") | reverse | .[].id // empty' \
    "$data_dir/parent-runs.json")

  # L2: the parent's tree equals the head tree of a recently merged PR whose
  # PR-context Backend CI run executed and was job-level all-green (fresh).
  # pr-head-trees.jsonl lines: {number, sha, tree}; tree may be empty when the
  # PR head could not be fetched (those lines are skipped).
  local line pr_number pr_head pr_tree pr_runs_file
  while IFS= read -r line; do
    [[ -n "$line" ]] || continue
    pr_number=$(jq -r '.number // empty' <<<"$line" 2>/dev/null) || continue
    pr_head=$(jq -r '.sha // empty' <<<"$line" 2>/dev/null) || continue
    pr_tree=$(jq -r '.tree // empty' <<<"$line" 2>/dev/null) || continue
    [[ -n "$pr_head" && "$pr_tree" == "$parent_tree" ]] || continue
    pr_runs_file="$data_dir/pr-runs-${pr_head}.json"
    [[ -f "$pr_runs_file" ]] || continue
    jq -e . "$pr_runs_file" >/dev/null 2>&1 || continue
    while IFS= read -r run_id; do
      [[ -n "$run_id" ]] || continue
      jobs_file="$data_dir/pr-jobs-${run_id}.json"
      [[ -f "$jobs_file" ]] || continue
      created_at=$(jq -r --arg id "$run_id" \
        '.workflow_runs[]? | select((.id | tostring) == $id) | .created_at // empty' \
        "$pr_runs_file" | head -n 1)
      if [[ -z "$created_at" ]] || ! run_jobs_all_green "$jobs_file" "$test_job_name"; then
        continue
      fi
      created_epoch=$(iso_to_epoch "$created_at") || continue
      age=$(( now_epoch - created_epoch ))
      if [[ $age -ge 0 && $age -le $max_age ]]; then
        emit_inherit \
          "$(jq -cn --argjson id "$run_id" --arg sha "$pr_head" --arg at "$created_at" --argjson age "$age" \
            '{run_id: $id, sha: $sha, created_at: $at, age_seconds: $age}')" \
          "$(jq -cn --argjson id "$run_id" --arg sha "$pr_head" --argjson pr "$pr_number" \
            '{step: "merged-pr-tree-equality", run_id: $id, sha: $sha, pull_request: $pr, detail: "merged PR head tree equals parent tree; executed all-green PR-context run"}')"
        return 0
      fi
    done < <(jq -r '[.workflow_runs[]? | select(.conclusion == "success")] | sort_by(.created_at // "") | reverse | .[].id // empty' \
      "$pr_runs_file")
  done < "$data_dir/pr-head-trees.jsonl"

  emit_none "no executed all-green evidence within $max_age seconds: parent push run absent or not fully executed, and no merged PR tree equality with a fresh green PR run"
  return 3
}

main() {
  local cmd="${1:-}"
  [[ -n "$cmd" ]] || fail "usage: release-evidence.sh <classify|decide> ..."
  shift
  case "$cmd" in
    classify) cmd_classify "$@" ;;
    decide) cmd_decide "$@" ;;
    *) fail "unknown subcommand: $cmd (expected classify or decide)" ;;
  esac
}

main "$@"
