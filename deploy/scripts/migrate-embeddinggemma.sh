#!/usr/bin/env bash
# Migrate an existing installation from runtime-hosted embeddings to the
# dedicated EmbeddingGemma service. Unrelated generation and remote model
# entries are preserved. Each changed file receives a one-time backup.
set -Eeuo pipefail

SOVEREIGN_HOME="${SOVEREIGN_HOME:-$HOME/.sovereign}"
SOVEREIGN_PROFILE="${SOVEREIGN_PROFILE:-}"
SOVEREIGN_RELEASE_ROOT="${SOVEREIGN_RELEASE_ROOT:-$SOVEREIGN_HOME/current}"

case "$SOVEREIGN_PROFILE" in
  cuda-x86_64)
    PROFILE_SOURCE="$SOVEREIGN_RELEASE_ROOT/deploy/config/embedding-profiles.yaml"
    REGISTRY_SOURCE="$SOVEREIGN_RELEASE_ROOT/deploy/config/model-registry.yaml"
    ;;
  metal-arm64)
    PROFILE_SOURCE="$SOVEREIGN_RELEASE_ROOT/deploy/config/embedding-profiles.metal.yaml"
    REGISTRY_SOURCE="$SOVEREIGN_RELEASE_ROOT/deploy/config/model-registry.metal.yaml"
    ;;
  *)
    echo "error: unsupported profile $SOVEREIGN_PROFILE" >&2
    exit 2
    ;;
esac

RUNTIME_CONFIG="$SOVEREIGN_HOME/config/runtime.yaml"
PROFILE_CONFIG="$SOVEREIGN_HOME/config/embedding-profiles.yaml"
MODEL_REGISTRY="$SOVEREIGN_HOME/config/model-registry.yaml"

for file in "$RUNTIME_CONFIG" "$PROFILE_CONFIG" "$MODEL_REGISTRY" \
  "$PROFILE_SOURCE" "$REGISTRY_SOURCE"; do
  [[ -f "$file" ]] || { echo "error: migration input is missing: $file" >&2; exit 1; }
done

backup_once() {
  local file="$1" backup="$1.pre-embeddinggemma"
  [[ -e "$backup" ]] || cp -p "$file" "$backup"
}

replace_file() {
  local source="$1" destination="$2"
  chmod --reference="$destination" "$source" 2>/dev/null || true
  mv "$source" "$destination"
}

TMP_RUNTIME="$RUNTIME_CONFIG.tmp.$$"
awk '
  BEGIN { in_embedding = 0; found = 0 }
  /^  embedding:[[:space:]]*$/ {
    print "  embedding:"
    print "    enabled: false"
    print "    task: embed"
    in_embedding = 1
    found = 1
    next
  }
  in_embedding && $0 !~ /^    / && $0 !~ /^[[:space:]]*$/ && $0 !~ /^[[:space:]]*#/ {
    in_embedding = 0
  }
  !in_embedding { print }
  END { if (!found) exit 42 }
' "$RUNTIME_CONFIG" > "$TMP_RUNTIME" || {
  status=$?
  rm -f "$TMP_RUNTIME"
  if [[ "$status" == 42 ]]; then
    echo "error: runtime config has no roles.embedding block" >&2
  fi
  exit "$status"
}

TMP_PROFILES="$PROFILE_CONFIG.tmp.$$"
awk '
  function flush() {
    if (block != "" && !drop) printf "%s", block
    block = ""
    drop = 0
  }
  /^  [A-Za-z0-9_.-]+:[[:space:]]*$/ {
    if (started) flush()
    started = 1
    block = $0 ORS
    key = $0
    sub(/^  /, "", key)
    sub(/:[[:space:]]*$/, "", key)
    drop = (key == "omni-default" || key == "text-compact" || key == "gemma-default")
    next
  }
  {
    if (!started) print
    else {
      block = block $0 ORS
      if ($0 ~ /LCO-Embedding\/LCO-Embedding-Omni-3B-2605/ ||
          $0 ~ /nomic-ai\/nomic-embed-text-v1.5/) drop = 1
    }
  }
  END { if (started) flush() }
