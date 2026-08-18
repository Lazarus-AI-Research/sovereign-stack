#!/usr/bin/env bash
# SovereignStack installer. It provisions required runtime dependencies,
# installs product assets, and starts the appliance.
set -Eeuo pipefail

DEFAULT_VERSION="0.1.0-rc.6"
VERSION="${SOVEREIGN_VERSION:-$DEFAULT_VERSION}"
VERSION="${VERSION#v}"
SOVEREIGN_HOME="${SOVEREIGN_HOME:-$HOME/.sovereign}"
BIN_DIR="${SOVEREIGN_BIN_DIR:-$HOME/.local/bin}"
PROFILE="${SOVEREIGN_PROFILE:-}"
ACCESS_MODE="${SOVEREIGN_ACCESS_MODE:-}"
ACCESS_TARGET=""
REPOSITORY="${SOVEREIGN_GITHUB_REPOSITORY:-Lazarus-AI-Research/sovereign-stack}"
RELEASE_URL="${SOVEREIGN_RELEASE_URL:-}"
OFFLINE_BUNDLE=""
OFFLINE_MODE=0
RESUME_MODE=0
EMBEDDINGGEMMA_MODEL_REPOSITORY=ggml-org/embeddinggemma-300M-qat-q4_0-GGUF
EMBEDDINGGEMMA_MODEL_REVISION=8dd0ca2a66a8f14470acb0e2a71f801afbc5fb73
EMBEDDINGGEMMA_MODEL_ARTIFACT=embeddinggemma-300M-qat-Q4_0.gguf
EMBEDDINGGEMMA_MODEL_SHA256=50d28e22432a148f6f8a86eab3700f92add5d1f54baf7790675a2a4dadbccf26

while [[ $# -gt 0 ]]; do
  case "$1" in
    --version) VERSION="${2#v}"; shift 2 ;;
    --home) SOVEREIGN_HOME="$2"; shift 2 ;;
    --profile) PROFILE="$2"; shift 2 ;;
    --access) ACCESS_MODE="$2"; shift 2 ;;
    --domain) ACCESS_MODE=domain; ACCESS_TARGET="$2"; shift 2 ;;
    --offline-bundle) OFFLINE_BUNDLE="$2"; shift 2 ;;
    --resume) RESUME_MODE=1; shift ;;
    *) echo "unknown option: $1" >&2; exit 2 ;;
  esac
done

