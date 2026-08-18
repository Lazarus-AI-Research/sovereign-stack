#!/usr/bin/env bash
set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TEST_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/sovereign-engine-test.XXXXXX")"
trap 'rm -rf "$TEST_ROOT"' EXIT

mkdir -p "$TEST_ROOT/bin" "$TEST_ROOT/home"
export ENGINE_TEST_LOG="$TEST_ROOT/docker.log"
cat > "$TEST_ROOT/bin/docker" <<'SH'
#!/usr/bin/env bash
set -eu
printf 'config=%s args=%s\n' "${DOCKER_CONFIG:-}" "$*" >> "$ENGINE_TEST_LOG"
case "$*" in
  "context show") printf 'unrelated-context\n' ;;
  *"version --format {{.Server.APIVersion}}"*) printf '1.52\n' ;;
  *"info --format {{.Architecture}}"*) printf 'arm64\n' ;;
  *"compose version --short"*) printf '5.5.0\n' ;;
  *"df -Pk /"*) printf '99999999\n' ;;
  *" port "*" 80/tcp") printf '127.0.0.1:54321\n' ;;
  *"--mount type=bind,source="*",target=/sovereign-probe"*)
    previous=""
    for argument in "$@"; do
      if [[ "$previous" == --mount ]]; then
        value="${argument#type=bind,source=}"
        value="${value%,target=/sovereign-probe}"
        printf 'container-ok\n' > "$value/container-written"
      fi
      previous="$argument"
    done
    ;;
  *) exit 0 ;;
esac
SH
chmod +x "$TEST_ROOT/bin/docker"
cat > "$TEST_ROOT/bin/curl" <<'SH'
#!/usr/bin/env bash
exit 0
SH
cat > "$TEST_ROOT/bin/nc" <<'SH'
#!/usr/bin/env bash
sleep 30
SH
chmod +x "$TEST_ROOT/bin/curl" "$TEST_ROOT/bin/nc"

export PATH="$TEST_ROOT/bin:/usr/bin:/bin"
export SOVEREIGN_HOME="$TEST_ROOT/home/.sovereign"
# shellcheck source=../../deploy/scripts/container-engine.sh
source "$ROOT/deploy/scripts/container-engine.sh"

sovereign_engine_require
STATE="$SOVEREIGN_HOME/state/container-engine.env"
grep -q '^provider=existing$' "$STATE"
grep -q '^managed=0$' "$STATE"
grep -q '^docker_context=unrelated-context$' "$STATE"

: > "$ENGINE_TEST_LOG"
sovereign_engine_docker ps
grep -q 'args=--context unrelated-context ps$' "$ENGINE_TEST_LOG"
if grep -q 'context use' "$ENGINE_TEST_LOG"; then
  echo "engine wrapper changed the active Docker context" >&2
  exit 1
fi

CONFIG_ROOT="$TEST_ROOT/private docker config"
mkdir -p "$CONFIG_ROOT"
sovereign_engine_write_state existing 0 "$TEST_ROOT/bin/docker" managed-context "$CONFIG_ROOT"
unset SOVEREIGN_ENGINE_LOADED
sovereign_engine_load
: > "$ENGINE_TEST_LOG"
sovereign_engine_compose version
grep -q "config=$CONFIG_ROOT args=--context managed-context compose version$" "$ENGINE_TEST_LOG"

sed 's/^docker_context=.*/docker_context=invalid context/' "$STATE" > "$STATE.invalid"
mv "$STATE.invalid" "$STATE"
unset SOVEREIGN_ENGINE_LOADED
if sovereign_engine_load; then
  echo "invalid container-engine context was accepted" >&2
  exit 1
fi

