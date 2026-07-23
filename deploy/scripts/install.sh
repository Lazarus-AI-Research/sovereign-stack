#!/usr/bin/env bash
# Noninteractive SovereignStack v0.1 installer. It installs product assets and
# starts the appliance; host drivers and container tooling remain prerequisites.
set -Eeuo pipefail

DEFAULT_VERSION="0.1.0-rc.3"
VERSION="${SOVEREIGN_VERSION:-$DEFAULT_VERSION}"
VERSION="${VERSION#v}"
SOVEREIGN_HOME="${SOVEREIGN_HOME:-$HOME/.sovereign}"
BIN_DIR="${SOVEREIGN_BIN_DIR:-$HOME/.local/bin}"
PROFILE="${SOVEREIGN_PROFILE:-}"
REPOSITORY="${SOVEREIGN_GITHUB_REPOSITORY:-Lazarus-AI-Research/sovereign-stack}"
RELEASE_URL="${SOVEREIGN_RELEASE_URL:-}"
OFFLINE_BUNDLE=""
OFFLINE_MODE=0
EMBEDDINGGEMMA_VERSION=v0.3.1
EMBEDDINGGEMMA_METAL_ASSET=embeddinggemma-darwin-arm64-metal
EMBEDDINGGEMMA_METAL_SHA256=c110806fcb22514c43bb237865340fec94d14d8de8466eeed7b5d288c58ce8b5
EMBEDDINGGEMMA_MODEL_REPOSITORY=ggml-org/embeddinggemma-300M-qat-q4_0-GGUF
EMBEDDINGGEMMA_MODEL_REVISION=8dd0ca2a66a8f14470acb0e2a71f801afbc5fb73
EMBEDDINGGEMMA_MODEL_ARTIFACT=embeddinggemma-300M-qat-Q4_0.gguf
EMBEDDINGGEMMA_MODEL_SHA256=50d28e22432a148f6f8a86eab3700f92add5d1f54baf7790675a2a4dadbccf26

while [[ $# -gt 0 ]]; do
  case "$1" in
    --version) VERSION="${2#v}"; shift 2 ;;
    --home) SOVEREIGN_HOME="$2"; shift 2 ;;
    --profile) PROFILE="$2"; shift 2 ;;
    --offline-bundle) OFFLINE_BUNDLE="$2"; shift 2 ;;
    *) echo "unknown option: $1" >&2; exit 2 ;;
  esac
done

RELEASE_URL="${RELEASE_URL:-https://github.com/$REPOSITORY/releases/download/v$VERSION}"

