#!/usr/bin/env bash
set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TEST_HOME="$(mktemp -d "${TMPDIR:-/tmp}/sovereign-install-test.XXXXXX")"
trap 'rm -rf "$TEST_HOME"' EXIT
mkdir -p "$TEST_HOME/user-home"
export HOME="$TEST_HOME/user-home"
export PATH="$ROOT/tests/fixtures/bin:$PATH"
export SOVEREIGN_SYSFS_ROOT="$TEST_HOME/sys"
export SOVEREIGN_TEST_CUDA_PROBE_IMAGE="example.test/sovereign-runtime@sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
mkdir -p "$SOVEREIGN_SYSFS_ROOT/bus/pci/devices/0000:01:00.0"
printf '0x10de\n' > "$SOVEREIGN_SYSFS_ROOT/bus/pci/devices/0000:01:00.0/vendor"
printf '0x030200\n' > "$SOVEREIGN_SYSFS_ROOT/bus/pci/devices/0000:01:00.0/class"
VERSION="$(<"$ROOT/VERSION")"

SOVEREIGN_HOME="$TEST_HOME" \
SOVEREIGN_BIN_DIR="$TEST_HOME/bin" \
SOVEREIGN_SOURCE_DIR="$ROOT" \
SOVEREIGN_INCLUDE_MODELS=0 \
SOVEREIGN_SKIP_START=1 \
  "$ROOT/deploy/scripts/install.sh" --profile metal-arm64 --json \
  > "$TEST_HOME/install-events.jsonl" 2> "$TEST_HOME/install-human.log"

# JSON mode reserves stdout for a versioned event stream while preserving
# ordinary command output on stderr. The latest event is also replaced
# atomically on disk for package launchers and recovery tools.
python3 - "$TEST_HOME/install-events.jsonl" "$TEST_HOME/state/install-event.json" \
  "$ROOT/schemas/installer-event.schema.json" <<'PY'
import json
import sys

from jsonschema import Draft202012Validator, FormatChecker

events = [json.loads(line) for line in open(sys.argv[1], encoding="utf-8") if line.strip()]
schema = json.load(open(sys.argv[3], encoding="utf-8"))
validator = Draft202012Validator(schema, format_checker=FormatChecker())
assert events
required = {
    "schema_version", "sequence", "timestamp", "version", "profile",
    "stage", "status", "message", "data_safe", "recovery", "component",
    "current_bytes", "total_bytes",
}
assert all(set(event) == required for event in events)
assert [event["sequence"] for event in events] == list(range(1, len(events) + 1))
assert all(event["schema_version"] == 1 for event in events)
assert all(event["profile"] == "metal-arm64" for event in events)
assert all(not list(validator.iter_errors(event)) for event in events)
assert any(event["stage"] == "engine" for event in events)
assert events[-1]["stage"] == "complete"
assert events[-1]["status"] == "completed"
assert events[-1]["data_safe"] is True
latest = json.load(open(sys.argv[2], encoding="utf-8"))
assert latest == events[-1]
PY
grep -q 'Checking metal-arm64 prerequisites' "$TEST_HOME/install-human.log"
grep -q "SovereignStack $VERSION installed for metal-arm64" "$TEST_HOME/install-human.log"
grep -qx 'stage=complete' "$TEST_HOME/state/install-journal.env"

FAILED_EVENT_HOME="$TEST_HOME/failed-event-install"
set +e
SOVEREIGN_HOME="$FAILED_EVENT_HOME" \
SOVEREIGN_BIN_DIR="$FAILED_EVENT_HOME/bin" \
SOVEREIGN_SOURCE_DIR="$ROOT" \
  "$ROOT/deploy/scripts/install.sh" --profile unsupported --json \
  > "$TEST_HOME/failed-install-events.jsonl" 2> "$TEST_HOME/failed-install-human.log"
