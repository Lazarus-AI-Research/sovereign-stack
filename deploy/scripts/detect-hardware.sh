#!/usr/bin/env bash
# Detect only the two hardware profiles certified for v0.1.0.
set -Eeuo pipefail

MIN_BYTES=$((32 * 1024 * 1024 * 1024))
MIN_VRAM_MIB=$((24 * 1024))
JSON=false
[[ "${1:-}" == "--json" ]] && JSON=true

fail() {
  if $JSON; then
    printf '{"supported":false,"error":"%s"}\n' "${1//\"/\\\"}"
  else
    echo "unsupported: $1" >&2
  fi
  exit 1
}

if [[ "$(uname -s)" == Darwin ]]; then
  [[ "$(uname -m)" == arm64 ]] || fail "v0.1.0 supports Apple Silicon Macs only"
  MEMORY="$(sysctl -n hw.memsize)"
  (( MEMORY >= MIN_BYTES )) || fail "Apple Silicon requires at least 32GB unified memory"
  if $JSON; then
    printf '{"supported":true,"profile":"metal-arm64","memory_bytes":%s}\n' "$MEMORY"
  else
    echo metal-arm64
  fi
  exit 0
fi

[[ "$(uname -s)" == Linux ]] || fail "supported operating systems are macOS and Ubuntu 24.04"
[[ "$(uname -m)" == x86_64 ]] || fail "CUDA v0.1.0 requires x86_64"
[[ -r /etc/os-release ]] || fail "cannot determine Linux distribution"
# shellcheck disable=SC1091
. /etc/os-release
[[ "${ID:-}" == ubuntu && "${VERSION_ID:-}" == 24.04 ]] || fail "CUDA v0.1.0 requires Ubuntu 24.04"
command -v nvidia-smi >/dev/null 2>&1 || fail "nvidia-smi is missing; install the NVIDIA driver"
VRAM="$(nvidia-smi --query-gpu=memory.total --format=csv,noheader,nounits -i 0 | head -n1 | tr -d ' ')"
[[ "$VRAM" =~ ^[0-9]+$ ]] || fail "could not read GPU 0 memory"
(( VRAM >= MIN_VRAM_MIB )) || fail "GPU 0 requires at least 24GB VRAM"
if $JSON; then
  NAME="$(nvidia-smi --query-gpu=name --format=csv,noheader -i 0 | head -n1)"
  printf '{"supported":true,"profile":"cuda-x86_64","gpu":"%s","vram_mib":%s}\n' "${NAME//\"/\\\"}" "$VRAM"
else
  echo cuda-x86_64
fi