' "$PROFILE_CONFIG" > "$TMP_PROFILES"
awk '
  function flush() {
    if (block != "" && wanted) printf "%s", block
    block = ""
    wanted = 0
  }
  /^  [A-Za-z0-9_.-]+:[[:space:]]*$/ {
    if (started) flush()
    started = 1
    block = $0 ORS
    key = $0
    sub(/^  /, "", key)
    sub(/:[[:space:]]*$/, "", key)
    wanted = (key == "gemma-default")
    next
  }
  { if (started) block = block $0 ORS }
  END { if (started) flush() }
' "$PROFILE_SOURCE" >> "$TMP_PROFILES"

TMP_REGISTRY="$MODEL_REGISTRY.tmp.$$"
awk '
  function flush() {
    if (block != "" && !drop) printf "%s", block
    block = ""
    drop = 0
  }
  /^  - [A-Za-z0-9_.-]+:/ {
    if (started) flush()
    started = 1
    block = $0 ORS
    drop = ($0 ~ /^  - id:[[:space:]]*(embedding-omni-default|embedding-text-compact|embedding-gemma-default)[[:space:]]*$/)
    next
  }
  {
    if (!started) print
    else {
      block = block $0 ORS
      if ($0 ~ /^    id:[[:space:]]*(embedding-omni-default|embedding-text-compact|embedding-gemma-default)[[:space:]]*$/ ||
          $0 ~ /LCO-Embedding\/LCO-Embedding-Omni-3B-2605/ ||
          $0 ~ /nomic-ai\/nomic-embed-text-v1.5/) drop = 1
    }
  }
  END { if (started) flush() }
' "$MODEL_REGISTRY" > "$TMP_REGISTRY"
awk '
  function flush() {
    if (block != "" && wanted) printf "%s", block
    block = ""
    wanted = 0
  }
  /^  - [A-Za-z0-9_.-]+:/ {
    if (started) flush()
    started = 1
    block = $0 ORS
    wanted = ($0 ~ /^  - id:[[:space:]]*embedding-gemma-default[[:space:]]*$/)
    next
  }
  {
    if (started) {
      block = block $0 ORS
      if ($0 ~ /^    id:[[:space:]]*embedding-gemma-default[[:space:]]*$/) wanted = 1
    }
  }
  END { if (started) flush() }
' "$REGISTRY_SOURCE" >> "$TMP_REGISTRY"

grep -q '^    enabled: false$' "$TMP_RUNTIME"
grep -q '^  gemma-default:$' "$TMP_PROFILES"
grep -q '^    provider: embeddinggemma$' "$TMP_PROFILES"
grep -q '^  - id: embedding-gemma-default$' "$TMP_REGISTRY"

for old in 'LCO-Embedding/LCO-Embedding-Omni-3B-2605' \
  'nomic-ai/nomic-embed-text-v1.5' 'embedding-omni-default' 'embedding-text-compact'; do
  if grep -Fq "$old" "$TMP_RUNTIME" "$TMP_PROFILES" "$TMP_REGISTRY"; then
    rm -f "$TMP_RUNTIME" "$TMP_PROFILES" "$TMP_REGISTRY"
    echo "error: retired embedding configuration remains after migration: $old" >&2
    exit 1
  fi
done

backup_once "$RUNTIME_CONFIG"
backup_once "$PROFILE_CONFIG"
backup_once "$MODEL_REGISTRY"
replace_file "$TMP_RUNTIME" "$RUNTIME_CONFIG"
replace_file "$TMP_PROFILES" "$PROFILE_CONFIG"
replace_file "$TMP_REGISTRY" "$MODEL_REGISTRY"

echo "migrated embedding configuration to embedding-gemma-default"
