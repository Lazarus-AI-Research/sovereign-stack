#!/usr/bin/env bash
set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TEST_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/sovereign-resume-test.XXXXXX")"
trap 'rm -rf "$TEST_ROOT"' EXIT
export HOME="$TEST_ROOT/user-home"
export SOVEREIGN_HOME="$TEST_ROOT/appliance"
export SOVEREIGN_BIN_DIR="$TEST_ROOT/bin"
export SOVEREIGN_SYSFS_ROOT="$TEST_ROOT/sys"
export SOVEREIGN_TEST_CUDA_PROBE_IMAGE="example.test/runtime@sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
export SOVEREIGN_TEST_DOCKER_LOG="$TEST_ROOT/docker.log"
mkdir -p "$HOME" "$SOVEREIGN_SYSFS_ROOT/bus/pci/devices/0000:01:00.0" "$TEST_ROOT/mock-bin"
printf '0x10de\n' > "$SOVEREIGN_SYSFS_ROOT/bus/pci/devices/0000:01:00.0/vendor"
printf '0x030200\n' > "$SOVEREIGN_SYSFS_ROOT/bus/pci/devices/0000:01:00.0/class"

RESULT_FILE="$TEST_ROOT/provision-result.env"
PROVISIONER="$TEST_ROOT/provisioner"
cat > "$PROVISIONER" <<'SH'
#!/usr/bin/env bash
set -Eeuo pipefail
uid="$(id -u)"
{
  printf 'schema_version=1\nowner_uid=%s\nchanged=1\nreboot_required=1\nmanaged_docker=1\n' "$uid"
  printf 'boot_id=%s\n' "${SOVEREIGN_TEST_BOOT_ID:?}"
  printf 'completed_at=2026-08-18T00:00:00Z\n'
} > "${RESUME_RESULT_FILE:?}"
chmod 644 "$RESUME_RESULT_FILE"
printf 'result_file=%s\nchanged=1\nreboot_required=1\nmanaged_docker=1\n' "$RESUME_RESULT_FILE"
SH
cat > "$TEST_ROOT/mock-bin/sudo" <<'SH'
#!/usr/bin/env bash
exec "$@"
SH
chmod 755 "$PROVISIONER" "$TEST_ROOT/mock-bin/sudo"
export PATH="$TEST_ROOT/mock-bin:$ROOT/tests/fixtures/bin:$PATH"
TEST_SOURCE="$TEST_ROOT/source"
mkdir -p "$TEST_SOURCE"
for item in deploy release schemas docs api VERSION LICENSE NOTICE THIRD_PARTY_NOTICES.md; do
  [[ ! -e "$ROOT/$item" ]] || cp -R "$ROOT/$item" "$TEST_SOURCE/$item"
done
cp "$PROVISIONER" "$TEST_SOURCE/deploy/scripts/provision-ubuntu.sh"
chmod 755 "$TEST_SOURCE/deploy/scripts/provision-ubuntu.sh"

set +e
RESUME_RESULT_FILE="$RESULT_FILE" \
SOVEREIGN_TEST_NVIDIA_READY=0 \
SOVEREIGN_TEST_BOOT_ID=boot-a \
SOVEREIGN_TEST_NO_REBOOT=1 \
SOVEREIGN_TEST_UNAME_S=Linux \
SOVEREIGN_TEST_UNAME_M=x86_64 \
SOVEREIGN_OS_RELEASE="$ROOT/tests/fixtures/os-release" \
SOVEREIGN_SOURCE_DIR="$TEST_SOURCE" \
SOVEREIGN_INCLUDE_MODELS=0 \
SOVEREIGN_SKIP_START=1 \
  "$ROOT/deploy/scripts/install.sh" --profile cuda-x86_64 > "$TEST_ROOT/first.log" 2>&1
FIRST_STATUS=$?
set -e
[[ "$FIRST_STATUS" == 194 ]] || { cat "$TEST_ROOT/first.log" >&2; exit 1; }
grep -qx 'stage=awaiting-reboot' "$SOVEREIGN_HOME/state/install-journal.env"
[[ -x "$SOVEREIGN_HOME/bootstrap/resume.sh" ]]
[[ -f "$SOVEREIGN_HOME/bootstrap/release/deploy/scripts/install.sh" ]]
UNIT_FILE="$(<"$SOVEREIGN_HOME/state/install-resume-unit")"
[[ -f "$UNIT_FILE" && -L "$(dirname "$UNIT_FILE")/default.target.wants/$(basename "$UNIT_FILE")" ]]

# Simulate the first boot with the driver, group membership, Docker, Compose,
# and NVIDIA runtime now active. The exact saved command completes and removes
# only its install-scoped resume unit.
set +e
RESUME_RESULT_FILE="$RESULT_FILE" \
SOVEREIGN_UBUNTU_PROVISION_RESULT="$RESULT_FILE" \
SOVEREIGN_TEST_NVIDIA_READY=1 \
SOVEREIGN_TEST_BOOT_ID=boot-b \
SOVEREIGN_TEST_DOCKER_ARCH=amd64 \
SOVEREIGN_TEST_UNAME_S=Linux \
SOVEREIGN_TEST_UNAME_M=x86_64 \
SOVEREIGN_OS_RELEASE="$ROOT/tests/fixtures/os-release" \
SOVEREIGN_INCLUDE_MODELS=0 \
SOVEREIGN_SKIP_START=1 \
  "$SOVEREIGN_HOME/bootstrap/resume.sh" > "$TEST_ROOT/resume.log" 2>&1
RESUME_STATUS=$?
set -e
if (( RESUME_STATUS != 0 )); then
  cat "$TEST_ROOT/resume.log" >&2
  exit "$RESUME_STATUS"
fi
grep -qx 'stage=complete' "$SOVEREIGN_HOME/state/install-journal.env"
grep -q '^provider=managed-docker$' "$SOVEREIGN_HOME/state/container-engine.env"
grep -q '^docker_context=default$' "$SOVEREIGN_HOME/state/container-engine.env"
grep -q -- '--gpus device=0 --entrypoint python3 .*torch.cuda.is_available' "$SOVEREIGN_TEST_DOCKER_LOG"
[[ ! -e "$UNIT_FILE" ]]
[[ ! -e "$SOVEREIGN_HOME/bootstrap" ]]
[[ "$(readlink "$SOVEREIGN_HOME/current")" == "$SOVEREIGN_HOME/releases/$(<"$ROOT/VERSION")" ]]

echo "Ubuntu reboot/resume tests passed"
