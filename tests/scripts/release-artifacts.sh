#!/usr/bin/env bash
set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TEST_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/sovereign-release-artifacts.XXXXXX")"
trap 'rm -rf "$TEST_ROOT"' EXIT
VERSION="$(<"$ROOT/VERSION")"

mkdir -p "$TEST_ROOT/digests" "$TEST_ROOT/generated"
index=1
for name in sovereign-control sovereign-docker-proxy sovereign-evals \
  sovereign-workspace sovereign-runtime-cuda sovereign-runtime-metal sovereign-embeddings; do
  printf 'sha256:%064x\n' "$index" > "$TEST_ROOT/digests/$name"
  index=$((index + 1))
done

python3 "$ROOT/release/generate_manifest.py" \
  --source "$ROOT/release/release-source.json" \
  --digest-dir "$TEST_ROOT/digests" \
  --stack-commit 0000000000000000000000000000000000000000 \
  --output "$TEST_ROOT/generated/manifest.json" \
  --image-lock-output "$TEST_ROOT/generated/images.env"

python3 - "$TEST_ROOT/generated/manifest.json" "$TEST_ROOT/generated/images.env" <<'PY'
import json
import sys

manifest = json.load(open(sys.argv[1], encoding="utf-8"))
assets = {asset["name"]: asset for asset in manifest["assets"]}
assert assets["embeddinggemma-darwin-arm64-metal"]["sha256"] == "c110806fcb22514c43bb237865340fec94d14d8de8466eeed7b5d288c58ce8b5"
images = {image["name"]: image for image in manifest["images"] if image["first_party"]}
names = {
    "SOVEREIGN_CONTROL_IMAGE": "sovereign-control",
    "SOVEREIGN_DOCKER_PROXY_IMAGE": "sovereign-docker-proxy",
    "SOVEREIGN_EVALS_IMAGE": "sovereign-evals",
    "SOVEREIGN_WORKSPACE_IMAGE": "sovereign-workspace",
    "SOVEREIGN_RUNTIME_CUDA_IMAGE": "sovereign-runtime-cuda",
    "SOVEREIGN_RUNTIME_METAL_IMAGE": "sovereign-runtime-metal",
    "SOVEREIGN_EMBEDDINGS_IMAGE": "sovereign-embeddings",
}
actual = dict(line.split("=", 1) for line in open(sys.argv[2], encoding="utf-8").read().splitlines())
expected = {
    key: f"{images[name]['reference']}@{images[name]['digest']}"
    for key, name in names.items()
}
assert actual == expected
PY

mkdir -p "$TEST_ROOT/release-root/release"
cp -R "$ROOT/deploy" "$TEST_ROOT/release-root/deploy"
cp "$TEST_ROOT/generated/manifest.json" "$TEST_ROOT/release-root/release/manifest.json"
cp "$TEST_ROOT/generated/images.env" "$TEST_ROOT/release-root/release/images.env"

for profile in metal-arm64 cuda-x86_64; do
  home="$TEST_ROOT/$profile"
  [[ "$profile" == metal-arm64 ]] && overlay=metal || overlay=cuda
  SOVEREIGN_HOME="$home" \
  SOVEREIGN_PROFILE="$profile" \
  SOVEREIGN_VERSION="$VERSION" \
  SOVEREIGN_RELEASE_ROOT="$TEST_ROOT/release-root" \
    "$ROOT/deploy/scripts/generate-config.sh"
  for key in SOVEREIGN_CONTROL_IMAGE SOVEREIGN_DOCKER_PROXY_IMAGE \
    SOVEREIGN_EVALS_IMAGE SOVEREIGN_WORKSPACE_IMAGE SOVEREIGN_RUNTIME_IMAGE; do
    grep -Eq "^$key=ghcr.io/lazarus-ai-research/[^@]+@sha256:[0-9a-f]{64}$" "$home/.env"
  done
  docker compose \
    --project-directory "$home" \
    --env-file "$home/.env" \
    -f "$TEST_ROOT/release-root/deploy/compose/compose.yml" \
    -f "$TEST_ROOT/release-root/deploy/compose/compose.runtime.$overlay.yml" \
    --profile tools config > "$home/compose.yml"
  for image in sovereign-control sovereign-docker-proxy sovereign-evals \
    sovereign-workspace sovereign-runtime; do
    grep -Eq "image: ghcr.io/lazarus-ai-research/$image:[^ ]+@sha256:[0-9a-f]{64}$" \
      "$home/compose.yml"
  done
  if [[ "$profile" == cuda-x86_64 ]]; then
    grep -Eq '^SOVEREIGN_EMBEDDINGS_IMAGE=ghcr.io/lazarus-ai-research/[^@]+@sha256:[0-9a-f]{64}$' "$home/.env"
    grep -Eq 'image: ghcr.io/lazarus-ai-research/sovereign-embeddings:[^ ]+@sha256:[0-9a-f]{64}$' \
      "$home/compose.yml"
  else
    grep -qx 'SOVEREIGN_EMBEDDINGS_IMAGE=' "$home/.env"
  fi
done

mv "$TEST_ROOT/release-root/release/images.env" "$TEST_ROOT/release-root/release/images.env.saved"
if SOVEREIGN_HOME="$TEST_ROOT/missing-lock" \
  SOVEREIGN_PROFILE=metal-arm64 \
  SOVEREIGN_VERSION="$VERSION" \
  SOVEREIGN_RELEASE_ROOT="$TEST_ROOT/release-root" \
    "$ROOT/deploy/scripts/generate-config.sh" >/dev/null 2>&1; then
  echo "configuration accepted a signed manifest without an image lock" >&2
  exit 1
fi

echo "release manifest image lock passed"
