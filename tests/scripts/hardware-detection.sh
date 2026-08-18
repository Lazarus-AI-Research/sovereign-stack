#!/usr/bin/env bash
set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TEST_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/sovereign-hardware-test.XXXXXX")"
trap 'rm -rf "$TEST_ROOT"' EXIT
mkdir -p "$TEST_ROOT/bin" "$TEST_ROOT/sys/bus/pci/devices/0000:01:00.0"
cp "$ROOT/tests/fixtures/bin/uname" "$TEST_ROOT/bin/uname"
chmod 755 "$TEST_ROOT/bin/uname"
printf '0x10de\n' > "$TEST_ROOT/sys/bus/pci/devices/0000:01:00.0/vendor"
printf '0x030200\n' > "$TEST_ROOT/sys/bus/pci/devices/0000:01:00.0/class"

# PCI detection selects CUDA before a driver exists and reports that driver
# provisioning remains outstanding.
RESULT="$(PATH="$TEST_ROOT/bin:/usr/bin:/bin" \
  SOVEREIGN_TEST_UNAME_S=Linux SOVEREIGN_TEST_UNAME_M=x86_64 \
  SOVEREIGN_SYSFS_ROOT="$TEST_ROOT/sys" SOVEREIGN_OS_RELEASE="$ROOT/tests/fixtures/os-release" \
  "$ROOT/deploy/scripts/detect-hardware.sh" --json)"
printf '%s\n' "$RESULT" | grep -q '"profile":"cuda-x86_64"'
printf '%s\n' "$RESULT" | grep -q '"driver_ready":false'
printf '%s\n' "$RESULT" | grep -q '"nvidia_pci_devices":1'

# A non-display NVIDIA PCI function is not enough to select the CUDA profile.
printf '0x020000\n' > "$TEST_ROOT/sys/bus/pci/devices/0000:01:00.0/class"
if PATH="$TEST_ROOT/bin:/usr/bin:/bin" \
  SOVEREIGN_TEST_UNAME_S=Linux SOVEREIGN_TEST_UNAME_M=x86_64 \
  SOVEREIGN_SYSFS_ROOT="$TEST_ROOT/sys" SOVEREIGN_OS_RELEASE="$ROOT/tests/fixtures/os-release" \
  "$ROOT/deploy/scripts/detect-hardware.sh" --json > "$TEST_ROOT/rejected.json"; then
  echo "hardware detector accepted an NVIDIA non-display PCI function" >&2
  exit 1
fi
grep -q 'no NVIDIA display or 3D controller' "$TEST_ROOT/rejected.json"

# With a loaded driver, choose the largest GPU rather than assuming row zero.
printf '0x030000\n' > "$TEST_ROOT/sys/bus/pci/devices/0000:01:00.0/class"
cat > "$TEST_ROOT/bin/nvidia-smi" <<'SH'
#!/usr/bin/env bash
if [[ " $* " == *" --query-gpu=index,name,memory.total "* ]]; then
  printf '0, NVIDIA Small Display, 8192\n1, NVIDIA Large Compute, 49152\n'
  exit 0
fi
exit 2
SH
chmod 755 "$TEST_ROOT/bin/nvidia-smi"
RESULT="$(PATH="$TEST_ROOT/bin:/usr/bin:/bin" \
  SOVEREIGN_TEST_UNAME_S=Linux SOVEREIGN_TEST_UNAME_M=x86_64 \
  SOVEREIGN_SYSFS_ROOT="$TEST_ROOT/sys" SOVEREIGN_OS_RELEASE="$ROOT/tests/fixtures/os-release" \
  "$ROOT/deploy/scripts/detect-hardware.sh" --json)"
printf '%s\n' "$RESULT" | grep -q '"driver_ready":true'
printf '%s\n' "$RESULT" | grep -q '"gpu_index":1'
printf '%s\n' "$RESULT" | grep -q '"vram_mib":49152'

echo "hardware detection tests passed"
