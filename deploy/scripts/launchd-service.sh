#!/usr/bin/env bash
# Transactionally replace a per-user LaunchAgent with bounded registration
# retries and rollback. Callers provide a validated candidate plist; this
# helper owns the live plist and never leaves the previous service unloaded.
set -Eeuo pipefail

[[ "${1:-}" == install && $# -ge 4 ]] || {
  echo "usage: launchd-service.sh install <label> <candidate.plist> <live.plist> [health-url]" >&2
  exit 2
}

LABEL="$2"
CANDIDATE="$3"
LIVE_PLIST="$4"
HEALTH_URL="${5:-}"
DOMAIN="gui/$(id -u)"
SERVICE="$DOMAIN/$LABEL"
RETRIES="${SOVEREIGN_LAUNCHD_RETRIES:-5}"
HEALTH_TIMEOUT="${SOVEREIGN_LAUNCHD_HEALTH_TIMEOUT:-120}"
BACKUP="$LIVE_PLIST.previous.$$"
WAS_LOADED=0
HAD_PREVIOUS=0

command -v launchctl >/dev/null 2>&1 || { echo "error: launchctl is required" >&2; exit 1; }
[[ -f "$CANDIDATE" ]] || { echo "error: launchd candidate is missing: $CANDIDATE" >&2; exit 1; }
if command -v plutil >/dev/null 2>&1; then
  plutil -lint "$CANDIDATE" >/dev/null
fi
mkdir -p "$(dirname "$LIVE_PLIST")"
if [[ -f "$LIVE_PLIST" ]]; then
  cp -p "$LIVE_PLIST" "$BACKUP"
  HAD_PREVIOUS=1
fi
if launchctl print "$SERVICE" >/dev/null 2>&1; then
  WAS_LOADED=1
  launchctl bootout "$SERVICE"
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    launchctl print "$SERVICE" >/dev/null 2>&1 || break
    sleep 0.2
  done
fi

rollback() {
  local reason="$1"
  launchctl bootout "$SERVICE" >/dev/null 2>&1 || true
  if (( HAD_PREVIOUS == 1 )); then
    install -m 600 "$BACKUP" "$LIVE_PLIST"
    if (( WAS_LOADED == 1 )); then
      if launchctl bootstrap "$DOMAIN" "$LIVE_PLIST"; then
        launchctl kickstart -k "$SERVICE" || true
        echo "error: $reason; restored the previous $LABEL service" >&2
      else
        echo "error: $reason; restored the previous plist but launchd could not reload $LABEL" >&2
      fi
    else
      echo "error: $reason; restored the previous unloaded $LABEL plist" >&2
    fi
  else
    rm -f "$LIVE_PLIST"
    echo "error: $reason; removed the failed new $LABEL service" >&2
  fi
  rm -f "$BACKUP"
  exit 1
}

install -m 600 "$CANDIDATE" "$LIVE_PLIST"
registered=0
attempt=1
while (( attempt <= RETRIES )); do
  if launchctl bootstrap "$DOMAIN" "$LIVE_PLIST"; then
    registered=1
    break
  fi
  if (( attempt < RETRIES )); then
    echo "launchd registration for $LABEL failed (attempt $attempt of $RETRIES); retrying..." >&2
    sleep "$attempt"
  else
    echo "launchd registration for $LABEL failed (attempt $attempt of $RETRIES)" >&2
  fi
  attempt=$((attempt + 1))
done
(( registered == 1 )) || rollback "launchd could not register $LABEL after $RETRIES attempts"
launchctl kickstart -k "$SERVICE" || rollback "launchd could not start $LABEL"

if [[ -n "$HEALTH_URL" && "${SOVEREIGN_LAUNCHD_SKIP_HEALTH:-0}" != 1 ]]; then
  command -v curl >/dev/null 2>&1 || rollback "curl is required to verify $LABEL health"
  deadline=$((SECONDS + HEALTH_TIMEOUT))
  until curl -fsS --max-time 3 "$HEALTH_URL" >/dev/null 2>&1; do
    (( SECONDS < deadline )) || rollback "$LABEL did not become healthy at $HEALTH_URL"
    sleep 1
  done
fi

rm -f "$BACKUP"
printf 'installed launchd service %s\n' "$LABEL"
