#!/usr/bin/env bash

set -euo pipefail
umask 077

[[ "${SUB2API_SYSTEMD_TEST_EPHEMERAL:-}" == "1" ]] || {
  echo "refusing to modify systemd outside an explicitly ephemeral test host" >&2
  exit 2
}
[[ $(id -u) == 0 ]] || { echo "systemd integration test must run as root" >&2; exit 2; }
[[ $(uname -s) == Linux ]] || { echo "systemd integration test requires Linux" >&2; exit 2; }
[[ $# == 1 && -x "$1" ]] || { echo "usage: $0 <sub2api-deployer-binary>" >&2; exit 2; }

SOURCE_BINARY=$(readlink -f -- "$1")
REPO_ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
INSTALLED_BINARY=/usr/local/sbin/sub2api-deployer
REAL_BINARY=/usr/local/sbin/sub2api-deployer.systemd-test-real
CONFIG_DIR=/etc/sub2api-deployer
CONFIG_FILE="$CONFIG_DIR/config.json"
STATE_ROOT=/var/lib/sub2api-deployer/systemd-activation-test
REQUEST="$STATE_ROOT/control-plane-upgrade.json"
STATUS="$REQUEST.status"
SERVICE_FILE=/etc/systemd/system/sub2api-deployer.service
UPGRADE_FILE=/etc/systemd/system/sub2api-deployer-upgrade.service
TIMER_FILE=/etc/systemd/system/sub2api-deployer-upgrade.timer
TIMER_DROPIN=/etc/systemd/system/sub2api-deployer-upgrade.timer.d/90-systemd-test.conf
ARM_FILE="$STATE_ROOT/arm-wrapper-delay"
SLEEPING_FILE="$STATE_ROOT/wrapper-sleeping"
SLEPT_FILE="$STATE_ROOT/wrapper-slept"

for path in "$INSTALLED_BINARY" "$REAL_BINARY" "$CONFIG_FILE" "$SERVICE_FILE" "$UPGRADE_FILE" "$TIMER_FILE"; do
  [[ ! -e "$path" && ! -L "$path" ]] || {
    echo "ephemeral host is not clean; refusing to replace $path" >&2
    exit 2
  }
done
systemctl is-system-running --wait >/dev/null 2>&1 || {
  state=$(systemctl is-system-running 2>/dev/null || true)
  [[ "$state" == running || "$state" == degraded ]] || { echo "systemd is not usable: $state" >&2; exit 2; }
}
systemctl is-active --quiet docker.service || { echo "docker.service must be active" >&2; exit 2; }

cleanup() {
  set +e
  systemctl disable --now sub2api-deployer-upgrade.timer >/dev/null 2>&1
  systemctl stop sub2api-deployer-upgrade.service sub2api-deployer.service >/dev/null 2>&1
  rm -f -- "$TIMER_DROPIN" "$UPGRADE_FILE" "$TIMER_FILE" "$SERVICE_FILE"
  rmdir -- "$(dirname -- "$TIMER_DROPIN")" >/dev/null 2>&1
  systemctl daemon-reload >/dev/null 2>&1
  rm -f -- "$INSTALLED_BINARY" "$REAL_BINARY" "$CONFIG_FILE"
  rm -rf -- "$STATE_ROOT"
  rmdir -- "$CONFIG_DIR" >/dev/null 2>&1
}
trap cleanup EXIT

install -d -m 0700 "$CONFIG_DIR" "$STATE_ROOT"
install -m 0755 "$SOURCE_BINARY" "$REAL_BINARY"
cat > "$INSTALLED_BINARY" <<EOF
#!/usr/bin/env bash
set -euo pipefail
if [[ "\${1:-}" == "--activate-staged-control-plane" && -e "$ARM_FILE" && ! -e "$SLEPT_FILE" ]]; then
  : > "$SLEPT_FILE"
  : > "$SLEEPING_FILE"
  sleep 30
fi
exec "$REAL_BINARY" "\$@"
EOF
chmod 0755 "$INSTALLED_BINARY"

jq \
  --arg state "$STATE_ROOT/state.json" \
  --arg request "$REQUEST" '
    .state_path = $state
    | .control_plane_upgrade_path = $request
    | .control_plane_upgrade_command = ["/usr/bin/systemctl", "start", "--no-block", "sub2api-deployer-upgrade.service"]
  ' "$REPO_ROOT/deploy/sub2api-deployer.example.json" > "$CONFIG_FILE"
"$INSTALLED_BINARY" -config "$CONFIG_FILE" -check >/dev/null

cat > "$SERVICE_FILE" <<'EOF'
[Unit]
Description=Sub2API systemd integration test daemon

[Service]
Type=simple
ExecStart=/bin/sleep infinity
EOF
install -m 0644 "$REPO_ROOT/deploy/sub2api-deployer-upgrade.service" "$UPGRADE_FILE"
install -m 0644 "$REPO_ROOT/deploy/sub2api-deployer-upgrade.timer" "$TIMER_FILE"
install -d -m 0755 "$(dirname -- "$TIMER_DROPIN")"
cat > "$TIMER_DROPIN" <<'EOF'
[Timer]
OnBootSec=
OnUnitInactiveSec=
OnBootSec=1s
OnUnitInactiveSec=2s
AccuracySec=100ms
EOF
chmod 0644 "$SERVICE_FILE" "$TIMER_DROPIN"
systemctl daemon-reload
systemd-analyze verify "$SERVICE_FILE" "$UPGRADE_FILE" "$TIMER_FILE"
systemctl start sub2api-deployer.service
DAEMON_PID=$(systemctl show --property=MainPID --value sub2api-deployer.service)
[[ "$DAEMON_PID" =~ ^[1-9][0-9]*$ ]]

EFFECTIVE=$(systemctl show --property=ExecStart --value sub2api-deployer-upgrade.service)
[[ -z "$(systemctl show --property=DropInPaths --value sub2api-deployer-upgrade.service)" ]]
[[ $(grep -oF 'argv[]=' <<<"$EFFECTIVE" | wc -l) == 1 ]]
[[ $(grep -oE '(^|[;{][[:space:]]*)path=' <<<"$EFFECTIVE" | wc -l) == 1 ]]
[[ "$EFFECTIVE" == *'path=/usr/local/sbin/sub2api-deployer ;'* ]]
[[ "$EFFECTIVE" == *'argv[]=/usr/local/sbin/sub2api-deployer --activate-staged-control-plane ;'* ]]

# No request is the normal path. It must stay silent and leave the daemon and
# state untouched across both manual invocations and real timer cycles.
START_EPOCH=$(date +%s)
for _ in 1 2 3; do
  systemctl start sub2api-deployer-upgrade.service
  ! systemctl is-failed --quiet sub2api-deployer-upgrade.service
done
[[ ! -e "$REQUEST" && ! -e "$STATUS" ]]
[[ $(systemctl show --property=MainPID --value sub2api-deployer.service) == "$DAEMON_PID" ]]

PREVIOUS_INVOCATION=$(systemctl show --property=InvocationID --value sub2api-deployer-upgrade.service)
systemctl enable --now sub2api-deployer-upgrade.timer
CHANGES=0
for _ in $(seq 1 80); do
  CURRENT_INVOCATION=$(systemctl show --property=InvocationID --value sub2api-deployer-upgrade.service)
  if [[ -n "$CURRENT_INVOCATION" && "$CURRENT_INVOCATION" != "$PREVIOUS_INVOCATION" ]]; then
    CHANGES=$((CHANGES + 1))
    PREVIOUS_INVOCATION=$CURRENT_INVOCATION
  fi
  (( CHANGES >= 2 )) && break
  sleep 0.25
done
(( CHANGES >= 2 )) || { echo "timer did not execute two clean no-request cycles" >&2; exit 1; }
! systemctl is-failed --quiet sub2api-deployer-upgrade.service
[[ ! -e "$STATUS" ]]
[[ $(systemctl show --property=MainPID --value sub2api-deployer.service) == "$DAEMON_PID" ]]
if journalctl -u sub2api-deployer-upgrade.service --since "@$START_EPOCH" --no-pager -o cat | \
    grep -Eq 'configuration is valid|activate staged control plane'; then
  echo "no-request timer path emitted application log noise" >&2
  exit 1
fi

# Kill the activator before it can consume a malformed request. The next timer
# run must retain and then quarantine the request; a following no-op run must
# clear systemd's failed state without touching the live daemon.
printf '%s\n' '{malformed' > "$REQUEST"
chmod 0600 "$REQUEST"
: > "$ARM_FILE"
systemctl start --no-block sub2api-deployer-upgrade.service
for _ in $(seq 1 40); do
  [[ -e "$SLEEPING_FILE" ]] && break
  sleep 0.1
done
[[ -e "$SLEEPING_FILE" ]] || { echo "activator wrapper did not enter the injected crash window" >&2; exit 1; }
systemctl kill --kill-who=main --signal=KILL sub2api-deployer-upgrade.service
for _ in $(seq 1 120); do
  [[ ! -e "$REQUEST" && -e "$STATUS" ]] && break
  sleep 0.25
done
[[ ! -e "$REQUEST" && -f "$STATUS" ]] || { echo "timer did not converge the killed activation request" >&2; exit 1; }
jq -e '.status == "failed" and .error_class == "permanent"' "$STATUS" >/dev/null
find "$STATE_ROOT/quarantine" -maxdepth 1 -type f -name 'control-plane-upgrade.*.json' | grep -q .
for _ in $(seq 1 40); do
  ! systemctl is-failed --quiet sub2api-deployer-upgrade.service && break
  sleep 0.25
done
! systemctl is-failed --quiet sub2api-deployer-upgrade.service
[[ $(systemctl show --property=MainPID --value sub2api-deployer.service) == "$DAEMON_PID" ]]

echo "real systemd control-plane activation tests passed"
