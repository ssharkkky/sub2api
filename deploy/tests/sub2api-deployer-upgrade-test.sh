#!/usr/bin/env bash

set -euo pipefail
umask 077

REPO_ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
TEST_DIR=$(mktemp -d "${TMPDIR:-/tmp}/sub2api-deployer-upgrade-test.XXXXXX")
trap 'rm -rf -- "$TEST_DIR"' EXIT

ROOT="$TEST_DIR/root"
FAKE_BIN="$TEST_DIR/bin"
CONTAINER_ID=$(printf 'a%.0s' {1..64})
TARGET_VERSION="0.1.168-ts.1"
TARGET_DIGEST="sha256:$(printf 'c%.0s' {1..64})"
TARGET_IMAGE="ghcr.io/ssharkkky/sub2api@$TARGET_DIGEST"
TARGET_COMMIT=$(printf 'd%.0s' {1..40})
TARGET_ARCH="amd64"
JOB_ID="control-plane-upgrade-0001"

mkdir -p \
  "$FAKE_BIN" \
  "$ROOT/run/sub2api-deployer" \
  "$ROOT/usr/local/sbin" \
  "$ROOT/var/lib/sub2api-deployer/control-plane-staging" \
  "$ROOT/var/lib/sub2api-deployer/quarantine"

cat > "$FAKE_BIN/systemctl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[[ "$1" == "restart" && "$2" == "sub2api-deployer.service" ]]
count=0
[[ ! -f "$FAKE_RESTART_COUNT" ]] || count=$(cat "$FAKE_RESTART_COUNT")
printf '%s\n' "$((count + 1))" > "$FAKE_RESTART_COUNT"
printf '%s\n' "$*" >> "$FAKE_SYSTEMCTL_LOG"
EOF

cat > "$FAKE_BIN/curl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
count=0
[[ ! -f "$FAKE_RESTART_COUNT" ]] || count=$(cat "$FAKE_RESTART_COUNT")
if [[ "$FAKE_HEALTH_MODE" == "rollback" && "$count" -lt 2 ]]; then
  printf '%s\n' "$FAKE_WRONG_BUILD_HEALTH"
else
  printf '%s\n' "$FAKE_TARGET_HEALTH"
fi
EOF

cat > "$FAKE_BIN/sleep" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF

cat > "$FAKE_BIN/flock" <<'EOF'
#!/usr/bin/env bash
[[ "$1" == "-n" && "$2" == "9" ]]
EOF
chmod 0755 "$FAKE_BIN/systemctl" "$FAKE_BIN/curl" "$FAKE_BIN/sleep" "$FAKE_BIN/flock"

HELPER="$TEST_DIR/sub2api-deployer-upgrade"
sed \
  -e "s#^REQUEST=.*#REQUEST=\"$ROOT/var/lib/sub2api-deployer/control-plane-upgrade.json\"#" \
  -e "s#^STATE=.*#STATE=\"$ROOT/var/lib/sub2api-deployer/state.json\"#" \
  -e "s#^STAGING_ROOT=.*#STAGING_ROOT=\"$ROOT/var/lib/sub2api-deployer/control-plane-staging\"#" \
  -e "s#^QUARANTINE=.*#QUARANTINE=\"$ROOT/var/lib/sub2api-deployer/quarantine\"#" \
  -e "s#^BINARY=.*#BINARY=\"$ROOT/usr/local/sbin/sub2api-deployer\"#" \
  -e "s#^LOCK=.*#LOCK=\"$ROOT/run/sub2api-deployer-install.lock\"#" \
  -e "s#^SOCKET=.*#SOCKET=\"$ROOT/run/sub2api-deployer/deployer.sock\"#" \
  -e "s#^NEXT_BINARY=.*#NEXT_BINARY=\"$ROOT/usr/local/sbin/.sub2api-deployer.next\"#" \
  "$REPO_ROOT/deploy/sub2api-deployer-upgrade.sh" > "$HELPER"
chmod 0755 "$HELPER"