FAILED_INSTALL_STATUS=$?
set -e
(( FAILED_INSTALL_STATUS != 0 ))
python3 - "$TEST_HOME/failed-install-events.jsonl" \
  "$ROOT/schemas/installer-event.schema.json" <<'PY'
import json
import sys

from jsonschema import Draft202012Validator, FormatChecker

events = [json.loads(line) for line in open(sys.argv[1], encoding="utf-8") if line.strip()]
schema = json.load(open(sys.argv[2], encoding="utf-8"))
validator = Draft202012Validator(schema, format_checker=FormatChecker())
assert all(not list(validator.iter_errors(event)) for event in events)
assert events[-1]["stage"] == "failed"
assert events[-1]["status"] == "failed"
assert events[-1]["data_safe"] is True
assert events[-1]["recovery"]
PY
grep -q 'error: unsupported profile unsupported' "$TEST_HOME/failed-install-human.log"
grep -qx 'stage=failed' "$FAILED_EVENT_HOME/state/install-journal.env"

SOVEREIGN_HOME="$TEST_HOME" "$TEST_HOME/bin/sovereign" validate
if SOVEREIGN_TEST_DOCKER_OWNER=/another/install \
  SOVEREIGN_HOME="$TEST_HOME" "$TEST_HOME/bin/sovereign" up >"$TEST_HOME/takeover.log" 2>&1; then
  echo "sovereign up accepted a foreign Compose project" >&2
  exit 1
fi
grep -q "belongs to another SovereignStack installation" "$TEST_HOME/takeover.log"
for file in .env agent.token secrets/control-vault.key; do
  mode="$(if stat -f %Lp "$TEST_HOME/$file" >/dev/null 2>&1; then stat -f %Lp "$TEST_HOME/$file"; else stat -c %a "$TEST_HOME/$file"; fi)"
  [[ "$mode" == 600 ]] || { echo "$file mode is $mode, expected 600" >&2; exit 1; }
done
[[ ! -e "$TEST_HOME/credentials" ]] || { echo "legacy generated credentials file should not exist" >&2; exit 1; }
[[ "$(SOVEREIGN_HOME="$TEST_HOME" "$TEST_HOME/bin/sovereign" url)" == "http://127.0.0.1:54854/" ]]
grep -qx 'SOVEREIGN_ACCESS_MODE=desktop' "$TEST_HOME/.env"
grep -qx 'SOVEREIGN_PUBLIC_URL=http://127.0.0.1:54854' "$TEST_HOME/.env"

# Upgrades preserve an existing installation's saved port instead of silently
# moving its portal to the new default.
LEGACY_PORT_HOME="$TEST_HOME/legacy-port"
mkdir -p "$LEGACY_PORT_HOME"
cat > "$LEGACY_PORT_HOME/.env" <<'EOF'
HTTP_PORT=8880
SOVEREIGN_PUBLIC_URL=http://127.0.0.1:8880
EOF
SOVEREIGN_HOME="$LEGACY_PORT_HOME" \
SOVEREIGN_PROFILE=metal-arm64 \
SOVEREIGN_RELEASE_ROOT="$TEST_HOME/current" \
  "$ROOT/deploy/scripts/generate-config.sh"
grep -qx 'HTTP_PORT=8880' "$LEGACY_PORT_HOME/.env"
grep -qx 'SOVEREIGN_PUBLIC_URL=http://127.0.0.1:8880' "$LEGACY_PORT_HOME/.env"

# Noninteractive installs can select a TLS domain directly; unsafe public HTTP
# remains impossible without the explicit acknowledgement value.
DOMAIN_HOME="$TEST_HOME/domain-config"
SOVEREIGN_HOME="$DOMAIN_HOME" \
SOVEREIGN_PROFILE=metal-arm64 \
SOVEREIGN_ACCESS_MODE=domain \
SOVEREIGN_SITE_ADDRESS=ai.example.test \
SOVEREIGN_RELEASE_ROOT="$TEST_HOME/current" \
  "$ROOT/deploy/scripts/generate-config.sh"
