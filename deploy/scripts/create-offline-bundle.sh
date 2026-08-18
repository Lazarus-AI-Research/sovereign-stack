#!/usr/bin/env bash
# Create a checksum-verifiable, same-platform SovereignStack offline bundle.
# Model weights are opt-in because they can make the archive very large.
set -Eeuo pipefail

SOVEREIGN_HOME="${SOVEREIGN_HOME:-$HOME/.sovereign}"
CURRENT="$SOVEREIGN_HOME/current"
ENV_FILE="$SOVEREIGN_HOME/.env"
PROFILE=""
OUTPUT=""
PULL_MISSING=1
INCLUDE_MODELS=()

usage() {
  cat <<'EOF'
Usage: create-offline-bundle.sh [options]

  --profile cuda-x86_64|metal-arm64
  --include-model <id>       Repeatable; accepts all, assistant-large,
                             embedding-gemma-default
  --include-models           Include the complete local model cache
  --output <archive.tar.gz>  Destination (default: ~/.sovereign/bundles/...)
  --no-pull                  Fail instead of pulling a missing image
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --profile) PROFILE="$2"; shift 2 ;;
    --include-model) INCLUDE_MODELS+=("$2"); shift 2 ;;
    --include-models) INCLUDE_MODELS+=(all); shift ;;
    --output) OUTPUT="$2"; shift 2 ;;
    --no-pull) PULL_MISSING=0; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "error: unknown option: $1" >&2; usage >&2; exit 2 ;;
  esac
done

die() { echo "error: $*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "$1 is required"; }
sha256() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | awk '{print $1}'
  else shasum -a 256 "$1" | awk '{print $1}'; fi
}
file_size() {
  if stat -f %z "$1" >/dev/null 2>&1; then stat -f %z "$1"
  else stat -c %s "$1"; fi
}
env_value() { sed -n "s/^$1=//p" "$ENV_FILE" | tail -n 1 | sed 's/^"//;s/"$//'; }
json_escape() {
  printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g; s/	/\\t/g'
}
artifact_json() {
  local name="$1" source="$2" hash="$3" bytes="$4"
  printf '{"name":"%s","source":"%s","sha256":"%s","bytes":%s}' \
    "$(json_escape "$name")" "$(json_escape "$source")" "$hash" "$bytes"
}

for command in tar openssl sed awk; do need "$command"; done
[[ -d "$CURRENT/deploy" && -f "$ENV_FILE" ]] || die "SovereignStack is not installed at $SOVEREIGN_HOME"
ENGINE_LIB="$CURRENT/deploy/scripts/container-engine.sh"
[[ -r "$ENGINE_LIB" ]] || die "container-engine support is missing from the installed release"
# shellcheck source=container-engine.sh
source "$ENGINE_LIB"
sovereign_engine_require

INSTALLED_PROFILE="$(<"$SOVEREIGN_HOME/state/profile")"
PROFILE="${PROFILE:-$INSTALLED_PROFILE}"
[[ "$PROFILE" == cuda-x86_64 || "$PROFILE" == metal-arm64 ]] || die "unsupported profile $PROFILE"
[[ "$PROFILE" == "$INSTALLED_PROFILE" ]] || die "same-platform bundles only: installed profile is $INSTALLED_PROFILE"

