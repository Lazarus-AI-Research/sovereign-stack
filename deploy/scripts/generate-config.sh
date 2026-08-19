#!/usr/bin/env bash
# Render a complete, owner-only appliance environment without prompting.
set -Eeuo pipefail

SOVEREIGN_HOME="${SOVEREIGN_HOME:-$HOME/.sovereign}"
PROFILE="${SOVEREIGN_PROFILE:-}"
VERSION="${SOVEREIGN_VERSION:-0.1.0-rc.6}"
RELEASE_ROOT="${SOVEREIGN_RELEASE_ROOT:-$SOVEREIGN_HOME/current}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --home) SOVEREIGN_HOME="$2"; shift 2 ;;
    --profile) PROFILE="$2"; shift 2 ;;
    --version) VERSION="${2#v}"; shift 2 ;;
    --release-root) RELEASE_ROOT="$2"; shift 2 ;;
    *) echo "unknown option: $1" >&2; exit 2 ;;
  esac
done

[[ "$PROFILE" == "cuda-x86_64" || "$PROFILE" == "metal-arm64" ]] || {
  echo "error: --profile must be cuda-x86_64 or metal-arm64" >&2
  exit 2
}

umask 077
mkdir -p "$SOVEREIGN_HOME/secrets/litellm" "$SOVEREIGN_HOME/logs" \
  "$SOVEREIGN_HOME/reports" "$SOVEREIGN_HOME/backups" "$SOVEREIGN_HOME/bundles" \
  "$SOVEREIGN_HOME/models" "$SOVEREIGN_HOME/state"

ENV_FILE="$SOVEREIGN_HOME/.env"
existing() {
  local key="$1"
  [[ -f "$ENV_FILE" ]] || return 0
  sed -n "s/^${key}=//p" "$ENV_FILE" | tail -n 1
}
random_hex() { openssl rand -hex "$1"; }
keep_or_generate() {
  local key="$1" bytes="$2" value
  value="$(existing "$key")"
  [[ -n "$value" && "$value" != change-me* ]] || value="$(random_hex "$bytes")"
  printf '%s' "$value"
}