say() { printf '\n==> %s\n' "$*"; }
die() { echo "error: $*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "$1 is required"; }
sha256() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | awk '{print $1}'
  else shasum -a 256 "$1" | awk '{print $1}'; fi
}
verify_pair() {
  local file="$1" sums="$2" expected actual
  expected="$(awk -v name="$(basename "$file")" '$2==name || $2=="*"name {print $1; exit}' "$sums")"
  [[ -n "$expected" ]] || expected="$(awk 'NR==1 {print $1}' "$sums")"
  actual="$(sha256 "$file")"
  [[ "$actual" == "$expected" ]] || die "checksum mismatch for $(basename "$file")"
}
verify_archive_paths() {
  local archive="$1" entry
  while IFS= read -r entry; do
    entry="${entry#./}"
    [[ -z "$entry" ]] && continue
    [[ "$entry" != /* && "/$entry/" != *"/../"* ]] || \
      die "unsafe archive entry in $(basename "$archive"): $entry"
  done < <(tar -tzf "$archive")
}

for command in curl tar openssl docker; do need "$command"; done
docker compose version >/dev/null 2>&1 || die "Docker Compose v2 is required"
docker info >/dev/null 2>&1 || die "Docker is not running"

SCRIPT_DIR=""
LOCAL_REPO=""
if [[ -n "${BASH_SOURCE[0]:-}" && -f "${BASH_SOURCE[0]}" ]]; then
  SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  LOCAL_REPO="$(cd "$SCRIPT_DIR/../.." 2>/dev/null && pwd || true)"
fi
SOURCE_DIR="${SOVEREIGN_SOURCE_DIR:-}"
if [[ -z "$SOURCE_DIR" && -n "$LOCAL_REPO" ]] &&
   [[ -f "$LOCAL_REPO/VERSION" && -f "$LOCAL_REPO/deploy/compose/compose.yml" ]]; then
  SOURCE_DIR="$LOCAL_REPO"
fi

if [[ -z "$PROFILE" ]]; then
  DETECTOR=""
  [[ -n "$SOURCE_DIR" ]] && DETECTOR="$SOURCE_DIR/deploy/scripts/detect-hardware.sh"
  [[ -z "$DETECTOR" && -n "$SCRIPT_DIR" && -x "$SCRIPT_DIR/detect-hardware.sh" ]] && \
    DETECTOR="$SCRIPT_DIR/detect-hardware.sh"
  if [[ -n "$DETECTOR" ]]; then
    PROFILE="$("$DETECTOR")"
  elif [[ "$(uname -s)-$(uname -m)" == Darwin-arm64 ]]; then
    PROFILE=metal-arm64
  elif [[ "$(uname -s)-$(uname -m)" == Linux-x86_64 ]]; then
    PROFILE=cuda-x86_64
  else
    die "unsupported platform"
  fi
fi
[[ "$PROFILE" == metal-arm64 || "$PROFILE" == cuda-x86_64 ]] || die "unsupported profile $PROFILE"

say "Checking $PROFILE prerequisites"
if [[ "$PROFILE" == metal-arm64 ]]; then
  [[ "$(uname -s)-$(uname -m)" == Darwin-arm64 ]] || die "metal-arm64 requires an Apple Silicon Mac"
  MEMORY="$(sysctl -n hw.memsize)"
  (( MEMORY >= 32 * 1024 * 1024 * 1024 )) || die "at least 32GB unified memory is required"
else
  [[ "$(uname -s)-$(uname -m)" == Linux-x86_64 ]] || die "cuda-x86_64 requires Ubuntu 24.04 x86_64"
  OS_RELEASE="${SOVEREIGN_OS_RELEASE:-/etc/os-release}"
  [[ -r "$OS_RELEASE" ]] || die "cannot read $OS_RELEASE"
  # Source OS metadata inside subshells: /etc/os-release defines VERSION,
  # which must never overwrite the SovereignStack release version.
  OS_ID="$(. "$OS_RELEASE" && printf '%s' "${ID:-}")"
  OS_VERSION_ID="$(. "$OS_RELEASE" && printf '%s' "${VERSION_ID:-}")"
  [[ "$OS_ID" == ubuntu && "$OS_VERSION_ID" == 24.04 ]] || die "CUDA v0.1.0 requires Ubuntu 24.04"
  need nvidia-smi
  VRAM="$(nvidia-smi --query-gpu=memory.total --format=csv,noheader,nounits -i 0 | head -n1 | tr -d ' ')"
  (( VRAM >= 24576 )) || die "GPU 0 must provide at least 24GB VRAM"
  docker info --format '{{json .Runtimes}}' | grep -qi nvidia || die "NVIDIA Container Toolkit is not configured for Docker"
fi

AVAILABLE_KB="$(df -Pk "$HOME" | awk 'NR==2 {print $4}')"
(( AVAILABLE_KB >= 20 * 1024 * 1024 )) || die "at least 20GB free disk space is required"

TMP_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/sovereign-install.XXXXXX")"
trap 'rm -rf "$TMP_ROOT"' EXIT
UNPACK="$TMP_ROOT/unpack"
mkdir -p "$UNPACK"

install_cosign() {
  local name checksum
  [[ -x "$TMP_ROOT/cosign" ]] && return 0
  case "$(uname -s)-$(uname -m)" in
    Darwin-arm64) name=cosign-darwin-arm64; checksum=94b42a9e697be95675f6160ab031a9a5f1ec1e646d6f648d7b2f5cd59ececbc5 ;;
    Linux-x86_64) name=cosign-linux-amd64; checksum=ae1ecd212663f3693ad9edf8b1a183900c9a52d3155ba6e354237f9a0f6463fc ;;
    *) die "no pinned cosign binary for this platform" ;;
  esac
  curl -fsSL --retry 4 -o "$TMP_ROOT/cosign" "https://github.com/sigstore/cosign/releases/download/v3.1.1/$name"
  [[ "$(sha256 "$TMP_ROOT/cosign")" == "$checksum" ]] || die "cosign checksum mismatch"
  chmod 700 "$TMP_ROOT/cosign"
}

if [[ -n "$OFFLINE_BUNDLE" ]]; then
  say "Verifying offline bundle"
  OFFLINE_MODE=1
  [[ -f "$OFFLINE_BUNDLE" ]] || die "offline bundle not found: $OFFLINE_BUNDLE"
  verify_archive_paths "$OFFLINE_BUNDLE"
  tar -xzf "$OFFLINE_BUNDLE" -C "$UNPACK"
  [[ -f "$UNPACK/checksums.sha256" ]] || die "offline bundle has no checksum manifest"
  (cd "$UNPACK" && if command -v sha256sum >/dev/null 2>&1; then sha256sum -c checksums.sha256; else shasum -a 256 -c checksums.sha256; fi)
  [[ -f "$UNPACK/manifest.json" && -f "$UNPACK/profile" && -f "$UNPACK/version" ]] || \
    die "offline bundle metadata is incomplete"
  [[ "$(<"$UNPACK/profile")" == "$PROFILE" ]] || die "offline bundle is for $(<"$UNPACK/profile"), not $PROFILE"
  [[ "$(<"$UNPACK/version")" == "$VERSION" ]] || die "offline bundle version $(<"$UNPACK/version") does not match requested $VERSION"
  [[ -f "$UNPACK/images.tar" ]] && docker load -i "$UNPACK/images.tar" >/dev/null
  if [[ -f "$UNPACK/release.tar.gz" ]]; then
    mkdir -p "$UNPACK/release"
    verify_archive_paths "$UNPACK/release.tar.gz"
    tar -xzf "$UNPACK/release.tar.gz" -C "$UNPACK/release"
  fi
  SOURCE_DIR="$UNPACK/release"
  if [[ -f "$UNPACK/weights.tar.gz" ]]; then
    mkdir -p "$SOVEREIGN_HOME/models"
    verify_archive_paths "$UNPACK/weights.tar.gz"
    tar -xzf "$UNPACK/weights.tar.gz" -C "$SOVEREIGN_HOME/models"
  elif [[ -d "$UNPACK/weights" ]]; then
    mkdir -p "$SOVEREIGN_HOME/models"
    cp -R "$UNPACK/weights/." "$SOVEREIGN_HOME/models/"
  fi
  if [[ -f "$UNPACK/metal-agent.tar.gz" ]]; then
    mkdir -p "$SOVEREIGN_HOME/runtime-dist/$VERSION"
    verify_archive_paths "$UNPACK/metal-agent.tar.gz"
    tar -xzf "$UNPACK/metal-agent.tar.gz" -C "$SOVEREIGN_HOME/runtime-dist/$VERSION"
  elif [[ -d "$UNPACK/metal-agent" ]]; then
    mkdir -p "$SOVEREIGN_HOME/runtime-dist/$VERSION"
    cp -R "$UNPACK/metal-agent/." "$SOVEREIGN_HOME/runtime-dist/$VERSION/"
  fi
elif [[ -z "$SOURCE_DIR" ]]; then
  say "Downloading SovereignStack $VERSION"
  ARCHIVE="sovereign-stack-$VERSION.tar.gz"
  curl -fsSL --retry 4 -o "$TMP_ROOT/$ARCHIVE" "$RELEASE_URL/$ARCHIVE"
  curl -fsSL --retry 4 -o "$TMP_ROOT/$ARCHIVE.sha256" "$RELEASE_URL/$ARCHIVE.sha256"
  verify_pair "$TMP_ROOT/$ARCHIVE" "$TMP_ROOT/$ARCHIVE.sha256"
  curl -fsSL --retry 2 -o "$TMP_ROOT/$ARCHIVE.sigstore.json" "$RELEASE_URL/$ARCHIVE.sigstore.json" || die "release signature bundle is missing"
  install_cosign
  "$TMP_ROOT/cosign" verify-blob \
    --bundle "$TMP_ROOT/$ARCHIVE.sigstore.json" \
    --certificate-identity-regexp "^https://github.com/$REPOSITORY/.github/workflows/release.yml@refs/tags/v" \
    --certificate-oidc-issuer https://token.actions.githubusercontent.com \
    "$TMP_ROOT/$ARCHIVE" >/dev/null
  verify_archive_paths "$TMP_ROOT/$ARCHIVE"
  tar -xzf "$TMP_ROOT/$ARCHIVE" -C "$UNPACK"
  SOURCE_DIR="$(find "$UNPACK" -maxdepth 4 -type f -path '*/deploy/compose/compose.yml' -print -quit | sed 's|/deploy/compose/compose.yml$||')"
fi

[[ -n "$SOURCE_DIR" && -f "$SOURCE_DIR/deploy/compose/compose.yml" ]] || die "release deployment files are missing"

say "Installing release assets"
RELEASES="$SOVEREIGN_HOME/releases"
TARGET="$RELEASES/$VERSION"
STAGING="$RELEASES/.${VERSION}.tmp.$$"
mkdir -p "$RELEASES"
rm -rf "$STAGING"
mkdir -p "$STAGING"
cp -R "$SOURCE_DIR/deploy" "$STAGING/deploy"
rm -rf "$STAGING/deploy/.env" "$STAGING/deploy/models" "$STAGING/deploy/logs" \
  "$STAGING/deploy/reports" "$STAGING/deploy/backups" "$STAGING/deploy/bundles" \
  "$STAGING/deploy/secrets"
for item in VERSION LICENSE NOTICE THIRD_PARTY_NOTICES.md schemas docs api release; do
  [[ -e "$SOURCE_DIR/$item" ]] && cp -R "$SOURCE_DIR/$item" "$STAGING/$item"
done
rm -rf "$TARGET"
mv "$STAGING" "$TARGET"
ln -sfn "$TARGET" "$SOVEREIGN_HOME/current"

mkdir -p "$SOVEREIGN_HOME/config" "$SOVEREIGN_HOME/branding" "$SOVEREIGN_HOME/models" \
  "$SOVEREIGN_HOME/logs/docker-proxy" "$SOVEREIGN_HOME/reports" "$SOVEREIGN_HOME/backups" \
  "$SOVEREIGN_HOME/bundles" "$SOVEREIGN_HOME/secrets/litellm"
FIRST_CONFIG=false
if [[ ! -f "$SOVEREIGN_HOME/config/branding.yaml" ]]; then
  FIRST_CONFIG=true
  cp -R "$TARGET/deploy/config/." "$SOVEREIGN_HOME/config/"
  cp -R "$TARGET/deploy/branding/." "$SOVEREIGN_HOME/branding/"
fi
if $FIRST_CONFIG; then
  if [[ "$PROFILE" == metal-arm64 ]]; then
    cp "$TARGET/deploy/config/runtime.metal.yaml" "$SOVEREIGN_HOME/config/runtime.yaml"
    cp "$TARGET/deploy/config/embedding-profiles.metal.yaml" "$SOVEREIGN_HOME/config/embedding-profiles.yaml"
    cp "$TARGET/deploy/config/model-registry.metal.yaml" "$SOVEREIGN_HOME/config/model-registry.yaml"
  else
    cp "$TARGET/deploy/config/runtime.yaml" "$SOVEREIGN_HOME/config/runtime.yaml"
  fi
else
  SOVEREIGN_HOME="$SOVEREIGN_HOME" SOVEREIGN_PROFILE="$PROFILE" \
  SOVEREIGN_RELEASE_ROOT="$TARGET" \
    "$TARGET/deploy/scripts/migrate-embeddinggemma.sh"
fi

SOVEREIGN_HOME="$SOVEREIGN_HOME" SOVEREIGN_PROFILE="$PROFILE" SOVEREIGN_VERSION="$VERSION" \
  SOVEREIGN_RELEASE_ROOT="$TARGET" "$TARGET/deploy/scripts/generate-config.sh"
if (( OFFLINE_MODE == 1 )); then
  : > "$SOVEREIGN_HOME/state/offline"
else
  rm -f "$SOVEREIGN_HOME/state/offline"
fi

mkdir -p "$BIN_DIR"
install -m 755 "$TARGET/deploy/scripts/sovereign" "$BIN_DIR/sovereign"

download_hf() {
  local repo="$1" revision="$2" file="$3" expected="$4" destination="$5" url
  [[ -f "$destination" && "$(sha256 "$destination")" == "$expected" ]] && return 0
  mkdir -p "$(dirname "$destination")"
  url="https://huggingface.co/$repo/resolve/$revision/$file?download=true"
  # macOS ships Bash 3.2, where expanding an empty array under `set -u`
  # raises an unbound-variable error. Keep the authenticated and anonymous
  # invocations explicit so the one-command Metal installer works there.
  if [[ -n "${HF_TOKEN:-}" ]]; then
    if ! curl -fL --retry 5 -C - -H "Authorization: Bearer $HF_TOKEN" -o "$destination.part" "$url"; then
      rm -f "$destination.part"
      curl -fL --retry 5 -H "Authorization: Bearer $HF_TOKEN" -o "$destination.part" "$url" || \
        die "download failed for $repo/$file; set HF_TOKEN if the repository is gated"
    fi
  elif ! curl -fL --retry 5 -C - -o "$destination.part" "$url"; then
    rm -f "$destination.part"
    curl -fL --retry 5 -o "$destination.part" "$url" || \
      die "download failed for $repo/$file; set HF_TOKEN if the repository is gated"
  fi
  [[ "$(sha256 "$destination.part")" == "$expected" ]] || die "model checksum mismatch: $file"
  mv "$destination.part" "$destination"
}

if [[ "${SOVEREIGN_INCLUDE_MODELS:-1}" != 0 ]]; then
  say "Installing pinned EmbeddingGemma model"
  EMBEDDING_MODEL_DIR="$SOVEREIGN_HOME/models/embeddinggemma"
  EMBEDDING_MODEL="$EMBEDDING_MODEL_DIR/$EMBEDDINGGEMMA_MODEL_ARTIFACT"
  if (( OFFLINE_MODE == 1 )); then
    [[ -f "$EMBEDDING_MODEL" ]] || die "offline bundle does not contain embedding-gemma-default weights"
    [[ "$(sha256 "$EMBEDDING_MODEL")" == "$EMBEDDINGGEMMA_MODEL_SHA256" ]] || \
      die "offline embedding-gemma-default weights failed checksum verification"
  else
    download_hf "$EMBEDDINGGEMMA_MODEL_REPOSITORY" "$EMBEDDINGGEMMA_MODEL_REVISION" \
      "$EMBEDDINGGEMMA_MODEL_ARTIFACT" "$EMBEDDINGGEMMA_MODEL_SHA256" "$EMBEDDING_MODEL"
  fi
fi

if [[ "$PROFILE" == metal-arm64 && "${SOVEREIGN_INCLUDE_MODELS:-1}" != 0 ]]; then
  say "Installing pinned Metal runtime and models"
  AGENT_DIST="$SOVEREIGN_HOME/runtime-dist/$VERSION"
  if [[ ! -x "$AGENT_DIST/agent-dist/install-agent.sh" ]]; then
    (( OFFLINE_MODE == 0 )) || die "offline bundle does not contain the Metal runtime agent"
    AGENT_ARCHIVE="sovereign-metal-agent-$VERSION-arm64.tar.gz"
    RUNTIME_URL="${SOVEREIGN_RUNTIME_RELEASE_URL:-https://github.com/Lazarus-AI-Research/sovereign-vllm/releases/download/v$VERSION}"
    curl -fsSL --retry 4 -o "$TMP_ROOT/$AGENT_ARCHIVE" "$RUNTIME_URL/$AGENT_ARCHIVE"
    curl -fsSL --retry 4 -o "$TMP_ROOT/$AGENT_ARCHIVE.sha256" "$RUNTIME_URL/$AGENT_ARCHIVE.sha256"
    verify_pair "$TMP_ROOT/$AGENT_ARCHIVE" "$TMP_ROOT/$AGENT_ARCHIVE.sha256"
    curl -fsSL --retry 2 -o "$TMP_ROOT/$AGENT_ARCHIVE.sigstore.json" "$RUNTIME_URL/$AGENT_ARCHIVE.sigstore.json" || \
      die "Metal agent signature bundle is missing"
    install_cosign
    "$TMP_ROOT/cosign" verify-blob \
      --bundle "$TMP_ROOT/$AGENT_ARCHIVE.sigstore.json" \
      --certificate-identity-regexp '^https://github.com/Lazarus-AI-Research/sovereign-vllm/.github/workflows/release.yml@refs/tags/v' \
      --certificate-oidc-issuer https://token.actions.githubusercontent.com \
      "$TMP_ROOT/$AGENT_ARCHIVE" >/dev/null
    mkdir -p "$AGENT_DIST"
    tar -xzf "$TMP_ROOT/$AGENT_ARCHIVE" -C "$AGENT_DIST"
  fi
  MODELS="$SOVEREIGN_HOME/models/metal"
  if (( OFFLINE_MODE == 1 )); then
    [[ -f "$MODELS/gemma-4-E2B_q4_0-it.gguf" ]] || die "offline bundle does not contain assistant-large weights"
    [[ -f "$MODELS/gemma-4-E2B-it-mmproj.gguf" ]] || die "offline bundle does not contain the Gemma multimodal projector"
  fi
  download_hf google/gemma-4-E2B-it-qat-q4_0-gguf 69536a21d70340464240401ba38223d805f6a709 \
    gemma-4-E2B_q4_0-it.gguf 3646b4c147cd235a44d91df1546d3b7d8e29b547dbe4e1f80856419aa455e6fd "$MODELS/gemma-4-E2B_q4_0-it.gguf"
  download_hf google/gemma-4-E2B-it-qat-q4_0-gguf 69536a21d70340464240401ba38223d805f6a709 \
    gemma-4-E2B-it-mmproj.gguf 58c187648007cab392bd5678b87e862c3e8794017deb945feea2cf256195e96a "$MODELS/gemma-4-E2B-it-mmproj.gguf"
  SOVEREIGN_AGENT_HOME="$SOVEREIGN_HOME" "$AGENT_DIST/agent-dist/install-agent.sh"

  EMBEDDING_DIST="$AGENT_DIST/embeddinggemma"
  EMBEDDING_BINARY="$EMBEDDING_DIST/embeddinggemma"
  if [[ ! -x "$EMBEDDING_BINARY" ]]; then
    (( OFFLINE_MODE == 0 )) || die "offline bundle does not contain the embeddinggemma Metal binary"
    mkdir -p "$EMBEDDING_DIST"
    if [[ -f "$TARGET/deploy/assets/$EMBEDDINGGEMMA_METAL_ASSET" ]]; then
      cp "$TARGET/deploy/assets/$EMBEDDINGGEMMA_METAL_ASSET" "$EMBEDDING_BINARY.part"
    else
      # Source-tree developer installs do not carry release binaries. Public
      # release archives vendor this exact file and are Sigstore-verified above.
      curl -fsSL --retry 4 -o "$EMBEDDING_BINARY.part" \
        "https://github.com/QuixiAI/embeddinggemma.c/releases/download/$EMBEDDINGGEMMA_VERSION/$EMBEDDINGGEMMA_METAL_ASSET"
    fi
    [[ "$(sha256 "$EMBEDDING_BINARY.part")" == "$EMBEDDINGGEMMA_METAL_SHA256" ]] || \
      die "embeddinggemma Metal binary checksum mismatch"
    mv "$EMBEDDING_BINARY.part" "$EMBEDDING_BINARY"
    chmod 755 "$EMBEDDING_BINARY"
  fi
  install -m 644 "$TARGET/deploy/assets/embeddinggemma.c.LICENSE" "$EMBEDDING_DIST/LICENSE"
  EMBEDDINGGEMMA_BINARY="$EMBEDDING_BINARY" \
  EMBEDDINGGEMMA_MODEL="$SOVEREIGN_HOME/models/embeddinggemma/$EMBEDDINGGEMMA_MODEL_ARTIFACT" \
  SOVEREIGN_HOME="$SOVEREIGN_HOME" \
    "$TARGET/deploy/scripts/install-embeddinggemma-metal.sh"
fi

if [[ "${SOVEREIGN_SKIP_START:-0}" != 1 ]]; then
  say "Starting SovereignStack"
  SOVEREIGN_HOME="$SOVEREIGN_HOME" "$BIN_DIR/sovereign" up
else
  say "Installation staged; start skipped by SOVEREIGN_SKIP_START=1"
fi

echo
echo "SovereignStack $VERSION installed for $PROFILE"
echo "URL: http://127.0.0.1:${SOVEREIGN_HTTP_PORT:-8880}/"
echo "Credentials: $SOVEREIGN_HOME/credentials"
