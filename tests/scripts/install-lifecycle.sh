#!/usr/bin/env bash
set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TEST_HOME="$(mktemp -d "${TMPDIR:-/tmp}/sovereign-install-test.XXXXXX")"
trap 'rm -rf "$TEST_HOME"' EXIT
mkdir -p "$TEST_HOME/user-home"
export HOME="$TEST_HOME/user-home"
export PATH="$ROOT/tests/fixtures/bin:$PATH"
VERSION="$(<"$ROOT/VERSION")"

SOVEREIGN_HOME="$TEST_HOME" \
SOVEREIGN_BIN_DIR="$TEST_HOME/bin" \
SOVEREIGN_SOURCE_DIR="$ROOT" \
SOVEREIGN_INCLUDE_MODELS=0 \
SOVEREIGN_SKIP_START=1 \
  "$ROOT/deploy/scripts/install.sh" --profile metal-arm64

SOVEREIGN_HOME="$TEST_HOME" "$TEST_HOME/bin/sovereign" validate
if SOVEREIGN_TEST_DOCKER_OWNER=/another/install \
  SOVEREIGN_HOME="$TEST_HOME" "$TEST_HOME/bin/sovereign" up >"$TEST_HOME/takeover.log" 2>&1; then
  echo "sovereign up accepted a foreign Compose project" >&2
  exit 1
fi
grep -q "belongs to another SovereignStack installation" "$TEST_HOME/takeover.log"
for file in .env credentials agent.token secrets/control-vault.key; do
  mode="$(if stat -f %Lp "$TEST_HOME/$file" >/dev/null 2>&1; then stat -f %Lp "$TEST_HOME/$file"; else stat -c %a "$TEST_HOME/$file"; fi)"
  [[ "$mode" == 600 ]] || { echo "$file mode is $mode, expected 600" >&2; exit 1; }
done
grep -Eq '^SOVEREIGN_HOST_UID=[0-9]+$' "$TEST_HOME/.env"
grep -Eq '^SOVEREIGN_HOST_GID=[0-9]+$' "$TEST_HOME/.env"
[[ "$(readlink "$TEST_HOME/current")" == "$TEST_HOME/releases/$VERSION" ]]

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
SOVEREIGN_SKIP_START=1 \
  "$ROOT/deploy/scripts/install.sh" --profile cuda-x86_64
[[ "$(readlink "$CUDA_HOME/current")" == "$CUDA_HOME/releases/$VERSION" ]]
grep -qx "SOVEREIGN_VERSION=$VERSION" "$CUDA_HOME/.env"

# Exercise bundle creation and a fresh offline install without downloading
# weights. The Docker fixture emits a small stand-in image archive.
mkdir -p "$TEST_HOME/runtime-dist/$VERSION/agent-dist"
cp "$ROOT/deploy/scripts/uninstall.sh" "$TEST_HOME/runtime-dist/$VERSION/agent-dist/install-agent.sh"
chmod +x "$TEST_HOME/runtime-dist/$VERSION/agent-dist/install-agent.sh"
BUNDLE="$TEST_HOME/bundles/lifecycle.tar.gz"
SOVEREIGN_HOME="$TEST_HOME" "$TEST_HOME/current/deploy/scripts/create-offline-bundle.sh" --no-pull --output "$BUNDLE"
[[ -s "$BUNDLE" && -s "$BUNDLE.json" ]]

OFFLINE_HOME="$TEST_HOME/offline-install"
SOVEREIGN_HOME="$OFFLINE_HOME" \
SOVEREIGN_BIN_DIR="$OFFLINE_HOME/bin" \
SOVEREIGN_INCLUDE_MODELS=0 \
SOVEREIGN_SKIP_START=1 \
  "$ROOT/deploy/scripts/install.sh" --version "$VERSION" --profile metal-arm64 --offline-bundle "$BUNDLE"
[[ -f "$OFFLINE_HOME/state/offline" ]]
[[ -x "$OFFLINE_HOME/runtime-dist/$VERSION/agent-dist/install-agent.sh" ]]

mkdir -p "$HOME/Library/LaunchAgents"
touch "$HOME/Library/LaunchAgents/com.lazarus.sovereign-runtime-agent.plist"
SOVEREIGN_HOME="$TEST_HOME" "$TEST_HOME/current/deploy/scripts/uninstall.sh"
[[ -f "$TEST_HOME/config/runtime.yaml" ]]
[[ ! -e "$TEST_HOME/current" ]]
[[ -f "$HOME/Library/LaunchAgents/com.lazarus.sovereign-runtime-agent.plist" ]]
echo "installer lifecycle passed"