grep -qx 'SOVEREIGN_BIND_ADDRESS=0.0.0.0' "$DOMAIN_HOME/.env"
grep -qx 'HTTP_PORT=80' "$DOMAIN_HOME/.env"
grep -qx 'HTTPS_PORT=443' "$DOMAIN_HOME/.env"
grep -qx 'SOVEREIGN_PUBLIC_URL=https://ai.example.test' "$DOMAIN_HOME/.env"
if SOVEREIGN_HOME="$TEST_HOME/rejected-wan" \
  SOVEREIGN_PROFILE=metal-arm64 \
  SOVEREIGN_ACCESS_MODE=wan-http \
  SOVEREIGN_PUBLIC_URL=http://203.0.113.10:54854 \
  SOVEREIGN_RELEASE_ROOT="$TEST_HOME/current" \
  "$ROOT/deploy/scripts/generate-config.sh" >/dev/null 2>&1; then
  echo "generate-config accepted public cleartext HTTP without acknowledgement" >&2
  exit 1
fi
grep -Eq '^SOVEREIGN_HOST_UID=[0-9]+$' "$TEST_HOME/.env"
grep -Eq '^SOVEREIGN_HOST_GID=[0-9]+$' "$TEST_HOME/.env"
[[ "$(readlink "$TEST_HOME/current")" == "$TEST_HOME/releases/$VERSION" ]]

# Upgrades preserve unrelated generation and remote-model configuration while
# replacing the retired runtime-hosted embedding profiles. Original files are
# retained as one-time migration backups.
cat > "$TEST_HOME/config/runtime.yaml" <<'EOF'
schema_version: "1.1"
runtime:
  profile: metal-arm64
roles:
  generation:
    enabled: true
    task: generate
    served_model_name: custom-generation
  embedding:
    enabled: true
    task: embed
    model: LCO-Embedding/LCO-Embedding-Omni-3B-2605
    served_model_name: embedding-omni-default
observability:
  structured_logs: true
EOF
cat > "$TEST_HOME/config/embedding-profiles.yaml" <<'EOF'
embedding_profiles:
  custom-remote:
    provider: custom
    served_model_name: custom-remote-embedding
    modalities: [text]
  omni-default:
    provider: sovereign-runtime
    model: LCO-Embedding/LCO-Embedding-Omni-3B-2605
    served_model_name: embedding-omni-default
    modalities: [text, image, audio]
EOF
cat > "$TEST_HOME/config/model-registry.yaml" <<'EOF'
models:
  - id: custom-generation
    role: generation
    source: remote
    model: custom/generation
  - id: custom-remote-embedding
    role: embedding
    source: remote
    model: custom/embedding
  - id: embedding-omni-default
    role: embedding
    source: huggingface
    model: LCO-Embedding/LCO-Embedding-Omni-3B-2605
EOF
SOVEREIGN_HOME="$TEST_HOME" \
SOVEREIGN_BIN_DIR="$TEST_HOME/bin" \
SOVEREIGN_SOURCE_DIR="$ROOT" \
SOVEREIGN_INCLUDE_MODELS=0 \
SOVEREIGN_SKIP_START=1 \
  "$ROOT/deploy/scripts/install.sh" --profile metal-arm64
grep -q '^    served_model_name: custom-generation$' "$TEST_HOME/config/runtime.yaml"
grep -q '^observability:$' "$TEST_HOME/config/runtime.yaml"
grep -A2 '^  embedding:$' "$TEST_HOME/config/runtime.yaml" | grep -q '^    enabled: false$'
grep -q '^  custom-remote:$' "$TEST_HOME/config/embedding-profiles.yaml"
grep -q '^  gemma-default:$' "$TEST_HOME/config/embedding-profiles.yaml"
grep -q '^  - id: custom-remote-embedding$' "$TEST_HOME/config/model-registry.yaml"
[[ "$(grep -c '^  - id: embedding-gemma-default$' "$TEST_HOME/config/model-registry.yaml")" == 1 ]]
! grep -Fq 'LCO-Embedding/LCO-Embedding-Omni-3B-2605' \
  "$TEST_HOME/config/runtime.yaml" "$TEST_HOME/config/embedding-profiles.yaml" \
  "$TEST_HOME/config/model-registry.yaml"
