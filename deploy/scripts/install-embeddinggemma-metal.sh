#!/usr/bin/env bash
# Install the host-side Metal embedding service for Docker Desktop deployments.
set -Eeuo pipefail

SOVEREIGN_HOME="${SOVEREIGN_HOME:-$HOME/.sovereign}"
BINARY="${EMBEDDINGGEMMA_BINARY:?set EMBEDDINGGEMMA_BINARY}"
MODEL="${EMBEDDINGGEMMA_MODEL:?set EMBEDDINGGEMMA_MODEL}"

[[ -x "$BINARY" ]] || { echo "error: embeddinggemma binary is not executable: $BINARY" >&2; exit 1; }
[[ -f "$MODEL" ]] || { echo "error: embeddinggemma model is missing: $MODEL" >&2; exit 1; }
command -v launchctl >/dev/null 2>&1 || { echo "error: launchctl is required" >&2; exit 1; }
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LAUNCHD_HELPER="$SCRIPT_DIR/launchd-service.sh"
[[ -x "$LAUNCHD_HELPER" ]] || { echo "error: launchd service helper is missing" >&2; exit 1; }

case "$SOVEREIGN_HOME$BINARY$MODEL" in
  *$'\n'*|*$'\r'*) echo "error: embeddinggemma paths must not contain newlines" >&2; exit 1 ;;
esac

xml_escape() {
  printf '%s' "$1" | sed 's/&/\&amp;/g; s/</\&lt;/g; s/>/\&gt;/g; s/"/\&quot;/g; s/'"'"'/\&apos;/g'
}

mkdir -p "$SOVEREIGN_HOME/state" "$SOVEREIGN_HOME/logs/embeddinggemma"
LABEL_FILE="$SOVEREIGN_HOME/state/embeddinggemma-launchd-label"
if [[ -s "$LABEL_FILE" ]]; then
  LABEL="$(<"$LABEL_FILE")"
else
  INSTALL_ID="$(printf '%s' "$SOVEREIGN_HOME" | cksum | awk '{print $1}')"
  LABEL="com.lazarus.sovereign-embeddinggemma.$INSTALL_ID"
  printf '%s\n' "$LABEL" > "$LABEL_FILE"
  chmod 600 "$LABEL_FILE"
fi
[[ "$LABEL" == com.lazarus.sovereign-embeddinggemma.* ]] || {
  echo "error: invalid stored embeddinggemma launchd label" >&2
  exit 1
}

PLIST_DIR="$HOME/Library/LaunchAgents"
PLIST="$PLIST_DIR/$LABEL.plist"
mkdir -p "$PLIST_DIR"
TMP_PLIST="$PLIST.tmp.$$"
trap 'rm -f "$TMP_PLIST"' EXIT

cat > "$TMP_PLIST" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>$(xml_escape "$LABEL")</string>
  <key>ProgramArguments</key>
  <array>
    <string>$(xml_escape "$BINARY")</string>
    <string>--bind</string>
    <string>127.0.0.1</string>
    <string>--port</string>
    <string>42666</string>
    <string>--backend</string>
    <string>metal</string>
    <string>--model</string>
    <string>$(xml_escape "$MODEL")</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>ThrottleInterval</key>
  <integer>5</integer>
  <key>ProcessType</key>
  <string>Interactive</string>
  <key>StandardOutPath</key>
  <string>$(xml_escape "$SOVEREIGN_HOME/logs/embeddinggemma/stdout.log")</string>
  <key>StandardErrorPath</key>
  <string>$(xml_escape "$SOVEREIGN_HOME/logs/embeddinggemma/stderr.log")</string>
</dict>
</plist>
EOF

"$LAUNCHD_HELPER" install "$LABEL" "$TMP_PLIST" "$PLIST" http://127.0.0.1:42666/healthz
printf 'installed host embedding service %s\n' "$LABEL"