POSTGRES_PASSWORD="$(keep_or_generate POSTGRES_PASSWORD 24)"
LITELLM_MASTER_KEY="$(existing LITELLM_MASTER_KEY)"
[[ -n "$LITELLM_MASTER_KEY" && "$LITELLM_MASTER_KEY" != sk-change-me* ]] || LITELLM_MASTER_KEY="sk-$(random_hex 24)"
RUNTIME_KEY="$(keep_or_generate SOVEREIGN_RUNTIME_API_KEY 32)"
PROXY_TOKEN="$(keep_or_generate DOCKER_PROXY_TOKEN 32)"
INDEX_TOKEN="$(keep_or_generate WORKSPACE_INDEX_ADMIN_TOKEN 32)"
OPERATOR_TOKEN="$(keep_or_generate SOVEREIGN_OPERATOR_TOKEN 32)"
HOSTD_TOKEN_FILE="$SOVEREIGN_HOME/state/hostd-token"
HOSTD_TOKEN=""
[[ -s "$HOSTD_TOKEN_FILE" ]] && HOSTD_TOKEN="$(<"$HOSTD_TOKEN_FILE")"
[[ -n "$HOSTD_TOKEN" ]] || HOSTD_TOKEN="$(existing SOVEREIGN_HOSTD_TOKEN)"
[[ -n "$HOSTD_TOKEN" && "$HOSTD_TOKEN" != change-me* ]] || HOSTD_TOKEN="$(random_hex 32)"
WORKSPACE_JWT_SECRET="$(keep_or_generate WORKSPACE_JWT_SECRET 32)"
ACCESS_MODE="${SOVEREIGN_ACCESS_MODE:-$(existing SOVEREIGN_ACCESS_MODE)}"
ACCESS_MODE="${ACCESS_MODE:-desktop}"
BIND_ADDRESS="${SOVEREIGN_BIND_ADDRESS:-$(existing SOVEREIGN_BIND_ADDRESS)}"
SITE_ADDRESS="${SOVEREIGN_SITE_ADDRESS:-$(existing SOVEREIGN_SITE_ADDRESS)}"
INSTALLED_HTTP_PORT="$(existing HTTP_PORT)"
INSTALLED_HTTP_PORT="${INSTALLED_HTTP_PORT:-${SOVEREIGN_HTTP_PORT:-54854}}"
INSTALLED_HTTPS_PORT="$(existing HTTPS_PORT)"
INSTALLED_HTTPS_PORT="${INSTALLED_HTTPS_PORT:-${SOVEREIGN_HTTPS_PORT:-8443}}"
PUBLIC_URL="${SOVEREIGN_PUBLIC_URL:-$(existing SOVEREIGN_PUBLIC_URL)}"
INSECURE_WAN_ACK="${SOVEREIGN_INSECURE_WAN_ACK:-$(existing SOVEREIGN_INSECURE_WAN_ACK)}"
case "$ACCESS_MODE" in
  desktop)
    BIND_ADDRESS="${BIND_ADDRESS:-127.0.0.1}"
    SITE_ADDRESS="${SITE_ADDRESS:-:80}"
    PUBLIC_URL="${PUBLIC_URL:-http://127.0.0.1:$INSTALLED_HTTP_PORT}"
    ;;
  lan)
    if [[ -z "$BIND_ADDRESS" ]]; then
      LAN_CANDIDATES="$(hostname -I 2>/dev/null || true)"
      [[ -n "$LAN_CANDIDATES" ]] || LAN_CANDIDATES="$(ipconfig getifaddr en0 2>/dev/null || true)"
      for candidate in $LAN_CANDIDATES; do
        if printf '%s\n' "$candidate" | awk -F. 'NF==4 && ($1==10 || ($1==192 && $2==168) || ($1==172 && $2>=16 && $2<=31)) {ok=1} END {exit !ok}'; then
          BIND_ADDRESS="$candidate"; break
        fi
      done
    fi
    [[ -n "$BIND_ADDRESS" ]] || { echo "error: LAN access requested but no private IPv4 address was found" >&2; exit 2; }
    SITE_ADDRESS="${SITE_ADDRESS:-:80}"
    PUBLIC_URL="${PUBLIC_URL:-http://$BIND_ADDRESS:$INSTALLED_HTTP_PORT}"
    ;;
  domain)
    [[ -n "$SITE_ADDRESS" && "$SITE_ADDRESS" != :80 ]] || { echo "error: domain access requires SOVEREIGN_SITE_ADDRESS" >&2; exit 2; }
    BIND_ADDRESS="${BIND_ADDRESS:-0.0.0.0}"
    [[ -n "$(existing HTTP_PORT)" || -n "${SOVEREIGN_HTTP_PORT:-}" ]] || INSTALLED_HTTP_PORT=80
    [[ -n "$(existing HTTPS_PORT)" || -n "${SOVEREIGN_HTTPS_PORT:-}" ]] || INSTALLED_HTTPS_PORT=443
    PUBLIC_URL="${PUBLIC_URL:-https://$SITE_ADDRESS}"
    ;;
  wan-http)
    [[ "$INSECURE_WAN_ACK" == "I-understand-cleartext-http-is-insecure" ]] || {
      echo "error: wan-http requires SOVEREIGN_INSECURE_WAN_ACK=I-understand-cleartext-http-is-insecure" >&2; exit 2
    }
    BIND_ADDRESS="${BIND_ADDRESS:-0.0.0.0}"
    SITE_ADDRESS="${SITE_ADDRESS:-:80}"
    [[ -n "$PUBLIC_URL" ]] || { echo "error: wan-http requires SOVEREIGN_PUBLIC_URL" >&2; exit 2; }
    ;;
  *) echo "error: unknown SOVEREIGN_ACCESS_MODE $ACCESS_MODE" >&2; exit 2 ;;
esac

AGENT_TOKEN_FILE="$SOVEREIGN_HOME/agent.token"
if [[ ! -f "$AGENT_TOKEN_FILE" ]]; then
  random_hex 32 > "$AGENT_TOKEN_FILE"
fi
chmod 600 "$AGENT_TOKEN_FILE"
AGENT_TOKEN="$(<"$AGENT_TOKEN_FILE")"