grep -Fq 'LCO-Embedding/LCO-Embedding-Omni-3B-2605' \
  "$TEST_HOME/config/runtime.yaml.pre-embeddinggemma"

# The Metal embedding service uses an install-scoped launchd label, escapes
# paths in its owner-only plist, restarts on failure, and uninstalls only its
# own job.
METAL_SERVICE_HOME="$TEST_HOME/metal & service"
mkdir -p "$METAL_SERVICE_HOME/bin" "$METAL_SERVICE_HOME/models"
printf '#!/usr/bin/env sh\nexit 0\n' > "$METAL_SERVICE_HOME/bin/embeddinggemma"
chmod 755 "$METAL_SERVICE_HOME/bin/embeddinggemma"
: > "$METAL_SERVICE_HOME/models/embeddinggemma.gguf"
SOVEREIGN_HOME="$METAL_SERVICE_HOME" \
EMBEDDINGGEMMA_BINARY="$METAL_SERVICE_HOME/bin/embeddinggemma" \
EMBEDDINGGEMMA_MODEL="$METAL_SERVICE_HOME/models/embeddinggemma.gguf" \
SOVEREIGN_LAUNCHD_SKIP_HEALTH=1 \
  "$ROOT/deploy/scripts/install-embeddinggemma-metal.sh"
METAL_LABEL="$(<"$METAL_SERVICE_HOME/state/embeddinggemma-launchd-label")"
METAL_PLIST="$HOME/Library/LaunchAgents/$METAL_LABEL.plist"
[[ -f "$METAL_PLIST" ]]
grep -q '<key>KeepAlive</key>' "$METAL_PLIST"
grep -q 'metal &amp; service' "$METAL_PLIST"
if command -v plutil >/dev/null 2>&1; then
  plutil -lint "$METAL_PLIST" >/dev/null
else
  python3 - "$METAL_PLIST" <<'PY'
import plistlib
import sys

with open(sys.argv[1], "rb") as plist:
    plistlib.load(plist)
PY
fi
SOVEREIGN_HOME="$METAL_SERVICE_HOME" "$ROOT/deploy/scripts/uninstall-embeddinggemma-metal.sh"
[[ ! -e "$METAL_PLIST" ]]

# Shared launchd replacement retries transient bootstrap error 5 and restores
# the previous loaded service if a replacement cannot be registered.
LAUNCHD_TEST="$TEST_HOME/launchd-helper"
mkdir -p "$LAUNCHD_TEST/state" "$LAUNCHD_TEST/live"
OLD_PLIST="$LAUNCHD_TEST/live/service.plist"
NEW_PLIST="$LAUNCHD_TEST/new.plist"
cat > "$OLD_PLIST" <<'EOF'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict><key>Label</key><string>test.service</string><key>ProgramArguments</key><array><string>/usr/bin/true</string></array><key>OLD_SERVICE</key><true/></dict></plist>
EOF
cat > "$NEW_PLIST" <<'EOF'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict><key>Label</key><string>test.service</string><key>ProgramArguments</key><array><string>/usr/bin/true</string></array><key>NEW_SERVICE</key><true/></dict></plist>
EOF
: > "$LAUNCHD_TEST/state/loaded"
SOVEREIGN_TEST_LAUNCHCTL_STATE="$LAUNCHD_TEST/state" \
SOVEREIGN_TEST_LAUNCHCTL_FAIL_COUNT=2 \
  "$ROOT/deploy/scripts/launchd-service.sh" install test.service "$NEW_PLIST" "$OLD_PLIST"
[[ "$(<"$LAUNCHD_TEST/state/attempts")" == 3 ]]
grep -q NEW_SERVICE "$OLD_PLIST"

