#!/usr/bin/env bash
set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TEST_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/sovereign-package-test.XXXXXX")"
trap 'rm -rf "$TEST_ROOT"' EXIT
PACKAGE_ROOT="$TEST_ROOT/package"
STATE_DIR="$TEST_ROOT/state"
OWNER_HOME="$TEST_ROOT/owner-home"
BIN="$TEST_ROOT/bin"
SYSTEMCTL_LOG="$TEST_ROOT/systemctl.log"
TEST_GROUP="$(/usr/bin/id -gn)"
mkdir -p "$PACKAGE_ROOT" "$OWNER_HOME" "$BIN"
cp "$ROOT/VERSION" "$PACKAGE_ROOT/VERSION"

cat > "$PACKAGE_ROOT/provision-ubuntu.sh" <<'SH'
#!/usr/bin/env bash
printf 'schema_version=1\nowner_uid=1000\nchanged=1\nreboot_required=1\nmanaged_docker=1\nboot_id=test-boot\n' \
  > "${PACKAGE_RESULT:?}"
chmod 644 "$PACKAGE_RESULT"
printf 'result_file=%s\nchanged=1\nreboot_required=1\nmanaged_docker=1\n' "$PACKAGE_RESULT"
SH
cat > "$PACKAGE_ROOT/install.sh" <<'SH'
#!/usr/bin/env bash
printf 'home=%s\nresult=%s\nauto_reboot=%s\n' \
  "$HOME" "${SOVEREIGN_UBUNTU_PROVISION_RESULT:-}" "${SOVEREIGN_AUTO_REBOOT:-}" \
  > "${PACKAGE_INSTALL_LOG:?}"
exit 194
SH
cat > "$BIN/getent" <<SH
#!/usr/bin/env bash
if [[ "\${1:-}" == passwd && "\${2:-}" == appliance ]]; then
  printf 'appliance:x:1000:1000::%s:/bin/bash\n' "$OWNER_HOME"
  exit 0
fi
exit 1
SH
cat > "$BIN/id" <<SH
#!/usr/bin/env bash
[[ "\${1:-}" == -u && "\${2:-}" == appliance ]] && { echo 1000; exit 0; }
[[ "\${1:-}" == -gn ]] && { echo '$TEST_GROUP'; exit 0; }
exec /usr/bin/id "\$@"
SH
cat > "$BIN/runuser" <<'SH'
#!/usr/bin/env bash
[[ "${1:-}" == -u && "${3:-}" == -- ]] || exit 2
shift 3
exec "$@"
SH
cat > "$BIN/install" <<'SH'
#!/usr/bin/env bash
directory=0
arguments=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    -d) directory=1; shift ;;
    -m|-o|-g) shift 2 ;;
    *) arguments+=("$1"); shift ;;
  esac
done
if (( directory == 1 )); then
  mkdir -p "${arguments[@]}"
else
  : > "${arguments[${#arguments[@]}-1]}"
fi
SH
cat > "$BIN/systemctl" <<'SH'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "${SYSTEMCTL_LOG:?}"
exit 0
SH
chmod 755 "$PACKAGE_ROOT/provision-ubuntu.sh" "$PACKAGE_ROOT/install.sh" "$BIN/"*

# The maintainer script records the owner and schedules the persistent service;
# it never attempts apt or appliance installation while dpkg owns its lock.
SYSTEMCTL_LOG="$SYSTEMCTL_LOG" \
SUDO_USER=appliance \
SOVEREIGN_PACKAGE_STATE_DIR="$STATE_DIR" \
SOVEREIGN_PACKAGE_UNIT=test-sovereign-package.service \
SOVEREIGN_POSTINST_SYSTEMD=1 \
PATH="$BIN:/usr/bin:/bin" \
  bash "$ROOT/packaging/linux/postinst" > "$TEST_ROOT/postinst.log" 2>&1
grep -qx appliance "$STATE_DIR/package-owner"
grep -qx 'daemon-reload' "$SYSTEMCTL_LOG"
grep -qx 'enable --now --no-block test-sovereign-package.service' "$SYSTEMCTL_LOG"
grep -q 'journalctl -fu test-sovereign-package.service' "$TEST_ROOT/postinst.log"
! grep -q 'nohup\|provision-ubuntu' "$ROOT/packaging/linux/postinst"

# The systemd coordinator runs after the transaction, provisions as root, then
# hands appliance ownership back to the selected login user. Exit 194 is the
# intentional reboot boundary, not a hidden package failure.
: > "$SYSTEMCTL_LOG"
set +e
PACKAGE_RESULT="$TEST_ROOT/provision.env" \
PACKAGE_INSTALL_LOG="$TEST_ROOT/install.env" \
SYSTEMCTL_LOG="$SYSTEMCTL_LOG" \
SOVEREIGN_PACKAGE_ROOT="$PACKAGE_ROOT" \
SOVEREIGN_PACKAGE_STATE_DIR="$STATE_DIR" \
SOVEREIGN_PACKAGE_LOG_DIR="$TEST_ROOT/root-log" \
SOVEREIGN_PACKAGE_UNIT=test-sovereign-package.service \
PATH="$BIN:/usr/bin:/bin" \
  bash "$ROOT/packaging/linux/package-install" > "$TEST_ROOT/coordinator.log" 2>&1
COORDINATOR_STATUS=$?
set -e
if (( COORDINATOR_STATUS != 0 )); then
  cat "$TEST_ROOT/coordinator.log" >&2
  exit "$COORDINATOR_STATUS"
fi
grep -qx "home=$OWNER_HOME" "$TEST_ROOT/install.env"
grep -qx "result=$TEST_ROOT/provision.env" "$TEST_ROOT/install.env"
grep -qx 'auto_reboot=0' "$TEST_ROOT/install.env"
grep -qx 'disable test-sovereign-package.service' "$SYSTEMCTL_LOG"
grep -q 'resume automatically' "$TEST_ROOT/coordinator.log"

echo "Linux package entrypoint tests passed"
