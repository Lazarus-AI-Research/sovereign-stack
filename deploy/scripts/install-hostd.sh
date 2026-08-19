#!/usr/bin/env bash
# Install the allowlisted Sovereign host lifecycle service for the current user.
set -Eeuo pipefail

SOVEREIGN_HOME="${SOVEREIGN_HOME:-$HOME/.sovereign}"
HOSTD_BINARY="${SOVEREIGN_HOSTD_BINARY:?set SOVEREIGN_HOSTD_BINARY}"
CLI_BINARY="${SOVEREIGN_CLI_BINARY:?set SOVEREIGN_CLI_BINARY}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LAUNCHD_HELPER="$SCRIPT_DIR/launchd-service.sh"

[[ -x "$HOSTD_BINARY" && -x "$CLI_BINARY" ]] || {
  echo "error: sovereign-hostd and sovereign must be executable" >&2
  exit 1
}
[[ -s "$SOVEREIGN_HOME/state/hostd-token" ]] || {
  echo "error: hostd token is missing" >&2
  exit 1
}

mkdir -p "$SOVEREIGN_HOME/logs/hostd"

if [[ "$(uname -s)" == Darwin ]]; then
  [[ -x "$LAUNCHD_HELPER" ]] || { echo "error: launchd service helper is missing" >&2; exit 1; }
  LABEL="com.lazarus.sovereign-hostd"
  PLIST_DIR="$HOME/Library/LaunchAgents"
  PLIST="$PLIST_DIR/$LABEL.plist"
  mkdir -p "$PLIST_DIR"
  escape() { printf '%s' "$1" | sed 's/&/\&amp;/g; s/</\&lt;/g; s/>/\&gt;/g; s/"/\&quot;/g'; }
  TEMP="$PLIST.tmp.$$"
  trap 'rm -f "$TEMP"' EXIT
  cat > "$TEMP" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>$LABEL</string>
  <key>ProgramArguments</key><array><string>$(escape "$HOSTD_BINARY")</string></array>
  <key>EnvironmentVariables</key><dict>
    <key>SOVEREIGN_HOME</key><string>$(escape "$SOVEREIGN_HOME")</string>
    <key>SOVEREIGN_CLI_PATH</key><string>$(escape "$CLI_BINARY")</string>
  </dict>
  <key>RunAtLoad</key><true/><key>KeepAlive</key><true/><key>ThrottleInterval</key><integer>5</integer>
  <key>StandardOutPath</key><string>$(escape "$SOVEREIGN_HOME/logs/hostd/stdout.log")</string>
  <key>StandardErrorPath</key><string>$(escape "$SOVEREIGN_HOME/logs/hostd/stderr.log")</string>
</dict></plist>
EOF
  "$LAUNCHD_HELPER" install "$LABEL" "$TEMP" "$PLIST" http://127.0.0.1:9191/host/v1/health
elif [[ "$(uname -s)" == Linux ]]; then
  UNIT_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"
  UNIT="$UNIT_DIR/sovereign-hostd.service"
  mkdir -p "$UNIT_DIR"
  TEMP="$UNIT.tmp.$$"
  trap 'rm -f "$TEMP"' EXIT
  HOSTD_LISTEN="$(docker network inspect bridge --format '{{(index .IPAM.Config 0).Gateway}}' 2>/dev/null || true):9191"
  [[ "$HOSTD_LISTEN" != :9191 ]] || HOSTD_LISTEN="127.0.0.1:9191"
  case "$SOVEREIGN_HOME$HOSTD_BINARY$CLI_BINARY$HOSTD_LISTEN" in
    *$'\n'*|*$'\r'*|*'"'*) echo "error: host service paths contain unsupported characters" >&2; exit 1 ;;
  esac
  cat > "$TEMP" <<EOF
[Unit]
Description=SovereignStack allowlisted host lifecycle service
After=docker.service

[Service]
ExecStart="$HOSTD_BINARY"
Environment="SOVEREIGN_HOME=$SOVEREIGN_HOME"
Environment="SOVEREIGN_CLI_PATH=$CLI_BINARY"
Environment="SOVEREIGN_HOSTD_LISTEN=$HOSTD_LISTEN"
Restart=on-failure
RestartSec=3

[Install]
WantedBy=default.target
EOF
  install -m 600 "$TEMP" "$UNIT"
  systemctl --user daemon-reload
  systemctl --user enable --now sovereign-hostd.service
else
  echo "error: unsupported host operating system" >&2
  exit 1
fi

echo "sovereign-hostd installed"