# A clean Mac with no ambient Docker installs the manifest-pinned toolchain in
# its private SovereignStack directories and starts Colima without activating
# or editing the user's global Docker context.
ARTIFACTS="$TEST_ROOT/artifacts"
MANAGED_HOME="$TEST_ROOT/managed-home/.sovereign"
mkdir -p "$ARTIFACTS/lima/bin" "$ARTIFACTS/docker/docker"
cat > "$ARTIFACTS/colima-Darwin-arm64" <<'SH'
#!/usr/bin/env bash
printf 'colima args=%s config=%s home=%s\n' "$*" "${DOCKER_CONFIG:-}" "${COLIMA_HOME:-}" >> "$ENGINE_TEST_LOG"
SH
cat > "$ARTIFACTS/cosign-Darwin-arm64" <<'SH'
#!/usr/bin/env bash
exit 0
SH
printf 'test colima disk image\n' > "$ARTIFACTS/colima-disk.raw.gz"
cat > "$ARTIFACTS/lima/bin/limactl" <<'SH'
#!/usr/bin/env bash
exit 0
SH
cp "$TEST_ROOT/bin/docker" "$ARTIFACTS/docker/docker/docker"
cat > "$ARTIFACTS/docker-compose-darwin-aarch64" <<'SH'
#!/usr/bin/env bash
exit 0
SH
chmod +x "$ARTIFACTS/cosign-Darwin-arm64" "$ARTIFACTS/colima-Darwin-arm64" "$ARTIFACTS/lima/bin/limactl" \
  "$ARTIFACTS/docker/docker/docker" "$ARTIFACTS/docker-compose-darwin-aarch64"
tar -czf "$ARTIFACTS/lima.tar.gz" -C "$ARTIFACTS/lima" .
tar -czf "$ARTIFACTS/docker.tgz" -C "$ARTIFACTS/docker" .
hash_file() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | awk '{print $1}'
  else shasum -a 256 "$1" | awk '{print $1}'; fi
}
bytes_file() {
  if stat -f %z "$1" >/dev/null 2>&1; then stat -f %z "$1"
  else stat -c %s "$1"; fi
}
MANIFEST="$ARTIFACTS/manifest.json"
cat > "$MANIFEST" <<EOF
{
  "engine_probe": {
    "image": "example.test/probe@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
    "container_port": 80,
    "minimum_api_version": "1.41",
    "minimum_free_kib": 20971520
  },
  "installer_dependencies": {
    "metal-arm64": {
      "cosign": {
        "version": "test", "artifact": "cosign-Darwin-arm64",
        "url": "file://$ARTIFACTS/cosign-Darwin-arm64",
        "sha256": "$(hash_file "$ARTIFACTS/cosign-Darwin-arm64")",
        "bytes": $(bytes_file "$ARTIFACTS/cosign-Darwin-arm64"),
        "format": "executable"
      },
      "colima": {
        "version": "test", "artifact": "colima-Darwin-arm64",
        "url": "file://$ARTIFACTS/colima-Darwin-arm64",
        "sha256": "$(hash_file "$ARTIFACTS/colima-Darwin-arm64")",
        "bytes": $(bytes_file "$ARTIFACTS/colima-Darwin-arm64"),
        "format": "executable"
      },
      "colima_disk_image": {
        "version": "test", "artifact": "colima-disk.raw.gz",
        "url": "file://$ARTIFACTS/colima-disk.raw.gz",
        "sha256": "$(hash_file "$ARTIFACTS/colima-disk.raw.gz")",
        "bytes": $(bytes_file "$ARTIFACTS/colima-disk.raw.gz"),
        "format": "raw.gz"
      },
      "lima": {
        "version": "test", "artifact": "lima.tar.gz",
        "url": "file://$ARTIFACTS/lima.tar.gz",
        "sha256": "$(hash_file "$ARTIFACTS/lima.tar.gz")",
        "bytes": $(bytes_file "$ARTIFACTS/lima.tar.gz"),
        "format": "tar.gz"
      },
      "docker_cli": {
        "version": "test", "artifact": "docker.tgz",
        "url": "file://$ARTIFACTS/docker.tgz",
        "sha256": "$(hash_file "$ARTIFACTS/docker.tgz")",
        "bytes": $(bytes_file "$ARTIFACTS/docker.tgz"),
        "format": "tar.gz"
      },
      "docker_compose": {
        "version": "test", "artifact": "docker-compose-darwin-aarch64",
        "url": "file://$ARTIFACTS/docker-compose-darwin-aarch64",
        "sha256": "$(hash_file "$ARTIFACTS/docker-compose-darwin-aarch64")",
        "bytes": $(bytes_file "$ARTIFACTS/docker-compose-darwin-aarch64"),
        "format": "executable"
      }
    }
  }
}
EOF

