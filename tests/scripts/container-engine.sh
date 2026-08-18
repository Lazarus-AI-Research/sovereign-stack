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
  *) exit 0 ;;
esac
SH
chmod +x "$TEST_ROOT/bin/docker"

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
cat > "$ARTIFACTS/lima/bin/limactl" <<'SH'
#!/usr/bin/env bash
exit 0
SH
cat > "$ARTIFACTS/docker/docker/docker" <<'SH'
#!/usr/bin/env bash
printf 'managed-docker config=%s args=%s\n' "${DOCKER_CONFIG:-}" "$*" >> "$ENGINE_TEST_LOG"
exit 0
SH
cat > "$ARTIFACTS/docker-compose-darwin-aarch64" <<'SH'
#!/usr/bin/env bash
exit 0
SH
chmod +x "$ARTIFACTS/colima-Darwin-arm64" "$ARTIFACTS/lima/bin/limactl" \
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
  "installer_dependencies": {
    "metal-arm64": {
      "colima": {
        "version": "test", "artifact": "colima-Darwin-arm64",
        "url": "file://$ARTIFACTS/colima-Darwin-arm64",
        "sha256": "$(hash_file "$ARTIFACTS/colima-Darwin-arm64")",
        "bytes": $(bytes_file "$ARTIFACTS/colima-Darwin-arm64"),
        "format": "executable"
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
export SOVEREIGN_COLIMA_HOME="$TEST_ROOT/managed-colima"
: > "$ENGINE_TEST_LOG"
unset SOVEREIGN_ENGINE_LOADED SOVEREIGN_ENGINE_READY
sovereign_engine_require
MANAGED_STATE="$MANAGED_HOME/state/container-engine.env"
grep -q '^provider=managed-colima$' "$MANAGED_STATE"
grep -q '^managed=1$' "$MANAGED_STATE"
grep -q '^docker_context=colima-sovereign$' "$MANAGED_STATE"
grep -q 'colima args=start sovereign --activate=false' "$ENGINE_TEST_LOG"
grep -q 'managed-docker .*args=--context colima-sovereign info$' "$ENGINE_TEST_LOG"
grep -q 'managed-docker .*args=--context colima-sovereign compose version$' "$ENGINE_TEST_LOG"
if grep -q 'context use' "$ENGINE_TEST_LOG"; then
  echo "managed Colima provisioning changed the active Docker context" >&2
  exit 1
fi

echo "container-engine tests passed"
