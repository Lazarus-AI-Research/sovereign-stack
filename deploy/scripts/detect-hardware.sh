#!/usr/bin/env bash
# Detect only the two hardware profiles certified for v0.1.0.
set -Eeuo pipefail

MIN_BYTES=$((32 * 1024 * 1024 * 1024))
MIN_VRAM_MIB=$((24 * 1024))
JSON=false
[[ "${1:-}" == "--json" ]] && JSON=true

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=hardware.sh
source "$SCRIPT_DIR/hardware.sh"

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
OS_RELEASE="${SOVEREIGN_OS_RELEASE:-/etc/os-release}"
[[ -r "$OS_RELEASE" ]] || fail "cannot determine Linux distribution"
# shellcheck disable=SC1091
. "$OS_RELEASE"
[[ "${ID:-}" == ubuntu && "${VERSION_ID:-}" == 24.04 ]] || fail "CUDA v0.1.0 requires Ubuntu 24.04"
sovereign_has_nvidia_display_device || fail "no NVIDIA display or 3D controller was found on PCIe"
if DETAILS="$(sovereign_nvidia_gpu_details)"; then
  IFS=$'\t' read -r GPU_INDEX VRAM NAME <<< "$DETAILS"
  [[ "$VRAM" =~ ^[0-9]+$ ]] || fail "could not read NVIDIA GPU memory"
  (( VRAM >= MIN_VRAM_MIB )) || fail "the largest NVIDIA GPU requires at least 24GB VRAM"
  if $JSON; then
    printf '{"supported":true,"profile":"cuda-x86_64","driver_ready":true,"gpu_index":%s,"gpu":"%s","vram_mib":%s}\n' \
      "$GPU_INDEX" "${NAME//\"/\\\"}" "$VRAM"
  else
    echo cuda-x86_64
  fi
elif $JSON; then
  printf '{"supported":true,"profile":"cuda-x86_64","driver_ready":false,"nvidia_pci_devices":%s}\n' \
    "$(sovereign_nvidia_display_devices | awk 'END {print NR + 0}')"
else
  echo cuda-x86_64
fi