if [[ -n "$OFFLINE_BUNDLE" && "$OFFLINE_BUNDLE" != /* ]]; then
  OFFLINE_BUNDLE="$PWD/$OFFLINE_BUNDLE"
fi

RELEASE_URL="${RELEASE_URL:-https://github.com/$REPOSITORY/releases/download/v$VERSION}"

say() { printf '\n==> %s\n' "$*"; }
die() {
  if [[ -n "${SOVEREIGN_HOME:-}" && "$SOVEREIGN_HOME" != / && "$SOVEREIGN_HOME" != "$HOME" ]] &&
     declare -F journal_stage >/dev/null 2>&1; then
    journal_stage failed "$*" || true
  fi
  echo "error: $*" >&2
  exit 1
}
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
component_value() {
  local file="$1" component="$2" key="$3"
  awk -v component="$component" -v key="$key" '
    index($0, "\"" component "\"") && index($0, "{") { inside=1; next }
    inside && index($0, "\"" key "\"") {
      value=$0
      sub("^.*\"" key "\"[[:space:]]*:[[:space:]]*\"", "", value)
      sub("\".*$", "", value)
      print value
      exit
    }
    inside && $0 ~ /^[[:space:]]*},?[[:space:]]*$/ { exit }
  ' "$file"
}
component_number() {
  local file="$1" component="$2" key="$3"
  awk -v component="$component" -v key="$key" '
    index($0, "\"" component "\"") && index($0, "{") { inside=1; next }
    inside && index($0, "\"" key "\"") {
      value=$0
      sub("^.*\"" key "\"[[:space:]]*:[[:space:]]*", "", value)
      sub("[[:space:]]*,.*$", "", value)
      sub("[[:space:]]*$", "", value)
      print value
      exit
    }
    inside && $0 ~ /^[[:space:]]*},?[[:space:]]*$/ { exit }
  ' "$file"
}
file_bytes() {
  if stat -f %z "$1" >/dev/null 2>&1; then stat -f %z "$1"
  else stat -c %s "$1"; fi
}

[[ "$SOVEREIGN_HOME" != / && "$SOVEREIGN_HOME" != "$HOME" && -n "$SOVEREIGN_HOME" ]] || \
  die "refusing unsafe SOVEREIGN_HOME: $SOVEREIGN_HOME"
if (( EUID == 0 )); then
  die "run the installer as the login user who will own the appliance; it requests administrator approval only for host provisioning"
fi

journal_stage() {
  local stage="$1" detail="${2:-}" journal temporary
  [[ "$stage" =~ ^[a-z0-9-]+$ && "$detail" != *$'\n'* && "$detail" != *$'\r'* ]] || return 1
  journal="$SOVEREIGN_HOME/state/install-journal.env"
  mkdir -p "$SOVEREIGN_HOME/state"
  temporary="$journal.tmp.$$"
  umask 077
  {
    printf 'schema_version=1\n'
    printf 'version=%s\n' "$VERSION"
    printf 'profile=%s\n' "$PROFILE"
    printf 'stage=%s\n' "$stage"
    printf 'detail=%s\n' "$detail"
    printf 'updated_at=%s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
  } > "$temporary"
  chmod 600 "$temporary"
  mv "$temporary" "$journal"
}

run_privileged() {
  if command -v sudo >/dev/null 2>&1; then
    sudo "$@"
  elif command -v pkexec >/dev/null 2>&1; then
    pkexec "$@"
  else
    die "Ubuntu host setup needs one administrator approval, but neither sudo nor pkexec is available"
  fi
}

for command in curl tar openssl; do need "$command"; done

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
journal_stage preflight "checking $PROFILE host"
if [[ -z "$ACCESS_MODE" ]]; then
  # A loopback-only result is unusable over SSH. Default headless installs to
  # the private LAN; local desktop installs remain loopback-only.
  if [[ -n "${SSH_CONNECTION:-}" ]]; then ACCESS_MODE=lan; else ACCESS_MODE=desktop; fi
fi
[[ "$ACCESS_MODE" == desktop || "$ACCESS_MODE" == lan || "$ACCESS_MODE" == domain ]] || \
  die "--access must be desktop, lan, or domain"
if [[ "$ACCESS_MODE" == domain ]]; then
  [[ -n "$ACCESS_TARGET" ]] || die "domain access requires --domain <hostname>"
  SOVEREIGN_SITE_ADDRESS="$ACCESS_TARGET"
fi

say "Checking $PROFILE prerequisites"
HOST_MEMORY_BYTES=""
GPU_VRAM_MIB=""
GPU_NAME=""
GPU_INDEX=""
if [[ "$PROFILE" == metal-arm64 ]]; then
  [[ "$(uname -s)-$(uname -m)" == Darwin-arm64 ]] || die "metal-arm64 requires an Apple Silicon Mac"
  MEMORY="$(sysctl -n hw.memsize)"
  (( MEMORY >= 32 * 1024 * 1024 * 1024 )) || die "at least 32GB unified memory is required"
  HOST_MEMORY_BYTES="$MEMORY"
  GPU_NAME="Apple Silicon"
else
  [[ "$(uname -s)-$(uname -m)" == Linux-x86_64 ]] || die "cuda-x86_64 requires Ubuntu 24.04 x86_64"
  OS_RELEASE="${SOVEREIGN_OS_RELEASE:-/etc/os-release}"
  [[ -r "$OS_RELEASE" ]] || die "cannot read $OS_RELEASE"
  # Source OS metadata inside subshells: /etc/os-release defines VERSION,
  # which must never overwrite the SovereignStack release version.
  OS_ID="$(. "$OS_RELEASE" && printf '%s' "${ID:-}")"
  OS_VERSION_ID="$(. "$OS_RELEASE" && printf '%s' "${VERSION_ID:-}")"
  [[ "$OS_ID" == ubuntu && "$OS_VERSION_ID" == 24.04 ]] || die "CUDA v0.1.0 requires Ubuntu 24.04"
  if [[ -r /proc/meminfo ]]; then
    HOST_MEMORY_BYTES="$(( $(awk '/^MemTotal:/ {print $2; exit}' /proc/meminfo) * 1024 ))"
  elif command -v sysctl >/dev/null 2>&1; then
    HOST_MEMORY_BYTES="$(sysctl -n hw.memsize 2>/dev/null || printf 0)"
  else
    HOST_MEMORY_BYTES=0
  fi
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
  SOVEREIGN_COSIGN="$TMP_ROOT/cosign"
  export SOVEREIGN_COSIGN
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
  OFFLINE_IMAGES="$UNPACK/images.tar"
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
journal_stage source-verified "release $VERSION is available"

# Component versions and download locations are delegated by the reviewed,
# signed release manifest. Developer-source installs read the same fields from
# release-source.json so they exercise the release contract without pretending
# that the stack and Metal-agent versions must be identical.
COMPONENT_MANIFEST="$SOURCE_DIR/release/manifest.json"
[[ -f "$COMPONENT_MANIFEST" ]] || COMPONENT_MANIFEST="$SOURCE_DIR/release/release-source.json"
[[ -f "$COMPONENT_MANIFEST" ]] || die "release component manifest is missing"
METAL_AGENT_VERSION="$(component_value "$COMPONENT_MANIFEST" metal_agent version)"
METAL_AGENT_ARCHIVE="$(component_value "$COMPONENT_MANIFEST" metal_agent artifact)"
METAL_AGENT_URL="$(component_value "$COMPONENT_MANIFEST" metal_agent url)"
METAL_AGENT_SIGNATURE_URL="$(component_value "$COMPONENT_MANIFEST" metal_agent signature_url)"
METAL_AGENT_SHA256="$(component_value "$COMPONENT_MANIFEST" metal_agent sha256)"
METAL_AGENT_BYTES="$(component_number "$COMPONENT_MANIFEST" metal_agent bytes)"
METAL_AGENT_SIGNER="$(component_value "$COMPONENT_MANIFEST" metal_agent signer_identity_regexp)"
EMBEDDINGGEMMA_VERSION="$(component_value "$COMPONENT_MANIFEST" embedding_runtime version)"
EMBEDDINGGEMMA_METAL_ASSET="$(component_value "$COMPONENT_MANIFEST" embedding_runtime artifact)"
EMBEDDINGGEMMA_METAL_URL="$(component_value "$COMPONENT_MANIFEST" embedding_runtime url)"
EMBEDDINGGEMMA_METAL_SHA256="$(component_value "$COMPONENT_MANIFEST" embedding_runtime sha256)"
EMBEDDINGGEMMA_METAL_BYTES="$(component_number "$COMPONENT_MANIFEST" embedding_runtime bytes)"
if [[ "$PROFILE" == metal-arm64 ]]; then
  [[ -n "$METAL_AGENT_VERSION" && -n "$METAL_AGENT_ARCHIVE" && -n "$METAL_AGENT_URL" &&
     -n "$METAL_AGENT_SIGNATURE_URL" && "$METAL_AGENT_SHA256" =~ ^[0-9a-f]{64}$ &&
     "$METAL_AGENT_BYTES" =~ ^[1-9][0-9]*$ && -n "$METAL_AGENT_SIGNER" ]] || \
    die "release manifest has an invalid Metal-agent contract"
fi
[[ -n "$EMBEDDINGGEMMA_VERSION" && -n "$EMBEDDINGGEMMA_METAL_ASSET" &&
   -n "$EMBEDDINGGEMMA_METAL_URL" && "$EMBEDDINGGEMMA_METAL_SHA256" =~ ^[0-9a-f]{64}$ &&
   "$EMBEDDINGGEMMA_METAL_BYTES" =~ ^[1-9][0-9]*$ ]] || \
  die "release manifest has an invalid EmbeddingGemma contract"

UBUNTU_MANAGED_DOCKER=0
if [[ "$PROFILE" == cuda-x86_64 ]]; then
  HARDWARE_LIB="$SOURCE_DIR/deploy/scripts/hardware.sh"
  PROVISIONER="$SOURCE_DIR/deploy/scripts/provision-ubuntu.sh"
  [[ -r "$HARDWARE_LIB" && -r "$PROVISIONER" ]] || \
    die "release Ubuntu hardware provisioning support is missing"
  # shellcheck source=hardware.sh
  source "$HARDWARE_LIB"
  sovereign_has_nvidia_display_device || \
    die "no NVIDIA display or 3D controller was found on PCIe"

  ubuntu_stack_ready() {
    sovereign_nvidia_driver_ready &&
      command -v docker >/dev/null 2>&1 &&
      docker --context default info >/dev/null 2>&1 &&
      docker --context default compose version >/dev/null 2>&1 &&
      docker --context default info --format '{{json .Runtimes}}' 2>/dev/null | grep -qi nvidia
  }
  provision_result_value() {
    local file="$1" key="$2"
    awk -v key="$key" 'index($0, key "=") == 1 {value=substr($0, length(key) + 2)} END {print value}' "$file"
  }
  stage_reboot_resume() {
    local bootstrap staging resume_script unit_dir wants_dir install_id unit_name unit_file escaped_resume item
    bootstrap="$SOVEREIGN_HOME/bootstrap"
    staging="$bootstrap/.release.tmp.$$"
    resume_script="$bootstrap/resume.sh"
    mkdir -p "$bootstrap"
    rm -rf "$staging"
    mkdir -p "$staging"
    cp -R "$SOURCE_DIR/deploy" "$staging/deploy"
    for item in VERSION LICENSE NOTICE THIRD_PARTY_NOTICES.md schemas docs api release; do
      [[ -e "$SOURCE_DIR/$item" ]] && cp -R "$SOURCE_DIR/$item" "$staging/$item"
    done
    rm -rf "$bootstrap/release"
    mv "$staging" "$bootstrap/release"

    {
      printf '#!/usr/bin/env bash\nset -Eeuo pipefail\nexec env '
      printf 'SOVEREIGN_HOME=%q SOVEREIGN_BIN_DIR=%q SOVEREIGN_SOURCE_DIR=%q SOVEREIGN_RESUME=1 SOVEREIGN_AUTO_REBOOT=0 ' \
        "$SOVEREIGN_HOME" "$BIN_DIR" "$bootstrap/release"
      printf '/bin/bash %q --resume --version %q --home %q --profile %q --access %q' \
        "$bootstrap/release/deploy/scripts/install.sh" "$VERSION" "$SOVEREIGN_HOME" "$PROFILE" "$ACCESS_MODE"
      [[ -z "$OFFLINE_BUNDLE" ]] || printf ' --offline-bundle %q' "$OFFLINE_BUNDLE"
      [[ "$ACCESS_MODE" != domain ]] || printf ' --domain %q' "$ACCESS_TARGET"
      printf '\n'
    } > "$resume_script"
    chmod 700 "$resume_script"

    if command -v sha256sum >/dev/null 2>&1; then
      install_id="$(printf '%s' "$SOVEREIGN_HOME" | sha256sum | awk '{print substr($1, 1, 12)}')"
    else
      install_id="$(printf '%s' "$SOVEREIGN_HOME" | shasum -a 256 | awk '{print substr($1, 1, 12)}')"
    fi
    unit_name="sovereign-stack-install-$install_id.service"
    unit_dir="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"
    wants_dir="$unit_dir/default.target.wants"
    unit_file="$unit_dir/$unit_name"
    mkdir -p "$wants_dir"
    escaped_resume="$(printf '%s' "$resume_script" | sed 's/%/%%/g; s/\\/\\\\/g; s/"/\\"/g')"
    {
      printf '[Unit]\nDescription=Resume SovereignStack installation after host provisioning\n'
      printf 'StartLimitIntervalSec=600\nStartLimitBurst=10\n\n'
      printf '[Service]\nType=oneshot\nExecStart=/bin/bash "%s"\n' "$escaped_resume"
      printf 'Restart=on-failure\nRestartSec=30\nTimeoutStartSec=infinity\n\n'
      printf '[Install]\nWantedBy=default.target\n'
    } > "$unit_file"
    chmod 600 "$unit_file"
    ln -sfn "$unit_file" "$wants_dir/$unit_name"
    printf '%s\n' "$unit_file" > "$SOVEREIGN_HOME/state/install-resume-unit"
    chmod 600 "$SOVEREIGN_HOME/state/install-resume-unit"
    journal_stage awaiting-reboot "Ubuntu host dependencies installed; automatic resume is enabled"
  }
  request_reboot() {
    echo
    echo "Ubuntu host dependencies are installed. SovereignStack will resume automatically after reboot."
    echo "Status journal: $SOVEREIGN_HOME/state/install-journal.env"
    if [[ -s "$SOVEREIGN_HOME/state/install-resume-unit" ]]; then
      echo "Resume status: systemctl --user status $(basename "$(<"$SOVEREIGN_HOME/state/install-resume-unit")")"
    fi
    echo "Manual resume: bash $SOVEREIGN_HOME/bootstrap/resume.sh"
    if [[ "${SOVEREIGN_AUTO_REBOOT:-1}" != 1 || "${SOVEREIGN_TEST_NO_REBOOT:-0}" == 1 ]]; then
      if (( RESUME_MODE == 1 )); then
        echo "The NVIDIA stack is not ready yet; the resume service will retry."
        exit 75
      else
        echo "Reboot this host to continue the installation."
        exit 194
      fi
    fi
    echo "Rebooting in 10 seconds; press Ctrl-C to postpone."
    sleep 10
    run_privileged /bin/bash "$PROVISIONER" --reboot || die "automatic reboot failed; reboot manually to continue"
    exit 194
  }

  RESULT_FILE="${SOVEREIGN_UBUNTU_PROVISION_RESULT:-/var/lib/sovereign-stack/provision-$(id -u).env}"
  PENDING_REBOOT=0
  if [[ -n "${SOVEREIGN_TEST_BOOT_ID:-}" ]]; then
    CURRENT_BOOT_ID="$SOVEREIGN_TEST_BOOT_ID"
  elif [[ -r /proc/sys/kernel/random/boot_id ]]; then
    CURRENT_BOOT_ID="$(</proc/sys/kernel/random/boot_id)"
  else
    CURRENT_BOOT_ID=unknown
  fi
  if [[ -r "$RESULT_FILE" && "$(provision_result_value "$RESULT_FILE" owner_uid)" == "$(id -u)" ]]; then
    UBUNTU_MANAGED_DOCKER="$(provision_result_value "$RESULT_FILE" managed_docker)"
    [[ "$UBUNTU_MANAGED_DOCKER" == 1 ]] || UBUNTU_MANAGED_DOCKER=0
    if [[ "$(provision_result_value "$RESULT_FILE" reboot_required)" == 1 &&
          "$(provision_result_value "$RESULT_FILE" boot_id)" == "$CURRENT_BOOT_ID" ]]; then
      PENDING_REBOOT=1
    fi
  fi

  if (( PENDING_REBOOT == 1 )); then
    stage_reboot_resume
    request_reboot
  fi
  if ! ubuntu_stack_ready; then
    say "Provisioning Ubuntu, Docker, and NVIDIA host dependencies"
    journal_stage host-provisioning "administrator approval may be requested"
    PROVISION_RESULT="$TMP_ROOT/ubuntu-provision-result.log"
    PROVISION_ARGS=(--owner "$(id -un)")
    (( OFFLINE_MODE == 0 )) || PROVISION_ARGS+=(--offline)
    if ! run_privileged /bin/bash "$PROVISIONER" "${PROVISION_ARGS[@]}" | tee "$PROVISION_RESULT"; then
      die "Ubuntu host dependency provisioning failed; appliance data is unchanged"
    fi
    RESULT_FILE="$(sed -n 's/^result_file=//p' "$PROVISION_RESULT" | tail -n 1)"
    [[ -n "$RESULT_FILE" && -r "$RESULT_FILE" ]] || \
      die "Ubuntu provisioner did not produce a readable result"
    [[ "$(provision_result_value "$RESULT_FILE" owner_uid)" == "$(id -u)" ]] || \
      die "Ubuntu provisioner result belongs to another user"
    UBUNTU_MANAGED_DOCKER="$(provision_result_value "$RESULT_FILE" managed_docker)"
    [[ "$UBUNTU_MANAGED_DOCKER" == 1 ]] || UBUNTU_MANAGED_DOCKER=0
    if [[ "$(provision_result_value "$RESULT_FILE" reboot_required)" == 1 ]]; then
      stage_reboot_resume
      request_reboot
    fi
  fi

  DETAILS="$(sovereign_nvidia_gpu_details)" || \
    die "the NVIDIA driver is installed but nvidia-smi cannot enumerate a GPU"
  IFS=$'\t' read -r GPU_INDEX GPU_VRAM_MIB GPU_NAME <<< "$DETAILS"
  (( GPU_VRAM_MIB >= 24576 )) || die "the largest NVIDIA GPU must provide at least 24GB VRAM"
  GPU_NAME="${GPU_NAME:-NVIDIA GPU}"
  journal_stage host-ready "NVIDIA GPU $GPU_INDEX: $GPU_VRAM_MIB MiB"
fi

ENGINE_LIB="$SOURCE_DIR/deploy/scripts/container-engine.sh"
[[ -r "$ENGINE_LIB" ]] || die "release container-engine support is missing"
# shellcheck source=container-engine.sh
source "$ENGINE_LIB"
say "Preparing the container engine"
journal_stage engine "selecting and probing the container engine"
SOVEREIGN_ENGINE_MANIFEST="$COMPONENT_MANIFEST"
SOVEREIGN_ENGINE_OFFLINE="$OFFLINE_MODE"
if (( OFFLINE_MODE == 1 )) && [[ "$PROFILE" == metal-arm64 ]]; then
  SOVEREIGN_ENGINE_ARTIFACT_DIR="$UNPACK/installer-dependencies"
  COSIGN_ARTIFACT="$(component_value "$COMPONENT_MANIFEST" cosign artifact)"
  [[ -n "$COSIGN_ARTIFACT" && -f "$SOVEREIGN_ENGINE_ARTIFACT_DIR/$COSIGN_ARTIFACT" ]] || \
    die "offline bundle is missing its pinned signature-verification tool"
  chmod 700 "$SOVEREIGN_ENGINE_ARTIFACT_DIR/$COSIGN_ARTIFACT"
  SOVEREIGN_COSIGN="$SOVEREIGN_ENGINE_ARTIFACT_DIR/$COSIGN_ARTIFACT"
fi
export SOVEREIGN_ENGINE_MANIFEST SOVEREIGN_ENGINE_OFFLINE
export SOVEREIGN_ENGINE_ARTIFACT_DIR SOVEREIGN_COSIGN
if [[ "$PROFILE" == cuda-x86_64 && ! -e "$(sovereign_engine_state_file)" ]]; then
  PRIVATE_DOCKER_CONFIG="$SOVEREIGN_HOME/state/docker-config"
  mkdir -p "$PRIVATE_DOCKER_CONFIG"
  [[ -f "$PRIVATE_DOCKER_CONFIG/config.json" ]] || printf '{}\n' > "$PRIVATE_DOCKER_CONFIG/config.json"
  chmod 700 "$PRIVATE_DOCKER_CONFIG"
  chmod 600 "$PRIVATE_DOCKER_CONFIG/config.json"
  if [[ "$UBUNTU_MANAGED_DOCKER" == 1 ]]; then
    sovereign_engine_write_state managed-docker 1 "$(command -v docker)" default "$PRIVATE_DOCKER_CONFIG" || \
      die "could not record the installer-managed Ubuntu engine"
  else
    sovereign_engine_write_state existing 0 "$(command -v docker)" default "$PRIVATE_DOCKER_CONFIG" || \
      die "could not record the compatible Ubuntu engine"
  fi
fi
sovereign_engine_require || die \
  "container-engine setup could not complete; existing appliance data is safe"
if [[ -n "${OFFLINE_IMAGES:-}" && -f "$OFFLINE_IMAGES" ]]; then
  sovereign_engine_docker load -i "$OFFLINE_IMAGES" >/dev/null
fi
if ! sovereign_engine_probe_compatibility "$COMPONENT_MANIFEST" "$PROFILE"; then
  if [[ "$PROFILE" == metal-arm64 && "$SOVEREIGN_ENGINE_PROVIDER" == existing ]]; then
    say "The existing engine is incompatible; preparing an isolated managed engine"
    sovereign_engine_provision_managed_colima "$COMPONENT_MANIFEST" || die \
      "managed container-engine fallback could not be prepared; existing engines and appliance data are unchanged"
    if [[ -n "${OFFLINE_IMAGES:-}" && -f "$OFFLINE_IMAGES" ]]; then
      sovereign_engine_docker load -i "$OFFLINE_IMAGES" >/dev/null
    fi
    sovereign_engine_probe_compatibility "$COMPONENT_MANIFEST" "$PROFILE" || die \
      "managed container-engine compatibility checks failed; probe resources were removed and appliance data is safe"
  else
    die "container-engine compatibility checks failed; probe resources were removed and appliance data is safe"
  fi
fi
if [[ "$PROFILE" == cuda-x86_64 ]]; then
  sovereign_engine_docker info --format '{{json .Runtimes}}' | grep -qi nvidia || \
    die "NVIDIA Container Toolkit is not configured for the selected engine"
  sovereign_engine_probe_cuda "$COMPONENT_MANIFEST" "$GPU_INDEX" || \
    die "NVIDIA GPU verification failed inside the pinned CUDA runtime container"
fi

say "Installing release assets"
journal_stage release "installing release assets"
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
  SOVEREIGN_ACCESS_MODE="$ACCESS_MODE" SOVEREIGN_SITE_ADDRESS="${SOVEREIGN_SITE_ADDRESS:-}" \
  SOVEREIGN_HOST_OS="$(uname -s | tr '[:upper:]' '[:lower:]')" SOVEREIGN_HOST_ARCH="$(uname -m)" \
  SOVEREIGN_HOST_MEMORY_BYTES="$HOST_MEMORY_BYTES" SOVEREIGN_GPU_NAME="$GPU_NAME" \
  SOVEREIGN_GPU_VRAM_MIB="$GPU_VRAM_MIB" SOVEREIGN_CUDA_GPU_INDEX="$GPU_INDEX" \
  SOVEREIGN_RELEASE_ROOT="$TARGET" "$TARGET/deploy/scripts/generate-config.sh"
if (( OFFLINE_MODE == 1 )); then
  : > "$SOVEREIGN_HOME/state/offline"
else
  rm -f "$SOVEREIGN_HOME/state/offline"
fi

mkdir -p "$BIN_DIR"
install -m 755 "$TARGET/deploy/scripts/sovereign" "$BIN_DIR/sovereign"
printf '%s\n' "$BIN_DIR" > "$SOVEREIGN_HOME/state/bin-dir"
chmod 600 "$SOVEREIGN_HOME/state/bin-dir"

case "$(uname -s)-$(uname -m)" in
  Darwin-arm64) HOSTD_ASSET="$TARGET/deploy/assets/sovereign-hostd-darwin-arm64" ;;
  Linux-x86_64) HOSTD_ASSET="$TARGET/deploy/assets/sovereign-hostd-linux-amd64" ;;
  *) HOSTD_ASSET="" ;;
esac
if [[ -n "$HOSTD_ASSET" && -f "$HOSTD_ASSET" ]]; then
  install -m 755 "$HOSTD_ASSET" "$BIN_DIR/sovereign-hostd"
  if [[ "${SOVEREIGN_SKIP_HOSTD_INSTALL:-0}" != 1 ]]; then
    SOVEREIGN_HOME="$SOVEREIGN_HOME" SOVEREIGN_HOSTD_BINARY="$BIN_DIR/sovereign-hostd" \
      SOVEREIGN_CLI_BINARY="$BIN_DIR/sovereign" "$TARGET/deploy/scripts/install-hostd.sh"
  fi
else
  say "Host lifecycle service is not present in this developer build; CLI access remains available"
fi

# Make the authenticated control portal available before optional runtimes and
# model weights. The remainder of installation can be observed in the browser
# instead of presenting a blank or unreachable URL for several minutes.
if [[ "${SOVEREIGN_SKIP_START:-0}" != 1 ]]; then
  say "Starting the SovereignStack portal"
  SOVEREIGN_HOME="$SOVEREIGN_HOME" "$BIN_DIR/sovereign" start
fi

download_hf() {
  local repo="$1" revision="$2" file="$3" expected="$4" destination="$5" url role total started
  [[ -f "$destination" && "$(sha256 "$destination")" == "$expected" ]] && return 0
  mkdir -p "$(dirname "$destination")"
  url="https://huggingface.co/$repo/resolve/$revision/$file?download=true"
  role=generation
  [[ "$destination" == *embeddinggemma* ]] && role=embeddings
  if [[ -n "${HF_TOKEN:-}" ]]; then
    total="$( { curl -fsSIL -H "Authorization: Bearer $HF_TOKEN" "$url" 2>/dev/null || true; } | awk 'BEGIN{IGNORECASE=1} /^content-length:/ {gsub("\\r", "", $2); value=$2} END{print value+0}')"
  else
    total="$( { curl -fsSIL "$url" 2>/dev/null || true; } | awk 'BEGIN{IGNORECASE=1} /^content-length:/ {gsub("\\r", "", $2); value=$2} END{print value+0}')"
  fi
  started="$(date +%s)"
  tracked_curl() {
    local resume="$1" pid result current progress_tmp
    progress_tmp="$SOVEREIGN_HOME/state/install-progress.json.tmp"
    [[ -f "$destination.part" ]] || : > "$destination.part"
    if [[ -n "${HF_TOKEN:-}" ]]; then
      if [[ "$resume" == true ]]; then curl -fL --retry 5 -C - -H "Authorization: Bearer $HF_TOKEN" -o "$destination.part" "$url" &
      else curl -fL --retry 5 -H "Authorization: Bearer $HF_TOKEN" -o "$destination.part" "$url" & fi
    elif [[ "$resume" == true ]]; then curl -fL --retry 5 -C - -o "$destination.part" "$url" &
    else curl -fL --retry 5 -o "$destination.part" "$url" & fi
    pid=$!
    while kill -0 "$pid" 2>/dev/null; do
      current="$(wc -c < "$destination.part" 2>/dev/null || printf 0)"
      printf '{"role":"%s","stage":"downloading","file":"%s","current":%s,"total":%s,"started_unix":%s}\n' \
        "$role" "${file//\"/}" "$current" "${total:-0}" "$started" > "$progress_tmp"
      mv "$progress_tmp" "$SOVEREIGN_HOME/state/install-progress.json"
      sleep 1
    done
    wait "$pid"; result=$?
    return "$result"
  }
  # macOS ships Bash 3.2, where expanding an empty array under `set -u`
  # raises an unbound-variable error. Keep the authenticated and anonymous
  # invocations explicit so the one-command Metal installer works there.
  if [[ -n "${HF_TOKEN:-}" ]]; then
    if ! tracked_curl true; then
      rm -f "$destination.part"
      tracked_curl false || \
        die "download failed for $repo/$file; set HF_TOKEN if the repository is gated"
    fi
  elif ! tracked_curl true; then
    rm -f "$destination.part"
    tracked_curl false || \
      die "download failed for $repo/$file; set HF_TOKEN if the repository is gated"
  fi
  printf '{"role":"%s","stage":"verifying","file":"%s","current":%s,"total":%s,"started_unix":%s}\n' \
    "$role" "${file//\"/}" "$(wc -c < "$destination.part")" "${total:-0}" "$started" > "$SOVEREIGN_HOME/state/install-progress.json"
  [[ "$(sha256 "$destination.part")" == "$expected" ]] || die "model checksum mismatch: $file"
  mv "$destination.part" "$destination"
  printf '{"role":"%s","stage":"complete","file":"%s","current":%s,"total":%s,"started_unix":%s}\n' \
    "$role" "${file//\"/}" "$(wc -c < "$destination")" "$(wc -c < "$destination")" "$started" > "$SOVEREIGN_HOME/state/install-progress.json"
}

journal_stage models "installing selected runtimes and model weights"
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
    AGENT_ARCHIVE="$METAL_AGENT_ARCHIVE"
    AGENT_URL="$METAL_AGENT_URL"
    AGENT_SIGNATURE_URL="$METAL_AGENT_SIGNATURE_URL"
    if [[ -n "${SOVEREIGN_RUNTIME_RELEASE_URL:-}" ]]; then
      AGENT_URL="${SOVEREIGN_RUNTIME_RELEASE_URL%/}/$AGENT_ARCHIVE"
      AGENT_SIGNATURE_URL="$AGENT_URL.sigstore.json"
    fi
    curl -fsSL --retry 4 -o "$TMP_ROOT/$AGENT_ARCHIVE" "$AGENT_URL"
    [[ "$(sha256 "$TMP_ROOT/$AGENT_ARCHIVE")" == "$METAL_AGENT_SHA256" ]] || \
      die "Metal agent checksum mismatch: $AGENT_ARCHIVE"
    [[ "$(file_bytes "$TMP_ROOT/$AGENT_ARCHIVE")" == "$METAL_AGENT_BYTES" ]] || \
      die "Metal agent size mismatch: $AGENT_ARCHIVE"
    curl -fsSL --retry 2 -o "$TMP_ROOT/$AGENT_ARCHIVE.sigstore.json" "$AGENT_SIGNATURE_URL" || \
      die "Metal agent signature bundle is missing"
    install_cosign
    "$TMP_ROOT/cosign" verify-blob \
      --bundle "$TMP_ROOT/$AGENT_ARCHIVE.sigstore.json" \
      --certificate-identity-regexp "$METAL_AGENT_SIGNER" \
      --certificate-oidc-issuer https://token.actions.githubusercontent.com \
      "$TMP_ROOT/$AGENT_ARCHIVE" >/dev/null
    verify_archive_paths "$TMP_ROOT/$AGENT_ARCHIVE"
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

  if [[ "${SOVEREIGN_SKIP_AGENT_HEALTH:-0}" != 1 ]]; then
    say "Waiting for the Metal generation service"
    AGENT_DEADLINE=$((SECONDS + ${SOVEREIGN_AGENT_TIMEOUT:-600}))
    AGENT_LAST_UPDATE=$SECONDS
    while true; do
      if curl -fsS --max-time 5 -H "Authorization: Bearer $(<"$SOVEREIGN_HOME/agent.token")" \
        http://127.0.0.1:9100/agent/manifest 2>/dev/null | \
        grep -Eq '"generation"[^}]*"status"[[:space:]]*:[[:space:]]*"healthy"'; then
        break
      fi
      (( SECONDS < AGENT_DEADLINE )) || {
        tail -n 80 "$SOVEREIGN_HOME/logs/agent.log" 2>/dev/null || true
        die "Metal generation service did not become healthy; existing data is safe, see $SOVEREIGN_HOME/logs/agent.log"
      }
      if (( SECONDS - AGENT_LAST_UPDATE >= 15 )); then
        echo "Metal generation service is still loading ($((SECONDS + ${SOVEREIGN_AGENT_TIMEOUT:-600} - AGENT_DEADLINE))s elapsed)..."
        AGENT_LAST_UPDATE=$SECONDS
      fi
      sleep 2
    done
  fi

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
      curl -fsSL --retry 4 -o "$EMBEDDING_BINARY.part" "$EMBEDDINGGEMMA_METAL_URL"
    fi
    [[ "$(sha256 "$EMBEDDING_BINARY.part")" == "$EMBEDDINGGEMMA_METAL_SHA256" ]] || \
      die "embeddinggemma Metal binary checksum mismatch"
    [[ "$(file_bytes "$EMBEDDING_BINARY.part")" == "$EMBEDDINGGEMMA_METAL_BYTES" ]] || \
      die "embeddinggemma Metal binary size mismatch"
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
  say "Completing SovereignStack startup"
  journal_stage startup "starting services and running health gates"
  SOVEREIGN_HOME="$SOVEREIGN_HOME" "$BIN_DIR/sovereign" up
else
  say "Installation staged; start skipped by SOVEREIGN_SKIP_START=1"
fi

journal_stage complete "SovereignStack $VERSION installed successfully"
if [[ -s "$SOVEREIGN_HOME/state/install-resume-unit" ]]; then
  RESUME_UNIT="$(<"$SOVEREIGN_HOME/state/install-resume-unit")"
  EXPECTED_UNIT_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"
  if [[ "$RESUME_UNIT" == "$EXPECTED_UNIT_DIR"/sovereign-stack-install-*.service ]]; then
    rm -f "$EXPECTED_UNIT_DIR/default.target.wants/$(basename "$RESUME_UNIT")" "$RESUME_UNIT"
    systemctl --user daemon-reload >/dev/null 2>&1 || true
  fi
  rm -f "$SOVEREIGN_HOME/state/install-resume-unit"
fi
if [[ -d "$SOVEREIGN_HOME/bootstrap" ]]; then
  rm -f "$SOVEREIGN_HOME/bootstrap/resume.sh"
  rm -rf "$SOVEREIGN_HOME/bootstrap/release"
  rmdir "$SOVEREIGN_HOME/bootstrap" 2>/dev/null || true
fi

echo
echo "SovereignStack $VERSION installed for $PROFILE"
echo "Portal: $(SOVEREIGN_HOME="$SOVEREIGN_HOME" "$BIN_DIR/sovereign" url)"
if command -v qrencode >/dev/null 2>&1; then
  SOVEREIGN_HOME="$SOVEREIGN_HOME" "$BIN_DIR/sovereign" url | qrencode -t ANSIUTF8
fi
echo "Open it any time with: sovereign open"
echo "For another computer: sovereign access lan, or sovereign access domain your-hostname.example"
echo "If the first-admin link expires: sovereign admin setup-link"