rm -f "$LAUNCHD_TEST/state/attempts"
: > "$LAUNCHD_TEST/state/loaded"
cp "$OLD_PLIST" "$NEW_PLIST.success"
sed 's/NEW_SERVICE/OLD_SERVICE/' "$NEW_PLIST.success" > "$OLD_PLIST"
if SOVEREIGN_TEST_LAUNCHCTL_STATE="$LAUNCHD_TEST/state" \
  SOVEREIGN_TEST_LAUNCHCTL_FAIL_NEW=1 \
  SOVEREIGN_LAUNCHD_RETRIES=3 \
    "$ROOT/deploy/scripts/launchd-service.sh" install test.service "$NEW_PLIST" "$OLD_PLIST"; then
  echo "launchd helper accepted an unregistrable replacement" >&2
  exit 1
fi
grep -q OLD_SERVICE "$OLD_PLIST"
[[ -f "$LAUNCHD_TEST/state/loaded" ]]

# The documented bootstrap is piped into Bash, where BASH_SOURCE[0] is unset.
# Exercise that exact shell form early without touching a real installation.
PIPE_HOME="$TEST_HOME/piped-install"
cat "$ROOT/deploy/scripts/install.sh" | env \
  SOVEREIGN_HOME="$PIPE_HOME" \
  SOVEREIGN_BIN_DIR="$PIPE_HOME/bin" \
  SOVEREIGN_SOURCE_DIR="$ROOT" \
  SOVEREIGN_INCLUDE_MODELS=0 \
  SOVEREIGN_SKIP_START=1 \
  bash -s -- --profile metal-arm64
[[ "$(readlink "$PIPE_HOME/current")" == "$PIPE_HOME/releases/$VERSION" ]]
grep -qx "SOVEREIGN_VERSION=$VERSION" "$PIPE_HOME/.env"

# Linux exposes a generic VERSION variable through /etc/os-release. Exercise
# the CUDA path and prove that it cannot replace the product release version.
CUDA_HOME="$TEST_HOME/cuda-install"
SOVEREIGN_TEST_UNAME_S=Linux \
SOVEREIGN_TEST_UNAME_M=x86_64 \
SOVEREIGN_OS_RELEASE="$ROOT/tests/fixtures/os-release" \
SOVEREIGN_HOME="$CUDA_HOME" \
SOVEREIGN_BIN_DIR="$CUDA_HOME/bin" \
SOVEREIGN_SOURCE_DIR="$ROOT" \
SOVEREIGN_INCLUDE_MODELS=0 \
SOVEREIGN_SKIP_START=1 \
  "$ROOT/deploy/scripts/install.sh" --profile cuda-x86_64
[[ "$(readlink "$CUDA_HOME/current")" == "$CUDA_HOME/releases/$VERSION" ]]
grep -qx "SOVEREIGN_VERSION=$VERSION" "$CUDA_HOME/.env"
grep -qx 'SOVEREIGN_CUDA_GPU_INDEX=0' "$CUDA_HOME/.env"
SOVEREIGN_TEST_UNAME_S=Linux \
SOVEREIGN_TEST_UNAME_M=x86_64 \
SOVEREIGN_OS_RELEASE="$ROOT/tests/fixtures/os-release" \
SOVEREIGN_HOME="$CUDA_HOME" \
SOVEREIGN_INCLUDE_MODELS=0 \
SOVEREIGN_SKIP_START=1 \
  "$CUDA_HOME/bin/sovereign" repair > "$TEST_HOME/cuda-repair.log"
grep -q 'Ubuntu host dependencies and container engine repaired' "$TEST_HOME/cuda-repair.log"
grep -qx 'stage=complete' "$CUDA_HOME/state/install-journal.env"