TARGET_HEALTH=$(jq -cn \
  --arg id "$CONTAINER_ID" \
  --arg version "$TARGET_VERSION" \
  --arg commit "$TARGET_COMMIT" \
  --arg arch "$TARGET_ARCH" \
  '{status:"ok", degraded:false, job_running:false, active_container_id:$id, active_version:$version, control_plane_upgrade_ready:true, build:{version:$version, commit:$commit, type:"release", arch:$arch}}')
WRONG_BUILD_HEALTH=$(jq -cn \
  --arg id "$CONTAINER_ID" \
  --arg version "$TARGET_VERSION" \
  '{status:"ok", degraded:false, job_running:false, active_container_id:$id, active_version:$version, control_plane_upgrade_ready:true, build:{version:"old", commit:"old", type:"release", arch:"amd64"}}')

write_fixture() {
  local status_attempt="${1:-0}"
  local stage="$ROOT/var/lib/sub2api-deployer/control-plane-staging/$JOB_ID"
  rm -rf -- "$stage" "$ROOT/var/lib/sub2api-deployer/quarantine"
  mkdir -p "$stage" "$ROOT/var/lib/sub2api-deployer/quarantine"
  printf '#!/usr/bin/env bash\n# old-deployer\nexit 0\n' > "$ROOT/usr/local/sbin/sub2api-deployer"
  printf '#!/usr/bin/env bash\n# target-deployer\nexit 0\n' > "$stage/sub2api-deployer"
  printf '{"schema":1}\n' > "$stage/CONTROL-PLANE-MANIFEST.json"
  chmod 0755 "$ROOT/usr/local/sbin/sub2api-deployer" "$stage/sub2api-deployer"
  binary_sha="sha256:$(sha256sum "$stage/sub2api-deployer" | awk '{print $1}')"
  manifest_sha="sha256:$(sha256sum "$stage/CONTROL-PLANE-MANIFEST.json" | awk '{print $1}')"
  jq -n \
    --arg job_id "$JOB_ID" \
    --arg id "$CONTAINER_ID" \
    --arg version "$TARGET_VERSION" \
    --arg image "$TARGET_IMAGE" \
    --arg binary "$stage/sub2api-deployer" \
    --arg binary_sha "$binary_sha" \
    --arg manifest "$stage/CONTROL-PLANE-MANIFEST.json" \
    --arg manifest_sha "$manifest_sha" \
    --arg commit "$TARGET_COMMIT" \
    --arg arch "$TARGET_ARCH" \
    '{schema:2, job_id:$job_id, container_id:$id, container_name:"sub2api-green", target_version:$version, expected_image:$image, expected_image_digest:($image | split("@") | .[1]), staged_binary:$binary, staged_binary_sha256:$binary_sha, staged_manifest:$manifest, staged_manifest_sha256:$manifest_sha, expected_commit:$commit, expected_arch:$arch}' \
    > "$ROOT/var/lib/sub2api-deployer/control-plane-upgrade.json"
  jq -n \
    --arg job_id "$JOB_ID" \
    --arg id "$CONTAINER_ID" \
    --arg version "$TARGET_VERSION" \
    --argjson attempt "$status_attempt" \
    '{schema:1, job_id:$job_id, container_id:$id, target_version:$version, status:"pending", attempt:$attempt}' \
    > "$ROOT/var/lib/sub2api-deployer/control-plane-upgrade.json.status"
  jq -n \
    --arg job_id "$JOB_ID" \
    --arg id "$CONTAINER_ID" \
    --arg version "$TARGET_VERSION" \
    --arg image "$TARGET_IMAGE" \
    '{degraded:false, active_container_id:$id, active_container:"sub2api-green", active_version:$version, active_image:$image, job:{id:$job_id,status:"succeeded"}}' \
    > "$ROOT/var/lib/sub2api-deployer/state.json"
  rm -f -- "$TEST_DIR/systemctl.log" "$TEST_DIR/restart-count"
}

run_helper() {
  PATH="$FAKE_BIN:$PATH" \
  FAKE_RESTART_COUNT="$TEST_DIR/restart-count" \
  FAKE_SYSTEMCTL_LOG="$TEST_DIR/systemctl.log" \
  FAKE_HEALTH_MODE="$1" \
  FAKE_TARGET_HEALTH="$TARGET_HEALTH" \
  FAKE_WRONG_BUILD_HEALTH="$WRONG_BUILD_HEALTH" \
    "$HELPER"
}

