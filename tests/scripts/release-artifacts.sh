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
  --schema-dir "$ROOT/schemas" \
  --output "$TEST_ROOT/generated/manifest.json" \
  --image-lock-output "$TEST_ROOT/generated/images.env"

python3 - "$TEST_ROOT/generated/manifest.json" "$TEST_ROOT/generated/images.env" \
  "$ROOT/schemas/release-manifest.schema.json" <<'PY'
import json
import sys

from jsonschema import Draft202012Validator, FormatChecker

manifest = json.load(open(sys.argv[1], encoding="utf-8"))
schema = json.load(open(sys.argv[3], encoding="utf-8"))
Draft202012Validator(schema, format_checker=FormatChecker()).validate(manifest)
runtime_version = manifest["runtime_version"]
assert manifest["schema_version"] == "1.3"
metal = manifest["metal_agent"]
assert metal["version"] == "0.1.0-rc.4"
assert metal["version"] != manifest["version"]
assert metal["artifact"] == "sovereign-metal-agent-0.1.0-rc.4-arm64.tar.gz"
assert metal["url"].endswith("/v0.1.0-rc.4/" + metal["artifact"])
assert metal["signature_url"] == metal["url"] + ".sigstore.json"
assert metal["sha256"] == "ab8eabebac94f719325ce57f901962544ad068debc7b9f274334303b2fda393d"
assert metal["bytes"] == 74820904
assert manifest["embedding_runtime"]["version"] == "0.3.1"
dependencies = manifest["installer_dependencies"]["metal-arm64"]
assert set(dependencies) == {
    "cosign", "colima", "colima_disk_image", "lima", "docker_cli", "docker_compose"
}
assert dependencies["colima"]["version"] == "0.10.3"
assert dependencies["colima_disk_image"]["sha256"] == "1fc0354f4f99734ce3886628cc7af8b0437c1a1d391b126bd09cba0df35ee53f"
assert dependencies["colima_disk_image"]["bytes"] == 332354401
assert dependencies["cosign"]["bytes"] == 139051394
assert dependencies["docker_cli"]["version"] == "29.7.2"
assert dependencies["docker_compose"]["signature_url"].endswith(".sigstore.json")
assert manifest["engine_probe"]["image"].endswith("@sha256:af5fdcd76f2db5e4e974ee92f96ee8c0fc3edb55bd4ba5032547cbf3f65e486d")
for dependency in dependencies.values():
    assert len(dependency["sha256"]) == 64
    assert dependency["bytes"] > 0
assert {schema["name"] for schema in manifest["schemas"]} >= {
    "release-manifest.schema.json", "runtime-config.schema.json"
}
assets = {asset["name"]: asset for asset in manifest["assets"]}
assert assets["embeddinggemma-darwin-arm64-metal"]["sha256"] == "c110806fcb22514c43bb237865340fec94d14d8de8466eeed7b5d288c58ce8b5"
images = {image["name"]: image for image in manifest["images"] if image["first_party"]}
assert images["sovereign-runtime-cuda"]["reference"].endswith(f"cuda-x86_64-{runtime_version}")
assert images["sovereign-runtime-metal"]["reference"].endswith(f"metal-arm64-{runtime_version}")
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

# A release source from before runtime_version was introduced remains valid and
# keeps the historical same-version behavior.
python3 - "$ROOT/release/release-source.json" "$TEST_ROOT/legacy-source.json" <<'PY'
import json
import sys

source = json.load(open(sys.argv[1], encoding="utf-8"))
source.pop("runtime_version", None)
with open(sys.argv[2], "w", encoding="utf-8") as output:
    json.dump(source, output)
PY
python3 "$ROOT/release/generate_manifest.py" \
  --source "$TEST_ROOT/legacy-source.json" \
  --digest-dir "$TEST_ROOT/digests" \
  --stack-commit 0000000000000000000000000000000000000000 \
  --schema-dir "$ROOT/schemas" \
  --output "$TEST_ROOT/generated/legacy-manifest.json"
python3 - "$TEST_ROOT/generated/legacy-manifest.json" <<'PY'
import json
import sys

manifest = json.load(open(sys.argv[1], encoding="utf-8"))
assert manifest["runtime_version"] == manifest["version"]
runtime_images = [image for image in manifest["images"] if image["name"].startswith("sovereign-runtime-")]
assert all(image["reference"].endswith(manifest["version"]) for image in runtime_images)
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

# Only the required OpenAI-compatible operations proxy to LiteLLM. A wildcard
# here would also expose its management API and administration UI.
grep -q '@scoped_openai path /api/openai/v1/models /api/openai/v1/chat/completions /api/openai/v1/embeddings' \
  "$ROOT/deploy/config/caddy/Caddyfile"
grep -A3 'handle @scoped_openai' "$ROOT/deploy/config/caddy/Caddyfile" | \
  grep -q 'reverse_proxy sovereign-gateway:4000'
! grep -Fq 'handle_path /api/openai/*' "$ROOT/deploy/config/caddy/Caddyfile"
grep -A2 'handle /api/openai/\*' "$ROOT/deploy/config/caddy/Caddyfile" | grep -q 'respond "not found" 404'

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
