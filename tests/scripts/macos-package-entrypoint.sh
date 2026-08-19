#!/usr/bin/env bash
set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TEST_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/sovereign-macos-package-test.XXXXXX")"
trap 'rm -rf "$TEST_ROOT"' EXIT
PACKAGE_ROOT="$TEST_ROOT/package"
OWNER_HOME="$TEST_ROOT/owner-home"
BIN="$TEST_ROOT/bin"
LAUNCHCTL_LOG="$TEST_ROOT/launchctl-arguments"
INSTALL_LOG="$TEST_ROOT/install-environment"
mkdir -p "$PACKAGE_ROOT" "$OWNER_HOME" "$BIN"
cp "$ROOT/VERSION" "$PACKAGE_ROOT/VERSION"
cp "$ROOT/packaging/macos/launch-install.command" "$PACKAGE_ROOT/launch-install.command"
chmod 755 "$PACKAGE_ROOT/launch-install.command"

cat > "$BIN/launchctl" <<'SH'
#!/usr/bin/env bash
printf '%s\n' "$@" > "${LAUNCHCTL_LOG:?}"
SH
cat > "$BIN/sudo" <<'SH'
#!/usr/bin/env bash
exit 99
SH
cat > "$BIN/open" <<'SH'
#!/usr/bin/env bash
exit 99
SH
cat > "$BIN/ps" <<'SH'
#!/usr/bin/env bash
echo '/usr/local/share/sovereign-stack/launch-install.command'
SH
chmod 755 "$BIN/launchctl" "$BIN/sudo" "$BIN/open" "$BIN/ps"

# The package script crosses into the signed-in desktop user's launchd domain
# and asks Terminal to open the visible launcher. The fake launchctl records
# every argument without starting a GUI or requiring root.
LAUNCHCTL_LOG="$LAUNCHCTL_LOG" \
SOVEREIGN_PACKAGE_ROOT="$PACKAGE_ROOT" \
SOVEREIGN_PACKAGE_LAUNCHCTL="$BIN/launchctl" \
SOVEREIGN_PACKAGE_SUDO="$BIN/sudo" \
SOVEREIGN_PACKAGE_OPEN="$BIN/open" \
SOVEREIGN_INSTALL_USER=appliance \
SOVEREIGN_INSTALL_HOME="$OWNER_HOME" \
SOVEREIGN_INSTALL_UID=501 \
  bash "$ROOT/packaging/macos/postinstall" > "$TEST_ROOT/postinstall.log" 2>&1
printf '%s\n' \
  asuser \
  501 \
  "$BIN/sudo" \
  -u \
  appliance \
  env \
  "HOME=$OWNER_HOME" \
  USER=appliance \
  "$BIN/open" \
  -a \
  Terminal \
  "$PACKAGE_ROOT/launch-install.command" \
  > "$TEST_ROOT/expected-launchctl-arguments"
cmp "$TEST_ROOT/expected-launchctl-arguments" "$LAUNCHCTL_LOG"
grep -q 'installation opened in Terminal for appliance' "$TEST_ROOT/postinstall.log"
! grep -q 'nohup' "$ROOT/packaging/macos/postinstall"
! grep -Eq '[[:space:]]&[[:space:]]*($|#)' "$ROOT/packaging/macos/postinstall"

cat > "$PACKAGE_ROOT/install.sh" <<'SH'
#!/usr/bin/env bash
printf 'home=%s\nuser=%s\nversion=%s\naccess=%s\n' \
  "$HOME" "$USER" "$SOVEREIGN_VERSION" "$SOVEREIGN_ACCESS_MODE" \
  > "${PACKAGE_INSTALL_LOG:?}"
exit "${PACKAGE_INSTALL_STATUS:-0}"
SH
chmod 755 "$PACKAGE_ROOT/install.sh"

