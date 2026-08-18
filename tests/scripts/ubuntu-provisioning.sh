#!/usr/bin/env bash
set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TEST_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/sovereign-provision-test.XXXXXX")"
trap 'rm -rf "$TEST_ROOT"' EXIT
FAKE_ROOT="$TEST_ROOT/root"
BIN="$TEST_ROOT/bin"
LOG="$TEST_ROOT/commands.log"
mkdir -p "$BIN" "$FAKE_ROOT/etc" "$FAKE_ROOT/sys/bus/pci/devices/0000:01:00.0"
cp "$ROOT/tests/fixtures/os-release" "$FAKE_ROOT/etc/os-release"
printf 'VERSION_CODENAME=noble\n' >> "$FAKE_ROOT/etc/os-release"
printf '0x10de\n' > "$FAKE_ROOT/sys/bus/pci/devices/0000:01:00.0/vendor"
printf '0x030200\n' > "$FAKE_ROOT/sys/bus/pci/devices/0000:01:00.0/class"

for command in apt-get ubuntu-drivers nvidia-ctk systemctl usermod loginctl; do
  cat > "$BIN/$command" <<'SH'
#!/usr/bin/env bash
printf '%s %s\n' "$(basename "$0")" "$*" >> "${PROVISION_LOG:?}"
exit 0
SH
  chmod 755 "$BIN/$command"
done
cat > "$BIN/docker" <<'SH'
#!/usr/bin/env bash
[[ "${PROVISION_READY:-0}" == 1 ]] || exit 1
if [[ "$*" == *'{{json .Runtimes}}'* ]]; then printf '{"nvidia":{}}\n'; fi
exit 0
SH
cat > "$BIN/nvidia-smi" <<'SH'
#!/usr/bin/env bash
[[ "${PROVISION_READY:-0}" == 1 ]] || exit 1
[[ " ${*:-} " == *" -L " ]] && echo 'GPU 0: NVIDIA Test GPU'
exit 0
SH
cat > "$BIN/curl" <<'SH'
#!/usr/bin/env bash
destination=""
previous=""
for argument in "$@"; do
  [[ "$previous" != -o ]] || destination="$argument"
  previous="$argument"
done
[[ -n "$destination" ]] || exit 2
printf '%s\n' "$*" > "$destination"
SH
cat > "$BIN/gpg" <<'SH'
#!/usr/bin/env bash
if [[ "$*" == *'--show-keys'* ]]; then
  if [[ "$*" == *docker.gpg.download* ]]; then
    printf 'fpr:::::::::9DC858229FC7DD38854AE2D88D81803C0EBFCD88:\n'
  else
    printf 'fpr:::::::::C95B321B61E88C1809C4F759DDCAE044F796ECB0:\n'
  fi
  exit 0
fi
destination=""
previous=""
for argument in "$@"; do
  [[ "$previous" != --output ]] || destination="$argument"
  previous="$argument"
done
[[ -n "$destination" ]] || exit 2
printf 'test keyring\n' > "$destination"
SH
chmod 755 "$BIN/docker" "$BIN/nvidia-smi" "$BIN/curl" "$BIN/gpg"

COMMON_ENV=(
  PATH="$BIN:/usr/bin:/bin"
  PROVISION_LOG="$LOG"
  SOVEREIGN_PROVISION_TEST=1
  SOVEREIGN_PROVISION_ROOT="$FAKE_ROOT"
  SOVEREIGN_SYSFS_ROOT="$FAKE_ROOT/sys"
  SOVEREIGN_OS_RELEASE="$FAKE_ROOT/etc/os-release"
  SOVEREIGN_PROVISION_OWNER_UID=1000
  SOVEREIGN_PROVISION_OWNER_GID=1000
  SOVEREIGN_PROVISION_BOOT_ID=test-boot
)

: > "$LOG"
env "${COMMON_ENV[@]}" "$ROOT/deploy/scripts/provision-ubuntu.sh" --owner appliance \
  > "$TEST_ROOT/result.log"
RESULT_FILE="$FAKE_ROOT/var/lib/sovereign-stack/provision-1000.env"
grep -qx 'changed=1' "$RESULT_FILE"
grep -qx 'reboot_required=1' "$RESULT_FILE"
grep -qx 'managed_docker=1' "$RESULT_FILE"
grep -qx 'boot_id=test-boot' "$RESULT_FILE"
grep -q 'ubuntu-drivers install --gpgpu' "$LOG"
grep -q 'apt-get -o DPkg::Lock::Timeout=900 install -y --no-install-recommends docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin' "$LOG"
grep -q 'apt-get -o DPkg::Lock::Timeout=900 install -y --no-install-recommends nvidia-container-toolkit' "$LOG"
grep -q 'nvidia-ctk runtime configure --runtime=docker' "$LOG"
grep -q 'usermod -aG docker appliance' "$LOG"
grep -q 'loginctl enable-linger appliance' "$LOG"
grep -qx 'deb \[arch=amd64 signed-by=/etc/apt/keyrings/sovereign-stack-docker.gpg\] https://download.docker.com/linux/ubuntu noble stable' \
  "$FAKE_ROOT/etc/apt/sources.list.d/sovereign-stack-docker.list"
grep -qx 'deb \[arch=amd64 signed-by=/usr/share/keyrings/sovereign-stack-nvidia-container-toolkit.gpg\] https://nvidia.github.io/libnvidia-container/stable/deb/amd64 /' \
  "$FAKE_ROOT/etc/apt/sources.list.d/sovereign-stack-nvidia-container-toolkit.list"
grep -qx 'sovereign-stack-installer' "$FAKE_ROOT/var/run/reboot-required.pkgs"

# A reconciled host is a no-op and does not touch apt or services.
: > "$LOG"
env "${COMMON_ENV[@]}" PROVISION_READY=1 SOVEREIGN_PROVISION_OWNER_IN_DOCKER=1 \
  "$ROOT/deploy/scripts/provision-ubuntu.sh" --owner appliance > "$TEST_ROOT/ready.log"
grep -qx 'changed=0' "$RESULT_FILE"
grep -qx 'reboot_required=0' "$RESULT_FILE"
[[ ! -s "$LOG" ]]

# Offline mode must fail closed instead of silently reaching package repos.
: > "$LOG"
if env "${COMMON_ENV[@]}" "$ROOT/deploy/scripts/provision-ubuntu.sh" --owner appliance --offline \
  > "$TEST_ROOT/offline.log" 2>&1; then
  echo "offline Ubuntu provisioner attempted to accept missing host packages" >&2
  exit 1
fi
grep -q 'offline bundle does not contain Ubuntu driver and container packages' "$TEST_ROOT/offline.log"
[[ ! -s "$LOG" ]]

echo "Ubuntu provisioning tests passed"
