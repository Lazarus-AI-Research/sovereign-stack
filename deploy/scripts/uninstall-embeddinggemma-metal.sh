#!/usr/bin/env bash
set -Eeuo pipefail

SOVEREIGN_HOME="${SOVEREIGN_HOME:-$HOME/.sovereign}"
LABEL_FILE="$SOVEREIGN_HOME/state/embeddinggemma-launchd-label"
[[ -s "$LABEL_FILE" ]] || exit 0
LABEL="$(<"$LABEL_FILE")"
[[ "$LABEL" == com.lazarus.sovereign-embeddinggemma.* ]] || {
  echo "warning: refusing invalid embeddinggemma launchd label: $LABEL" >&2
  exit 0
}

launchctl bootout "gui/$(id -u)/$LABEL" 2>/dev/null || true
rm -f "$HOME/Library/LaunchAgents/$LABEL.plist"
rm -f "$LABEL_FILE"
printf 'removed host embedding service %s\n' "$LABEL"
