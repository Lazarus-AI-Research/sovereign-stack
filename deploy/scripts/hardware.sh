#!/usr/bin/env bash
# Shared, side-effect-free hardware detection helpers. This file is sourced by
# both the profile detector and the installer; it must never install packages.

SOVEREIGN_MIN_CUDA_VRAM_MIB="${SOVEREIGN_MIN_CUDA_VRAM_MIB:-24576}"

sovereign_nvidia_sysfs_root() {
  printf '%s' "${SOVEREIGN_SYSFS_ROOT:-/sys}"
}

sovereign_nvidia_display_devices() {
  local root device vendor class found=1
  root="$(sovereign_nvidia_sysfs_root)"
  for device in "$root"/bus/pci/devices/*; do
    [[ -d "$device" && -r "$device/vendor" && -r "$device/class" ]] || continue
    vendor="$(tr '[:upper:]' '[:lower:]' < "$device/vendor" | tr -d '[:space:]')"
    class="$(tr '[:upper:]' '[:lower:]' < "$device/class" | tr -d '[:space:]')"
    if [[ "$vendor" == 0x10de && "$class" == 0x03* ]]; then
      printf '%s\n' "$(basename "$device")"
      found=0
    fi
  done
  return "$found"
}

sovereign_has_nvidia_display_device() {
  sovereign_nvidia_display_devices >/dev/null 2>&1
}

# Print the best visible GPU as: index<TAB>VRAM MiB<TAB>name. Selection is by
# VRAM rather than nvidia-smi row order so a small display adapter cannot hide
# a certified compute GPU.
sovereign_nvidia_gpu_details() {
  command -v nvidia-smi >/dev/null 2>&1 || return 1
  nvidia-smi --query-gpu=index,name,memory.total --format=csv,noheader,nounits 2>/dev/null | \
    awk -F, '
      {
        index_value=$1; name=$2; memory=$3
        gsub(/^[[:space:]]+|[[:space:]]+$/, "", index_value)
        gsub(/^[[:space:]]+|[[:space:]]+$/, "", name)
        gsub(/^[[:space:]]+|[[:space:]]+$/, "", memory)
        if (index_value ~ /^[0-9]+$/ && memory ~ /^[0-9]+$/ && memory + 0 > best + 0) {
          best=memory; best_index=index_value; best_name=name
        }
      }
      END {
        if (best_index == "") exit 1
        printf "%s\t%s\t%s\n", best_index, best, best_name
      }
    '
}

sovereign_nvidia_driver_ready() {
  local details vram
  details="$(sovereign_nvidia_gpu_details)" || return 1
  IFS=$'\t' read -r _ vram _ <<< "$details"
  [[ "$vram" =~ ^[0-9]+$ && "$vram" -ge "$SOVEREIGN_MIN_CUDA_VRAM_MIB" ]]
}