# Exercise bundle creation and a fresh offline install without downloading
# weights. The bundle must carry the complete installer toolchain so the fresh
# target can begin without Docker, Colima, Compose, Cosign, or network access.
mkdir -p "$TEST_HOME/runtime-dist/$VERSION/agent-dist"
cp "$ROOT/deploy/scripts/uninstall.sh" "$TEST_HOME/runtime-dist/$VERSION/agent-dist/install-agent.sh"
chmod +x "$TEST_HOME/runtime-dist/$VERSION/agent-dist/install-agent.sh"
BUNDLE_DEPS="$TEST_HOME/bundle-dependencies"
mkdir -p "$BUNDLE_DEPS/lima/bin" "$BUNDLE_DEPS/docker/docker"
cat > "$BUNDLE_DEPS/cosign-test" <<'SH'
#!/usr/bin/env bash
exit 0
SH
cat > "$BUNDLE_DEPS/colima-test" <<'SH'
#!/usr/bin/env bash
printf 'colima %s\n' "$*" >> "${LIFECYCLE_ENGINE_LOG:?}"
exit 0
SH
printf 'test colima disk image\n' > "$BUNDLE_DEPS/colima-disk.raw.gz"
cat > "$BUNDLE_DEPS/lima/bin/limactl" <<'SH'
#!/usr/bin/env bash
exit 0
SH
cp "$ROOT/tests/fixtures/bin/docker" "$BUNDLE_DEPS/docker/docker/docker"
cat > "$BUNDLE_DEPS/docker-compose-test" <<'SH'
#!/usr/bin/env bash
exit 0
SH
chmod 755 "$BUNDLE_DEPS/cosign-test" "$BUNDLE_DEPS/colima-test" \
  "$BUNDLE_DEPS/lima/bin/limactl" "$BUNDLE_DEPS/docker/docker/docker" \
  "$BUNDLE_DEPS/docker-compose-test"