VERSION="$(env_value SOVEREIGN_VERSION)"
[[ -n "$VERSION" ]] || VERSION="$(<"$CURRENT/VERSION")"
ARCHITECTURE=amd64
[[ "$PROFILE" == metal-arm64 ]] && ARCHITECTURE=arm64
mkdir -p "$SOVEREIGN_HOME/bundles"
OUTPUT="${OUTPUT:-$SOVEREIGN_HOME/bundles/sovereign-offline-$VERSION-$PROFILE.tar.gz}"
case "$OUTPUT" in /*) ;; *) OUTPUT="$PWD/$OUTPUT" ;; esac
mkdir -p "$(dirname "$OUTPUT")"

TMP_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/sovereign-bundle.XXXXXX")"
trap 'rm -rf "$TMP_ROOT"' EXIT
STAGE="$TMP_ROOT/stage"
mkdir -p "$STAGE"

printf '%s\n' "$PROFILE" > "$STAGE/profile"
printf '%s\n' "$VERSION" > "$STAGE/version"
tar -czf "$STAGE/release.tar.gz" -C "$CURRENT" .

IMAGE_KEYS=(
  SOVEREIGN_CONTROL_IMAGE SOVEREIGN_DOCKER_PROXY_IMAGE SOVEREIGN_EVALS_IMAGE
  SOVEREIGN_WORKSPACE_IMAGE SOVEREIGN_RUNTIME_IMAGE CADDY_IMAGE LITELLM_IMAGE
  PGVECTOR_IMAGE PHOENIX_IMAGE PROMETHEUS_IMAGE GRAFANA_IMAGE LOKI_IMAGE OTEL_IMAGE
)
[[ "$PROFILE" == cuda-x86_64 ]] && IMAGE_KEYS+=(SOVEREIGN_EMBEDDINGS_IMAGE)
IMAGE_REFS=()
IMAGE_JSON="$TMP_ROOT/images.jsonl"
: > "$IMAGE_JSON"
for key in "${IMAGE_KEYS[@]}"; do
  ref="$(env_value "$key")"
  [[ -n "$ref" ]] || die "$key is missing from $ENV_FILE"
  if ! sovereign_engine_docker image inspect "$ref" >/dev/null 2>&1; then
    (( PULL_MISSING == 1 )) || die "image is not present locally: $ref"
    echo "pulling $ref" >&2
    sovereign_engine_docker pull "$ref" >/dev/null
  fi
  inspect="$(sovereign_engine_docker image inspect --format '{{.Id}} {{.Size}}' "$ref")"
  image_hash="${inspect%% *}"; image_hash="${image_hash#sha256:}"
  image_bytes="${inspect##* }"
  artifact_json "$key" "$ref" "$image_hash" "$image_bytes" >> "$IMAGE_JSON"
  printf '\n' >> "$IMAGE_JSON"
  IMAGE_REFS+=("$ref")
done

echo "saving ${#IMAGE_REFS[@]} images (this can take several minutes)..." >&2
sovereign_engine_docker save -o "$STAGE/images.tar" "${IMAGE_REFS[@]}"

MODEL_JSON="$TMP_ROOT/models.jsonl"
: > "$MODEL_JSON"
if (( ${#INCLUDE_MODELS[@]} > 0 )); then
  MODEL_ROOT="$SOVEREIGN_HOME/models"
  [[ -d "$MODEL_ROOT" ]] || die "model directory does not exist: $MODEL_ROOT"
  MODEL_PATHS=()
  for model in "${INCLUDE_MODELS[@]}"; do
    case "$model" in
      all)
        MODEL_PATHS=(.)
        break
        ;;
      assistant-large)
        if [[ "$PROFILE" == metal-arm64 ]]; then
          MODEL_PATHS+=(metal/gemma-4-E2B_q4_0-it.gguf metal/gemma-4-E2B-it-mmproj.gguf)
        else
          MODEL_PATHS+=(hf/hub/models--google--gemma-4-E2B-it)
        fi
        ;;
      embedding-gemma-default)
        MODEL_PATHS+=(embeddinggemma/embeddinggemma-300M-qat-Q4_0.gguf)
        ;;
      *)
        [[ "$model" != /* && "$model" != *..* && -e "$MODEL_ROOT/$model" ]] || \
          die "unknown model id or cache path: $model"
        MODEL_PATHS+=("$model")
        ;;
    esac
  done
  for path in "${MODEL_PATHS[@]}"; do
    [[ -e "$MODEL_ROOT/$path" ]] || die "requested model is not cached: $path"
  done
  tar -czf "$STAGE/weights.tar.gz" -C "$MODEL_ROOT" "${MODEL_PATHS[@]}"
  weights_hash="$(sha256 "$STAGE/weights.tar.gz")"
  weights_bytes="$(file_size "$STAGE/weights.tar.gz")"
  for model in "${INCLUDE_MODELS[@]}"; do
    artifact_json "$model" weights.tar.gz "$weights_hash" "$weights_bytes" >> "$MODEL_JSON"
    printf '\n' >> "$MODEL_JSON"
  done
fi

if [[ "$PROFILE" == metal-arm64 ]]; then
  AGENT_ROOT="$SOVEREIGN_HOME/runtime-dist/$VERSION"
  [[ -x "$AGENT_ROOT/agent-dist/install-agent.sh" ]] || \
    die "Metal agent distribution is missing: $AGENT_ROOT"
  tar -czf "$STAGE/metal-agent.tar.gz" -C "$AGENT_ROOT" .
fi

# Generate the manifest after all payload artifacts exist. Artifact names and
# references are constrained above, so this does not require jq or Python on
# an appliance host.
CREATED_AT="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
BUNDLE_ID="$VERSION-$PROFILE-$(openssl rand -hex 6)"
{
  printf '{\n  "schema_version": "1.0",\n'
  printf '  "bundle_id": "%s",\n' "$BUNDLE_ID"
  printf '  "version": "%s",\n' "$VERSION"
  printf '  "profile": "%s",\n' "$PROFILE"
  printf '  "architecture": "%s",\n' "$ARCHITECTURE"
  printf '  "created_at": "%s",\n' "$CREATED_AT"
  if [[ -f "$STAGE/weights.tar.gz" ]]; then
    printf '  "includes_weights": true,\n'
  else
    printf '  "includes_weights": false,\n'
  fi
  printf '  "images": [\n'
  awk 'NF {if (seen++) printf ",\n"; printf "    %s", $0} END {printf "\n"}' "$IMAGE_JSON"
  printf '  ],\n  "models": [\n'
  awk 'NF {if (seen++) printf ",\n"; printf "    %s", $0} END {printf "\n"}' "$MODEL_JSON"
  printf '  ],\n  "files": [\n'
  first=1
  for file in "$STAGE/release.tar.gz" "$STAGE/images.tar" "$STAGE/weights.tar.gz" "$STAGE/metal-agent.tar.gz"; do
    [[ -f "$file" ]] || continue
    (( first == 1 )) || printf ',\n'
    first=0
    printf '    %s' "$(artifact_json "$(basename "$file")" "$(basename "$file")" "$(sha256 "$file")" "$(file_size "$file")")"
  done
  printf '\n  ]\n}\n'
} > "$STAGE/manifest.json"

(cd "$STAGE" && {
  find . -type f ! -name checksums.sha256 -print | LC_ALL=C sort | while IFS= read -r file; do
    printf '%s  %s\n' "$(sha256 "$file")" "${file#./}"
  done
} > checksums.sha256)

rm -f "$OUTPUT.tmp"
tar -czf "$OUTPUT.tmp" -C "$STAGE" .
mv "$OUTPUT.tmp" "$OUTPUT"
cp "$STAGE/manifest.json" "$OUTPUT.json"
printf 'created %s\n' "$OUTPUT"
printf 'sha256 %s\n' "$(sha256 "$OUTPUT")"
