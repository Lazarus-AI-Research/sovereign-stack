#!/usr/bin/env bash
# Visible one-shot launcher opened by the native macOS package in Terminal.
set -Eeuo pipefail

PACKAGE_ROOT="${SOVEREIGN_PACKAGE_ROOT:-/usr/local/share/sovereign-stack}"
VERSION="$(<"$PACKAGE_ROOT/VERSION")"
LOG_DIR="$HOME/Library/Logs/SovereignStack"
LOG_FILE="$LOG_DIR/install.log"
LOCK_DIR="$LOG_DIR/install.lock"
umask 077
mkdir -p "$LOG_DIR"
chmod 700 "$LOG_DIR"

if ! mkdir "$LOCK_DIR" 2>/dev/null; then
  OLD_PID=""
  [[ ! -r "$LOCK_DIR/pid" ]] || OLD_PID="$(<"$LOCK_DIR/pid")"
  if [[ "$OLD_PID" =~ ^[0-9]+$ ]] && kill -0 "$OLD_PID" >/dev/null 2>&1 &&
     ps -p "$OLD_PID" -o command= 2>/dev/null | grep -q 'launch-install.command'; then
    echo "A SovereignStack installation is already running (process $OLD_PID)."
    echo "Follow its progress in the existing Terminal window or at $LOG_FILE."
    [[ ! -t 0 ]] || read -r -p "Press Return to close this window. " _
    exit 0
  fi
  rm -rf "$LOCK_DIR"
  mkdir "$LOCK_DIR"
fi
printf '%s\n' "$$" > "$LOCK_DIR/pid"
cleanup() { rm -rf "$LOCK_DIR"; }
trap cleanup EXIT

touch "$LOG_FILE"
chmod 600 "$LOG_FILE"

{
  echo
  echo "SovereignStack $VERSION installation"
  echo "Progress is also saved to: $LOG_FILE"
  echo
} | tee -a "$LOG_FILE"
set +e
env SOVEREIGN_VERSION="$VERSION" SOVEREIGN_ACCESS_MODE=desktop \
  /bin/bash "$PACKAGE_ROOT/install.sh" 2>&1 | tee -a "$LOG_FILE"
INSTALL_STATUS=${PIPESTATUS[0]}
set -e
if (( INSTALL_STATUS != 0 )); then
  {
    echo
    echo "SovereignStack installation stopped with status $INSTALL_STATUS."
    echo "Existing appliance data is safe. Review $LOG_FILE, then run sovereign-install to retry."
  } | tee -a "$LOG_FILE"
  [[ ! -t 0 ]] || read -r -p "Press Return to close this window. " _
  exit "$INSTALL_STATUS"
fi

{
  echo
  echo "SovereignStack installation completed successfully. This window may be closed."
} | tee -a "$LOG_FILE"