# Full activation replaces the binary, restarts once, and reaches succeeded.
write_fixture
expected_target="$TEST_DIR/expected-target"
cp -a "$ROOT/var/lib/sub2api-deployer/control-plane-staging/$JOB_ID/sub2api-deployer" "$expected_target"
run_helper target
cmp -s "$expected_target" "$ROOT/usr/local/sbin/sub2api-deployer"
[[ ! -e "$ROOT/var/lib/sub2api-deployer/control-plane-upgrade.json" ]]
[[ ! -e "$ROOT/var/lib/sub2api-deployer/control-plane-staging/$JOB_ID" ]]
[[ $(cat "$TEST_DIR/restart-count") == 1 ]]
jq -e '.status == "succeeded" and .attempt == 1' "$ROOT/var/lib/sub2api-deployer/control-plane-upgrade.json.status" >/dev/null

# A crash after binary replacement is idempotent when the live process already
# has the target identity: no second service restart occurs.
write_fixture
cp -a "$ROOT/var/lib/sub2api-deployer/control-plane-staging/$JOB_ID/sub2api-deployer" "$ROOT/usr/local/sbin/sub2api-deployer"
run_helper target
[[ ! -e "$TEST_DIR/restart-count" ]]
jq -e '.status == "succeeded"' "$ROOT/var/lib/sub2api-deployer/control-plane-upgrade.json.status" >/dev/null

# Failed target health restores and verifies the previous binary, then leaves a
# bounded retry request for the timer.
write_fixture
cp -a "$ROOT/usr/local/sbin/sub2api-deployer" "$TEST_DIR/expected-old"
if run_helper rollback >"$TEST_DIR/rollback.log" 2>&1; then
  echo "failed target health unexpectedly succeeded" >&2
  exit 1
fi
cmp -s "$TEST_DIR/expected-old" "$ROOT/usr/local/sbin/sub2api-deployer"
[[ -f "$ROOT/var/lib/sub2api-deployer/control-plane-upgrade.json" ]]
[[ $(cat "$TEST_DIR/restart-count") == 2 ]]
jq -e '.status == "retrying" and .attempt == 1 and .max_attempts == 5 and .error_class == "transient"' "$ROOT/var/lib/sub2api-deployer/control-plane-upgrade.json.status" >/dev/null

# The fifth failed attempt is terminal and quarantines the request.
write_fixture 4
if run_helper rollback >"$TEST_DIR/max-attempts.log" 2>&1; then
  echo "fifth failed attempt unexpectedly succeeded" >&2
  exit 1
fi
[[ ! -e "$ROOT/var/lib/sub2api-deployer/control-plane-upgrade.json" ]]
[[ $(find "$ROOT/var/lib/sub2api-deployer/quarantine" -type f | wc -l | tr -d ' ') == 1 ]]
jq -e '.status == "failed" and .attempt == 5' "$ROOT/var/lib/sub2api-deployer/control-plane-upgrade.json.status" >/dev/null

# A malformed request uses the pre-existing status identity and cannot remain
# permanently pending.
write_fixture
printf '{not-json\n' > "$ROOT/var/lib/sub2api-deployer/control-plane-upgrade.json"
if run_helper target >"$TEST_DIR/malformed.log" 2>&1; then
  echo "malformed request unexpectedly succeeded" >&2
  exit 1
fi
jq -e --arg job_id "$JOB_ID" '.status == "failed" and .job_id == $job_id and .error_class == "permanent"' "$ROOT/var/lib/sub2api-deployer/control-plane-upgrade.json.status" >/dev/null
[[ $(find "$ROOT/var/lib/sub2api-deployer/quarantine" -type f | wc -l | tr -d ' ') == 1 ]]

if grep -Eq '(^|[[:space:]])docker([[:space:]]|$)' "$REPO_ROOT/deploy/sub2api-deployer-upgrade.sh"; then
  echo "stable activator must not call Docker" >&2
  exit 1
fi

echo "sub2api deployer control-plane activator tests passed"
