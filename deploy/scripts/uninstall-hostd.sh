#!/usr/bin/env bash
# Remove the install-scoped host lifecycle service. Appliance data is retained.
set -Eeuo pipefail

SOVEREIGN_HOME="${SOVEREIGN_HOME:-$HOME/.sovereign}"
if [[ "$(uname -s)" == Darwin ]]; then
  LABEL="com.lazarus.sovereign-hostd"
  launchctl bootout "gui/$(id -u)/$LABEL" 2>/dev/null || true
  rm -f "$HOME/Library/LaunchAgents/$LABEL.plist"
elif [[ "$(uname -s)" == Linux ]]; then
  systemctl --user disable --now sovereign-hostd.service 2>/dev/null || true
  rm -f "${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user/sovereign-hostd.service"
  systemctl --user daemon-reload 2>/dev/null || true
fi

BIN_DIR="$HOME/.local/bin"
[[ -s "$SOVEREIGN_HOME/state/bin-dir" ]] && BIN_DIR="$(<"$SOVEREIGN_HOME/state/bin-dir")"
case "$BIN_DIR" in
  ""|/|"$HOME") echo "warning: refusing unsafe hostd binary directory: $BIN_DIR" >&2 ;;
  *) rm -f "$BIN_DIR/sovereign-hostd" ;;
esac
