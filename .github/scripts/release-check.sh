#!/usr/bin/env bash

# release-check.sh — pure decision logic for the lightweight "Release Check"
# workflow (SOP sec 10.6). The workflow does the git/gh fetching; this script
# only decides, so every rule is locally testable.
#
# Subcommands:
#   containment --files <list-file> --version <v>
#       The PR diff must be non-empty and touch only
#       backend/cmd/server/VERSION and docs/releases/v<v>.md.
#
#   version-increment --version <v> --existing-tags <list-file>
#       X.Y.Z-ts.N must increment: N > highest existing ts.N for the same
#       upstream base, or N == 1 when this base has no published ts tag yet.
#
#   high-risk --release-files <list-file> --pr-body <body-file>
#       Mechanical high-risk path decision over the release content (paths
#       changed since the last published release). Subset list per SOP sec 7:
#       auth/authz, payment, migrations/, deploy/, deployer, secrets, email,
#       ratelimit. On a hit the PR body must link the audit conclusion
#       (a github.com pull/ URL); on a miss it prints audit-not-required.

set -euo pipefail

fail() {
  echo "release-check: $*" >&2
  exit 1
}

read_nonempty_file() {
  local file="$1" label="$2"
  [[ -f "$file" ]] || fail "$label file not found: $file"
  [[ -s "$file" ]] || fail "$label file is empty: $file"
}

cmd_containment() {
  local files="" version=""
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --files) files="${2:?}"; shift 2 ;;
      --version) version="${2:?}"; shift 2 ;;
      *) fail "containment: unknown argument: $1" ;;
    esac
  done
  [[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+-ts\.[1-9][0-9]*$ ]] || \
    fail "containment: version must be X.Y.Z-ts.N: $version"
  read_nonempty_file "$files" "changed files"
  local notes="docs/releases/v${version}.md"
  local f count=0
  while IFS= read -r f; do
    [[ -n "$f" ]] || continue
    if [[ "$f" != "backend/cmd/server/VERSION" && "$f" != "$notes" ]]; then
      fail "containment: release PR touches a path outside VERSION and release notes: $f"
    fi
    count=$(( count + 1 ))
  done < "$files"
  [[ "$count" -gt 0 ]] || fail "containment: release PR diff is empty"
  echo "containment ok: $count file(s), version $version"
}

cmd_version_increment() {
  local version="" tags_file=""
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --version) version="${2:?}"; shift 2 ;;
      --existing-tags) tags_file="${2:?}"; shift 2 ;;
      *) fail "version-increment: unknown argument: $1" ;;
    esac
  done
  [[ "$version" =~ ^([0-9]+\.[0-9]+\.[0-9]+)-ts\.([1-9][0-9]*)$ ]] || \
    fail "version-increment: version must be X.Y.Z-ts.N: $version"
  local base="${BASH_REMATCH[1]}"
  local n="${BASH_REMATCH[2]}"
  read_nonempty_file "$tags_file" "existing tags"

  local same_base_max=0 any_ts_tag=0 tag t_base t_n
  while IFS= read -r tag; do
    [[ -n "$tag" ]] || continue
    # Tags carry a "v" prefix (v1.2.3-ts.1); the version argument does not.
    if [[ "$tag" =~ ^v?([0-9]+\.[0-9]+\.[0-9]+)-ts\.([1-9][0-9]*)$ ]]; then
      t_base="${BASH_REMATCH[1]}"
      t_n="${BASH_REMATCH[2]}"
      any_ts_tag=1
      if [[ "$t_base" == "$base" && "$t_n" -gt "$same_base_max" ]]; then
        same_base_max="$t_n"
      fi
    fi
  done < "$tags_file"

  if [[ "$same_base_max" -gt 0 ]]; then
    [[ "$n" -gt "$same_base_max" ]] || \
      fail "version-increment: v${version} does not exceed the highest published revision for base ${base} (v${base}-ts.${same_base_max})"
    echo "version-increment ok: v${version} > v${base}-ts.${same_base_max}"
  elif [[ "$any_ts_tag" -eq 1 ]]; then
    [[ "$n" -eq 1 ]] || \
      fail "version-increment: base ${base} has no published ts tag yet, so the first revision must be ts.1 (got ts.${n})"
    echo "version-increment ok: v${version} is the first revision for base ${base}"
  else
    echo "version-increment ok: no published ts tags exist, v${version} accepted"
  fi
}

cmd_high_risk() {
  local release_files="" pr_body=""
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --release-files) release_files="${2:?}"; shift 2 ;;
      --pr-body) pr_body="${2:?}"; shift 2 ;;
      *) fail "high-risk: unknown argument: $1" ;;
    esac
  done
  read_nonempty_file "$release_files" "release files"
  [[ -f "$pr_body" ]] || fail "pr body file not found: $pr_body"

  # Over-inclusive substring match is intentional: a false positive only asks
  # for an audit link, which is cheaper than a missed high-risk change.
  local pattern='(auth|payment|migrations/|deploy/|deployer|secret|email|ratelimit|rate-limit|rate_limit)'
  local hits=""
  hits=$(grep -Ei "$pattern" "$release_files" || true)
  if [[ -z "$hits" ]]; then
    echo "high-risk ok: no high-risk paths in release content; audit not required (record in release notes)"
    return 0
  fi

  local hit_list
  hit_list=$(printf '%s\n' "$hits" | sort -u | tr '\n' ' ')
  if grep -Eq 'github\.com/[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+/pull/[0-9]+' "$pr_body"; then
    echo "high-risk hit (paths: $hit_list); PR body links an audit conclusion — accepted"
    return 0
  fi
  echo "high-risk hit (paths: $hit_list) but the PR body contains no github.com/pull/ audit link" >&2
  return 1
}

main() {
  local cmd="${1:-}"
  [[ -n "$cmd" ]] || fail "usage: release-check.sh <containment|version-increment|high-risk> ..."
  shift
  case "$cmd" in
    containment) cmd_containment "$@" ;;
    version-increment) cmd_version_increment "$@" ;;
    high-risk) cmd_high_risk "$@" ;;
    *) fail "unknown subcommand: $cmd" ;;
  esac
}

main "$@"