tar -czf "$BUNDLE_DEPS/lima-test.tar.gz" -C "$BUNDLE_DEPS/lima" .
tar -czf "$BUNDLE_DEPS/docker-test.tgz" -C "$BUNDLE_DEPS/docker" .
printf '{}\n' > "$BUNDLE_DEPS/docker-compose-test.sigstore.json"
test_hash() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | awk '{print $1}'
  else shasum -a 256 "$1" | awk '{print $1}'; fi
}
test_bytes() {
  if stat -f %z "$1" >/dev/null 2>&1; then stat -f %z "$1"
  else stat -c %s "$1"; fi
}
cat > "$TEST_HOME/current/release/release-source.json" <<EOF
{
  "metal_agent": {
    "version": "test", "artifact": "metal-agent-test.tar.gz",
    "url": "https://example.test/metal-agent-test.tar.gz",
    "signature_url": "https://example.test/metal-agent-test.tar.gz.sigstore.json",
    "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
    "bytes": 1, "signer_identity_regexp": "^test$"
  },
  "embedding_runtime": {
    "version": "test", "artifact": "embedding-test",
    "url": "https://example.test/embedding-test",
    "sha256": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
    "bytes": 1
  },
  "engine_probe": {
    "image": "example.test/probe@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
    "container_port": 80, "minimum_api_version": "1.41", "minimum_free_kib": 20971520
  },
  "installer_dependencies": {"metal-arm64": {
    "cosign": {
      "version": "test", "artifact": "cosign-test", "url": "file://$BUNDLE_DEPS/cosign-test",
      "sha256": "$(test_hash "$BUNDLE_DEPS/cosign-test")", "bytes": $(test_bytes "$BUNDLE_DEPS/cosign-test"), "format": "executable"
    },
    "colima": {
      "version": "test", "artifact": "colima-test", "url": "file://$BUNDLE_DEPS/colima-test",
      "sha256": "$(test_hash "$BUNDLE_DEPS/colima-test")", "bytes": $(test_bytes "$BUNDLE_DEPS/colima-test"), "format": "executable"
    },
    "colima_disk_image": {
      "version": "test", "artifact": "colima-disk.raw.gz", "url": "file://$BUNDLE_DEPS/colima-disk.raw.gz",
      "sha256": "$(test_hash "$BUNDLE_DEPS/colima-disk.raw.gz")", "bytes": $(test_bytes "$BUNDLE_DEPS/colima-disk.raw.gz"), "format": "raw.gz"
    },
    "lima": {
      "version": "test", "artifact": "lima-test.tar.gz", "url": "file://$BUNDLE_DEPS/lima-test.tar.gz",
      "sha256": "$(test_hash "$BUNDLE_DEPS/lima-test.tar.gz")", "bytes": $(test_bytes "$BUNDLE_DEPS/lima-test.tar.gz"), "format": "tar.gz"
    },
    "docker_cli": {
      "version": "test", "artifact": "docker-test.tgz", "url": "file://$BUNDLE_DEPS/docker-test.tgz",
      "sha256": "$(test_hash "$BUNDLE_DEPS/docker-test.tgz")", "bytes": $(test_bytes "$BUNDLE_DEPS/docker-test.tgz"), "format": "tar.gz"
    },
    "docker_compose": {
      "version": "test", "artifact": "docker-compose-test", "url": "file://$BUNDLE_DEPS/docker-compose-test",
      "signature_url": "https://example.test/docker-compose-test.sigstore.json",
      "signer_identity_regexp": "^test$",
      "sha256": "$(test_hash "$BUNDLE_DEPS/docker-compose-test")", "bytes": $(test_bytes "$BUNDLE_DEPS/docker-compose-test"), "format": "executable"
    }
  }}
}
EOF
BUNDLE="$TEST_HOME/bundles/lifecycle.tar.gz"
SOVEREIGN_ENGINE_ARTIFACT_DIR="$BUNDLE_DEPS" \
SOVEREIGN_HOME="$TEST_HOME" "$TEST_HOME/current/deploy/scripts/create-offline-bundle.sh" --no-pull --output "$BUNDLE"
[[ -s "$BUNDLE" && -s "$BUNDLE.json" ]]
tar -tzf "$BUNDLE" > "$TEST_HOME/bundle-files.txt"
grep -q 'installer-dependencies/cosign-test$' "$TEST_HOME/bundle-files.txt"
grep -q 'installer-dependencies/colima-disk.raw.gz$' "$TEST_HOME/bundle-files.txt"
grep -q 'installer-dependencies/docker-compose-test.sigstore.json$' "$TEST_HOME/bundle-files.txt"

OFFLINE_HOME="$TEST_HOME/offline-install"
OFFLINE_BLOCKERS="$TEST_HOME/offline-blockers"
NETWORK_LOG="$TEST_HOME/offline-network.log"
LIFECYCLE_ENGINE_LOG="$TEST_HOME/offline-engine.log"
mkdir -p "$OFFLINE_BLOCKERS"
cat > "$OFFLINE_BLOCKERS/curl" <<'SH'
#!/usr/bin/env bash
for argument in "$@"; do
  case "$argument" in
    http://127.0.0.1:*) exit 0 ;;
    http://localhost:*) exit 0 ;;
    https://*|http://*) printf '%s\n' "$argument" >> "${NETWORK_LOG:?}"; exit 97 ;;
  esac
done
exit 0
SH
chmod 755 "$OFFLINE_BLOCKERS/curl"
: > "$NETWORK_LOG"
: > "$LIFECYCLE_ENGINE_LOG"

# An ambient engine that answers basic Docker calls but fails a full
# compatibility probe is left untouched and replaced with the isolated
# managed provider, including while offline.
FALLBACK_HOME="$TEST_HOME/incompatible-engine-install"
SOVEREIGN_HOME="$FALLBACK_HOME" \
SOVEREIGN_BIN_DIR="$FALLBACK_HOME/bin" \
SOVEREIGN_INCLUDE_MODELS=0 \
SOVEREIGN_SKIP_START=1 \
SOVEREIGN_TEST_DOCKER_ARCH=amd64 \
SOVEREIGN_ENGINE_PLATFORM=Darwin-arm64 \
SOVEREIGN_COLIMA_HOME="$TEST_HOME/fallback-colima" \
NETWORK_LOG="$NETWORK_LOG" \
LIFECYCLE_ENGINE_LOG="$LIFECYCLE_ENGINE_LOG" \
PATH="$OFFLINE_BLOCKERS:$PATH" \
  "$ROOT/deploy/scripts/install.sh" --version "$VERSION" --profile metal-arm64 --offline-bundle "$BUNDLE"