VAULT_KEY_FILE="$SOVEREIGN_HOME/secrets/control-vault.key"
if [[ ! -f "$VAULT_KEY_FILE" ]]; then
  random_hex 32 > "$VAULT_KEY_FILE"
fi
chmod 600 "$VAULT_KEY_FILE"

printf '%s\n' "$HOSTD_TOKEN" > "$HOSTD_TOKEN_FILE"
chmod 600 "$HOSTD_TOKEN_FILE"

MANIFEST_FILE="$RELEASE_ROOT/release/manifest.json"
IMAGE_LOCK_FILE="$RELEASE_ROOT/release/images.env"
RUNTIME_VERSION="$VERSION"
if [[ -f "$MANIFEST_FILE" ]]; then
  MANIFEST_RUNTIME_VERSION="$(sed -n 's/.*"runtime_version": *"\([^"]*\)".*/\1/p' "$MANIFEST_FILE" | head -n 1)"
  [[ -z "$MANIFEST_RUNTIME_VERSION" ]] || RUNTIME_VERSION="$MANIFEST_RUNTIME_VERSION"
  [[ "$RUNTIME_VERSION" =~ ^0\.1\.0(-rc\.[0-9]+)?$ ]] || {
    echo "error: release manifest has an invalid runtime_version" >&2
    exit 1
  }
fi

CONTROL_IMAGE="ghcr.io/lazarus-ai-research/sovereign-control:$VERSION"
DOCKER_PROXY_IMAGE="ghcr.io/lazarus-ai-research/sovereign-docker-proxy:$VERSION"
EVALS_IMAGE="ghcr.io/lazarus-ai-research/sovereign-evals:$VERSION"
WORKSPACE_IMAGE="ghcr.io/lazarus-ai-research/sovereign-workspace:$VERSION"
EMBEDDINGS_IMAGE=""
if [[ "$PROFILE" == "metal-arm64" ]]; then
  EMBEDDINGS_BASE_URL="http://host.docker.internal:42666/v1"
  RUNTIME_IMAGE="ghcr.io/lazarus-ai-research/sovereign-runtime:metal-arm64-$RUNTIME_VERSION"
else
  EMBEDDINGS_BASE_URL="http://sovereign-embeddings:42666/v1"
  EMBEDDINGS_IMAGE="ghcr.io/lazarus-ai-research/sovereign-embeddings:$VERSION"
  RUNTIME_IMAGE="ghcr.io/lazarus-ai-research/sovereign-runtime:cuda-x86_64-$RUNTIME_VERSION"
fi
EMBEDDING_ALIAS="embedding-gemma-default"
PASSAGE_PREFIX="title: none | text: "
QUERY_PREFIX="task: search result | query: "

locked_image() {
  local key="$1" expected="$2" value digest
  value="$(sed -n "s/^${key}=//p" "$IMAGE_LOCK_FILE" | tail -n 1)"
  [[ "$value" == "$expected@sha256:"* ]] || {
    echo "error: $key is missing or inconsistent with release $VERSION" >&2
    return 1
  }
  digest="${value##*@sha256:}"
  [[ ${#digest} -eq 64 && "$digest" != *[!0-9a-f]* ]] || {
    echo "error: $key has an invalid digest" >&2
    return 1
  }
  printf '%s' "$value"
}
if [[ -f "$MANIFEST_FILE" ]]; then
  [[ -f "$IMAGE_LOCK_FILE" ]] || {
    echo "error: signed release manifest has no image lock" >&2
    exit 1
  }
  CONTROL_IMAGE="$(locked_image SOVEREIGN_CONTROL_IMAGE "$CONTROL_IMAGE")"
  DOCKER_PROXY_IMAGE="$(locked_image SOVEREIGN_DOCKER_PROXY_IMAGE "$DOCKER_PROXY_IMAGE")"
  EVALS_IMAGE="$(locked_image SOVEREIGN_EVALS_IMAGE "$EVALS_IMAGE")"
  WORKSPACE_IMAGE="$(locked_image SOVEREIGN_WORKSPACE_IMAGE "$WORKSPACE_IMAGE")"
  if [[ "$PROFILE" == "metal-arm64" ]]; then
    RUNTIME_IMAGE="$(locked_image SOVEREIGN_RUNTIME_METAL_IMAGE "$RUNTIME_IMAGE")"
  else
    RUNTIME_IMAGE="$(locked_image SOVEREIGN_RUNTIME_CUDA_IMAGE "$RUNTIME_IMAGE")"
    EMBEDDINGS_IMAGE="$(locked_image SOVEREIGN_EMBEDDINGS_IMAGE "$EMBEDDINGS_IMAGE")"
  fi
fi

TMP="$ENV_FILE.tmp.$$"
cat > "$TMP" <<EOF
# Generated by SovereignStack $VERSION. Owner-readable only.
SOVEREIGN_VERSION=$VERSION
SOVEREIGN_PROFILE=$PROFILE
SOVEREIGN_RELEASE_ROOT=$RELEASE_ROOT
SOVEREIGN_ACCESS_MODE=$ACCESS_MODE
SOVEREIGN_BIND_ADDRESS=$BIND_ADDRESS
SOVEREIGN_SITE_ADDRESS=$SITE_ADDRESS
SOVEREIGN_PUBLIC_URL=$PUBLIC_URL
SOVEREIGN_INSECURE_WAN_ACK=$INSECURE_WAN_ACK
SOVEREIGN_HOST_UID=$(id -u)
SOVEREIGN_HOST_GID=$(id -g)
SOVEREIGN_HOST_OS=${SOVEREIGN_HOST_OS:-$(uname -s | tr '[:upper:]' '[:lower:]')}
SOVEREIGN_HOST_ARCH=${SOVEREIGN_HOST_ARCH:-$(uname -m)}
SOVEREIGN_HOST_MEMORY_BYTES=${SOVEREIGN_HOST_MEMORY_BYTES:-}
SOVEREIGN_GPU_NAME=${SOVEREIGN_GPU_NAME:-}
SOVEREIGN_GPU_VRAM_MIB=${SOVEREIGN_GPU_VRAM_MIB:-}
SOVEREIGN_CUDA_GPU_INDEX=${SOVEREIGN_CUDA_GPU_INDEX:-}
HTTP_PORT=$INSTALLED_HTTP_PORT
HTTPS_PORT=$INSTALLED_HTTPS_PORT

SOVEREIGN_CONTROL_IMAGE=$CONTROL_IMAGE
SOVEREIGN_DOCKER_PROXY_IMAGE=$DOCKER_PROXY_IMAGE
SOVEREIGN_EVALS_IMAGE=$EVALS_IMAGE
SOVEREIGN_WORKSPACE_IMAGE=$WORKSPACE_IMAGE
SOVEREIGN_RUNTIME_IMAGE=$RUNTIME_IMAGE
SOVEREIGN_EMBEDDINGS_IMAGE=$EMBEDDINGS_IMAGE
SOVEREIGN_EMBEDDINGS_BASE_URL=$EMBEDDINGS_BASE_URL

CADDY_IMAGE=caddy@sha256:af5fdcd76f2db5e4e974ee92f96ee8c0fc3edb55bd4ba5032547cbf3f65e486d
LITELLM_IMAGE=ghcr.io/berriai/litellm@sha256:f46d672e9d12a84e5dde046ff3865e0bf323e49f9f9b8eb08cd33c1713ea9627
PGVECTOR_IMAGE=pgvector/pgvector@sha256:1d533553fefe4f12e5d80c7b80622ba0c382abb5758856f52983d8789179f0fb
PHOENIX_IMAGE=arizephoenix/phoenix@sha256:0826e5c6247d012bf8cfb58df5dd9778b26ebb7f9b1afac577a8e0bb3d83606f
PROMETHEUS_IMAGE=prom/prometheus@sha256:3c42b892cf723fa54d2f262c37a0e1f80aa8c8ddb1da7b9b0df9455a35a7f893
GRAFANA_IMAGE=grafana/grafana@sha256:121a7a9ece6dc10b969f1f96eed64b4f07dfac0d0b8abc070f7cb83bbde86f63
LOKI_IMAGE=grafana/loki@sha256:70b9f699fc9bb868b62f1cfd4f787dfa50242f1fd92e6089787d5d7daea75fe8
OTEL_IMAGE=otel/opentelemetry-collector-contrib@sha256:125bdbeb7590cc1952c5b3430ecf14063568980c2c93d5b38676cc0446ed8108

STORAGE_DIR=/app/server/storage
DISABLE_TELEMETRY=true
LLM_PROVIDER=generic-openai
GENERIC_OPEN_AI_BASE_PATH=http://sovereign-gateway:4000/v1
GENERIC_OPEN_AI_API_KEY=$LITELLM_MASTER_KEY
GENERIC_OPEN_AI_MODEL_PREF=assistant-large
GENERIC_OPEN_AI_MODEL_TOKEN_LIMIT=2048
EMBEDDING_ENGINE=generic-openai
EMBEDDING_BASE_PATH=http://sovereign-gateway:4000/v1
GENERIC_OPEN_AI_EMBEDDING_API_KEY=$LITELLM_MASTER_KEY
EMBEDDING_MODEL_PREF=$EMBEDDING_ALIAS
GENERIC_OPEN_AI_EMBEDDING_PASSAGE_PREFIX="$PASSAGE_PREFIX"
GENERIC_OPEN_AI_EMBEDDING_QUERY_PREFIX="$QUERY_PREFIX"
VECTOR_DB=pgvector
PGVECTOR_CONNECTION_STRING=postgresql://sovereign:$POSTGRES_PASSWORD@postgres:5432/vectors

POSTGRES_USER=sovereign
POSTGRES_PASSWORD=$POSTGRES_PASSWORD
CONTROL_DATABASE_URL=postgres://sovereign:$POSTGRES_PASSWORD@postgres:5432/sovereign_control?sslmode=disable
DATABASE_URL=postgres://sovereign:$POSTGRES_PASSWORD@postgres:5432/litellm
PHOENIX_SQL_DATABASE_URL=postgresql://sovereign:$POSTGRES_PASSWORD@postgres:5432/phoenix
LITELLM_MASTER_KEY=$LITELLM_MASTER_KEY
SOVEREIGN_OPERATOR_TOKEN=$OPERATOR_TOKEN
SOVEREIGN_HOSTD_TOKEN_FILE=/sovereign/state/hostd-token
SOVEREIGN_HOSTD_URL=http://host.docker.internal:9191
WORKSPACE_JWT_SECRET=$WORKSPACE_JWT_SECRET
SOVEREIGN_RUNTIME_API_KEY=$RUNTIME_KEY
DOCKER_PROXY_TOKEN=$PROXY_TOKEN
WORKSPACE_INDEX_ADMIN_TOKEN=$INDEX_TOKEN
SOVEREIGN_AGENT_TOKEN=$AGENT_TOKEN
SOVEREIGN_VAULT_KEY_FILE=/sovereign/secrets/control-vault.key
RUNTIME_CONFIG_PATH=/sovereign/config/runtime.yaml
LITELLM_CONFIG_PATH=/sovereign/secrets/litellm/config.yaml
LITELLM_CONFIG_HOST_PATH=./secrets/litellm/config.yaml
DEPLOY_ROOT=$SOVEREIGN_HOME
HF_TOKEN=${HF_TOKEN:-$(existing HF_TOKEN)}
HF_HOME=/models/hf
EOF
chmod 600 "$TMP"
mv "$TMP" "$ENV_FILE"

printf '%s\n' "$PROFILE" > "$SOVEREIGN_HOME/state/profile"
chmod 600 "$SOVEREIGN_HOME/state/profile"
printf '%s\n' "$ACCESS_MODE" > "$SOVEREIGN_HOME/state/access-mode"
chmod 600 "$SOVEREIGN_HOME/state/access-mode"

GATEWAY_CONFIG="$SOVEREIGN_HOME/secrets/litellm/config.yaml"
if [[ ! -s "$GATEWAY_CONFIG" ]]; then
  cp "$RELEASE_ROOT/deploy/config/litellm/config.yaml" "$GATEWAY_CONFIG"
  chmod 600 "$GATEWAY_CONFIG"
fi

echo "generated $ENV_FILE for $PROFILE"
