#!/usr/bin/env bash
# Remove SovereignStack. Data and configuration are preserved unless the user
# crosses the explicit --purge --yes boundary.
set -Eeuo pipefail

SOVEREIGN_HOME="${SOVEREIGN_HOME:-$HOME/.sovereign}"
PURGE=0
CONFIRMED=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --purge) PURGE=1; shift ;;
    --yes) CONFIRMED=1; shift ;;
    *) echo "error: unknown option: $1" >&2; exit 2 ;;
  esac
done

die() { echo "error: $*" >&2; exit 1; }
[[ "$SOVEREIGN_HOME" != / && "$SOVEREIGN_HOME" != "$HOME" && -n "$SOVEREIGN_HOME" ]] || \
  die "refusing unsafe SOVEREIGN_HOME: $SOVEREIGN_HOME"
CURRENT="$SOVEREIGN_HOME/current"
ENV_FILE="$SOVEREIGN_HOME/.env"
PROFILE=""
[[ -f "$SOVEREIGN_HOME/state/profile" ]] && PROFILE="$(<"$SOVEREIGN_HOME/state/profile")"
case "$PROFILE" in
  cuda-x86_64) OVERLAY=cuda ;;
  metal-arm64) OVERLAY=metal ;;
  *) OVERLAY="" ;;
esac

ENGINE_LIB="$CURRENT/deploy/scripts/container-engine.sh"
ENGINE_SUPPORT=0
if [[ -n "$OVERLAY" && -d "$CURRENT/deploy" && -f "$ENV_FILE" && -r "$ENGINE_LIB" ]]; then
  # shellcheck source=container-engine.sh
  source "$ENGINE_LIB"
  ENGINE_SUPPORT=1
  COMPONENT_MANIFEST="$CURRENT/release/manifest.json"
  [[ -f "$COMPONENT_MANIFEST" ]] || COMPONENT_MANIFEST="$CURRENT/release/release-source.json"
  SOVEREIGN_ENGINE_MANIFEST="$COMPONENT_MANIFEST"
  export SOVEREIGN_ENGINE_MANIFEST
fi