grep -q '^provider=managed-colima$' "$FALLBACK_HOME/state/container-engine.env"
[[ ! -s "$NETWORK_LOG" ]]
SOVEREIGN_HOME="$FALLBACK_HOME" LIFECYCLE_ENGINE_LOG="$LIFECYCLE_ENGINE_LOG" \
  "$FALLBACK_HOME/current/deploy/scripts/uninstall.sh" --purge --yes >/dev/null
[[ ! -e "$FALLBACK_HOME" ]]

SOVEREIGN_HOME="$OFFLINE_HOME" \
SOVEREIGN_BIN_DIR="$OFFLINE_HOME/bin" \
SOVEREIGN_INCLUDE_MODELS=0 \
SOVEREIGN_SKIP_START=1 \
SOVEREIGN_ENGINE_PREFER_MANAGED=1 \
SOVEREIGN_ENGINE_PLATFORM=Darwin-arm64 \
SOVEREIGN_COLIMA_HOME="$TEST_HOME/offline-colima" \
NETWORK_LOG="$NETWORK_LOG" \
LIFECYCLE_ENGINE_LOG="$LIFECYCLE_ENGINE_LOG" \
PATH="$OFFLINE_BLOCKERS:$PATH" \
  "$ROOT/deploy/scripts/install.sh" --version "$VERSION" --profile metal-arm64 --offline-bundle "$BUNDLE"
[[ -f "$OFFLINE_HOME/state/offline" ]]
[[ -x "$OFFLINE_HOME/runtime-dist/$VERSION/agent-dist/install-agent.sh" ]]
grep -q '^provider=managed-colima$' "$OFFLINE_HOME/state/container-engine.env"
[[ ! -s "$NETWORK_LOG" ]]

# Purge first renders the exact owned VM and data scope, then requires a
# separate confirmation. The confirmed path deletes only that managed profile.
if SOVEREIGN_HOME="$OFFLINE_HOME" LIFECYCLE_ENGINE_LOG="$LIFECYCLE_ENGINE_LOG" \
  "$OFFLINE_HOME/current/deploy/scripts/uninstall.sh" --purge > "$TEST_HOME/purge-preview.log" 2>&1; then
  echo "uninstall accepted purge without explicit confirmation" >&2
  exit 1
fi
grep -q 'managed Colima VM: sovereign' "$TEST_HOME/purge-preview.log"
[[ -d "$OFFLINE_HOME" ]]
SOVEREIGN_HOME="$OFFLINE_HOME" LIFECYCLE_ENGINE_LOG="$LIFECYCLE_ENGINE_LOG" \
  "$OFFLINE_HOME/current/deploy/scripts/uninstall.sh" --purge --yes
[[ ! -e "$OFFLINE_HOME" ]]
grep -q 'colima delete sovereign --force' "$LIFECYCLE_ENGINE_LOG"

mkdir -p "$HOME/Library/LaunchAgents"
touch "$HOME/Library/LaunchAgents/com.lazarus.sovereign-runtime-agent.plist"
SOVEREIGN_HOME="$TEST_HOME" "$TEST_HOME/current/deploy/scripts/uninstall.sh"
[[ -f "$TEST_HOME/config/runtime.yaml" ]]
[[ ! -e "$TEST_HOME/current" ]]
[[ -f "$HOME/Library/LaunchAgents/com.lazarus.sovereign-runtime-agent.plist" ]]
echo "installer lifecycle passed"