export SOVEREIGN_HOME="$MANAGED_HOME"
export SOVEREIGN_ENGINE_PLATFORM=Darwin-arm64
export SOVEREIGN_ENGINE_PREFER_MANAGED=1
export SOVEREIGN_ENGINE_MANIFEST="$MANIFEST"
export SOVEREIGN_ENGINE_ARTIFACT_DIR="$ARTIFACTS"
export SOVEREIGN_COLIMA_HOME="$TEST_ROOT/managed-colima"
: > "$ENGINE_TEST_LOG"
unset SOVEREIGN_ENGINE_LOADED SOVEREIGN_ENGINE_READY
sovereign_engine_require
sovereign_engine_probe_compatibility "$MANIFEST" metal-arm64
MANAGED_STATE="$MANAGED_HOME/state/container-engine.env"
grep -q '^provider=managed-colima$' "$MANAGED_STATE"
grep -q '^managed=1$' "$MANAGED_STATE"
grep -q '^docker_context=colima-sovereign$' "$MANAGED_STATE"
grep -q 'colima args=start sovereign --activate=false' "$ENGINE_TEST_LOG"
grep -q -- '--disk-image .*colima-disk.raw.gz' "$ENGINE_TEST_LOG"
grep -q 'args=--context colima-sovereign info$' "$ENGINE_TEST_LOG"
grep -q 'args=--context colima-sovereign compose version$' "$ENGINE_TEST_LOG"
grep -q 'args=--context colima-sovereign volume create .*sovereign-engine-probe-' "$ENGINE_TEST_LOG"
grep -q 'args=--context colima-sovereign .*127.0.0.1::80' "$ENGINE_TEST_LOG"
grep -q 'host.docker.internal:' "$ENGINE_TEST_LOG"
if grep -q 'context use' "$ENGINE_TEST_LOG"; then
  echo "managed Colima provisioning changed the active Docker context" >&2
  exit 1
fi

# Repair reinstalls a damaged managed toolchain from verified artifacts, and
# lifecycle ownership stops and purges only the installer-owned profile.
mv "$SOVEREIGN_ENGINE_DOCKER_CLI" "$SOVEREIGN_ENGINE_DOCKER_CLI.damaged"
unset SOVEREIGN_ENGINE_LOADED SOVEREIGN_ENGINE_READY
sovereign_engine_repair "$MANIFEST" metal-arm64
[[ -x "$SOVEREIGN_ENGINE_DOCKER_CLI" ]]
sovereign_engine_stop_managed
sovereign_engine_purge_managed
grep -q 'colima args=stop sovereign' "$ENGINE_TEST_LOG"
grep -q 'colima args=delete sovereign --force' "$ENGINE_TEST_LOG"

# Shared engines never receive managed lifecycle operations.
export SOVEREIGN_HOME="$TEST_ROOT/shared-engine-home"
sovereign_engine_write_state existing 0 "$TEST_ROOT/bin/docker" unrelated-context
unset SOVEREIGN_ENGINE_LOADED SOVEREIGN_ENGINE_READY
sovereign_engine_load
: > "$ENGINE_TEST_LOG"
sovereign_engine_stop_managed
sovereign_engine_purge_managed
[[ ! -s "$ENGINE_TEST_LOG" ]]

echo "container-engine tests passed"
