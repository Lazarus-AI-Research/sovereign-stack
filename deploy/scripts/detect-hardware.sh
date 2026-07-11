#!/usr/bin/env bash
# detect-hardware.sh — suggest a Sovereign runtime profile for this host.
# Prints one of: metal, cuda, rocm, xpu, cpu.
# TODO: distinguish dgx-spark and strix-halo variants; report device details.
set -euo pipefail

if [[ "$(uname -s)" == "Darwin" && "$(uname -m)" == "arm64" ]]; then
  echo metal
  exit 0
fi

if command -v nvidia-smi >/dev/null 2>&1; then
  echo cuda
  exit 0
fi

if [[ -e /dev/kfd ]]; then
  echo rocm
  exit 0
fi

if compgen -G '/dev/dri/renderD*' >/dev/null 2>&1 && command -v sycl-ls >/dev/null 2>&1; then
  echo xpu
  exit 0
fi

echo cpu