# A live launcher PID prevents a duplicate installer from starting. The
# original process retains ownership of the lock for its eventual cleanup.
LOCK_DIR="$OWNER_HOME/Library/Logs/SovereignStack/install.lock"
mkdir -p "$LOCK_DIR"
printf '%s\n' "$$" > "$LOCK_DIR/pid"
HOME="$OWNER_HOME" \
USER=appliance \
SOVEREIGN_PACKAGE_ROOT="$PACKAGE_ROOT" \
PATH="$BIN:/usr/bin:/bin" \
  bash "$PACKAGE_ROOT/launch-install.command" > "$TEST_ROOT/launcher-duplicate.log" 2>&1
grep -q "installation is already running (process $$)" "$TEST_ROOT/launcher-duplicate.log"
test -d "$LOCK_DIR"
rm -rf "$LOCK_DIR"

# The launcher keeps output visible, passes the package version and desktop
# mode to the shared installer, writes a private log, and releases its lock.
set +e
HOME="$OWNER_HOME" \
USER=appliance \
PACKAGE_INSTALL_LOG="$INSTALL_LOG" \
PACKAGE_INSTALL_STATUS=0 \
SOVEREIGN_PACKAGE_ROOT="$PACKAGE_ROOT" \
  bash "$PACKAGE_ROOT/launch-install.command" > "$TEST_ROOT/launcher-success.log" 2>&1
LAUNCHER_STATUS=$?
set -e
if (( LAUNCHER_STATUS != 0 )); then
  cat "$TEST_ROOT/launcher-success.log" >&2
  echo "macOS launcher returned $LAUNCHER_STATUS for a successful install" >&2
  exit 1
fi
grep -qx "home=$OWNER_HOME" "$INSTALL_LOG"
grep -qx 'user=appliance' "$INSTALL_LOG"
grep -qx "version=$(<"$ROOT/VERSION")" "$INSTALL_LOG"
grep -qx 'access=desktop' "$INSTALL_LOG"
test -f "$OWNER_HOME/Library/Logs/SovereignStack/install.log"
test ! -e "$OWNER_HOME/Library/Logs/SovereignStack/install.lock"
if [[ "$(uname -s)" == Darwin ]]; then
  LOG_MODE="$(stat -f '%Lp' "$OWNER_HOME/Library/Logs/SovereignStack/install.log")"
  DIR_MODE="$(stat -f '%Lp' "$OWNER_HOME/Library/Logs/SovereignStack")"
else
  LOG_MODE="$(stat -c '%a' "$OWNER_HOME/Library/Logs/SovereignStack/install.log")"
  DIR_MODE="$(stat -c '%a' "$OWNER_HOME/Library/Logs/SovereignStack")"
fi
[[ "$LOG_MODE" == 600 ]]
[[ "$DIR_MODE" == 700 ]]
grep -q 'installation completed successfully' "$TEST_ROOT/launcher-success.log"

# Failures remain failures, are copied to the durable log, and do not leave a
# stale duplicate-install lock that would prevent a retry.
set +e
HOME="$OWNER_HOME" \
USER=appliance \
PACKAGE_INSTALL_LOG="$INSTALL_LOG" \
PACKAGE_INSTALL_STATUS=42 \
SOVEREIGN_PACKAGE_ROOT="$PACKAGE_ROOT" \
  bash "$PACKAGE_ROOT/launch-install.command" > "$TEST_ROOT/launcher-failure.log" 2>&1
LAUNCHER_STATUS=$?
set -e
if (( LAUNCHER_STATUS != 42 )); then
  cat "$TEST_ROOT/launcher-failure.log" >&2
  echo "macOS launcher returned $LAUNCHER_STATUS instead of 42" >&2
  exit 1
fi
test ! -e "$OWNER_HOME/Library/Logs/SovereignStack/install.lock"
grep -q 'installation stopped with status 42' "$TEST_ROOT/launcher-failure.log"
grep -q 'installation stopped with status 42' "$OWNER_HOME/Library/Logs/SovereignStack/install.log"
grep -q 'run sovereign-install to retry' "$OWNER_HOME/Library/Logs/SovereignStack/install.log"

echo "macOS package entrypoint tests passed"