purge_preview() {
  local volume provider="" managed_home="" managed_profile=""
  printf 'Permanent purge preview:\n'
  printf '  appliance files: %s\n' "$SOVEREIGN_HOME"
  if [[ -f "$CURRENT/deploy/compose/compose.yml" ]]; then
    while IFS= read -r volume; do
      printf '  Docker volume: sovereign-stack_%s\n' "$volume"
    done < <(awk '
      /^volumes:/ { inside=1; next }
      inside && /^[^[:space:]]/ { exit }
      inside && /^  [A-Za-z0-9_-]+:/ {
        value=$1; sub(":$", "", value); print value
      }
    ' "$CURRENT/deploy/compose/compose.yml")
  fi
  if (( ENGINE_SUPPORT == 1 )) && [[ -r "$(sovereign_engine_state_file)" ]]; then
    provider="$(sovereign_engine_state_value "$(sovereign_engine_state_file)" provider)"
    if [[ "$provider" == managed-colima ]]; then
      managed_home="$(sovereign_engine_state_value "$(sovereign_engine_state_file)" colima_home)"
      managed_profile="$(sovereign_engine_state_value "$(sovereign_engine_state_file)" colima_profile)"
      printf '  managed Colima VM: %s (COLIMA_HOME=%s)\n' "${managed_profile:-unknown}" "${managed_home:-unknown}"
    else
      printf '  container engine: preserved (%s is not installer-owned)\n' "${provider:-unknown}"
    fi
  fi
}

if (( PURGE == 1 )); then
  purge_preview
  if (( CONFIRMED != 1 )); then
    die "review the purge preview, then confirm with: sovereign uninstall --purge --yes"
  fi
fi

if (( ENGINE_SUPPORT == 1 )); then
  if sovereign_engine_require; then
    COMPOSE=(sovereign_engine_compose --project-directory "$SOVEREIGN_HOME" --env-file "$ENV_FILE"
      -f "$CURRENT/deploy/compose/compose.yml"
      -f "$CURRENT/deploy/compose/compose.runtime.${OVERLAY}.yml")
    if (( PURGE == 1 )); then
      "${COMPOSE[@]}" down -v --remove-orphans || true
    else
      "${COMPOSE[@]}" down --remove-orphans || true
    fi
  else
    echo "warning: container engine is unavailable; container data was left unchanged" >&2
  fi
fi

if [[ -x "$CURRENT/deploy/scripts/uninstall-hostd.sh" ]]; then
  SOVEREIGN_HOME="$SOVEREIGN_HOME" "$CURRENT/deploy/scripts/uninstall-hostd.sh"
fi

if [[ "$PROFILE" == metal-arm64 ]]; then
  if [[ -x "$CURRENT/deploy/scripts/uninstall-embeddinggemma-metal.sh" ]]; then
    SOVEREIGN_HOME="$SOVEREIGN_HOME" "$CURRENT/deploy/scripts/uninstall-embeddinggemma-metal.sh"
  fi
  AGENT_UNINSTALL=""
  if [[ -f "$ENV_FILE" ]]; then
    VERSION="$(sed -n 's/^SOVEREIGN_VERSION=//p' "$ENV_FILE" | tail -n 1)"
    [[ -n "$VERSION" ]] && AGENT_UNINSTALL="$SOVEREIGN_HOME/runtime-dist/$VERSION/agent-dist/uninstall-agent.sh"
  fi
  if [[ -x "$AGENT_UNINSTALL" ]]; then
    SOVEREIGN_AGENT_HOME="$SOVEREIGN_HOME" "$AGENT_UNINSTALL"
  elif [[ "$SOVEREIGN_HOME" == "$HOME/.sovereign" ]] && command -v launchctl >/dev/null 2>&1; then
    # The label is global to this login session. Only use the fallback for the
    # default install home; a custom home without its scoped uninstaller must
    # never remove another SovereignStack installation's host agent.
    launchctl bootout "gui/$(id -u)/com.lazarus.sovereign-runtime-agent" 2>/dev/null || true
    rm -f "$HOME/Library/LaunchAgents/com.lazarus.sovereign-runtime-agent.plist"
  elif [[ ! -x "$AGENT_UNINSTALL" ]]; then
    echo "warning: Metal agent uninstaller is unavailable; host agent was left unchanged" >&2
  fi
fi

BIN_DIR="$HOME/.local/bin"
[[ -s "$SOVEREIGN_HOME/state/bin-dir" ]] && BIN_DIR="$(<"$SOVEREIGN_HOME/state/bin-dir")"
CLI="$BIN_DIR/sovereign"
if [[ -f "$CLI" ]] && cmp -s "$CLI" "$CURRENT/deploy/scripts/sovereign" 2>/dev/null; then
  rm -f "$CLI"
fi

if (( PURGE == 1 )); then
  if (( ENGINE_SUPPORT == 1 )); then
    sovereign_engine_purge_managed || die \
      "the installer-owned Colima VM could not be removed; appliance files were left in place"
  fi
  rm -rf "$SOVEREIGN_HOME"
  echo "SovereignStack and all local appliance data were purged."
else
  if (( ENGINE_SUPPORT == 1 )); then
    sovereign_engine_stop_managed || \
      echo "warning: managed Colima could not be stopped; it was preserved" >&2
  fi
  rm -f "$CURRENT"
  rm -rf "$SOVEREIGN_HOME/releases"
  mkdir -p "$SOVEREIGN_HOME/state"
  printf 'uninstalled_at=%s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" > "$SOVEREIGN_HOME/state/uninstalled"
  echo "SovereignStack was removed; configuration, models, backups, and Docker volumes were preserved."
  echo "Re-run the installer to attach the preserved data."
fi
