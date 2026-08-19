#!/usr/bin/env bash
# Reconcile the privileged Ubuntu host dependencies required by the CUDA
# profile. The main installer invokes this narrow helper through sudo/pkexec;
# appliance data and credentials remain owned by the unprivileged user.
set -Eeuo pipefail

OWNER=""
OFFLINE=0
REBOOT_ONLY=0
PROVISION_ROOT="${SOVEREIGN_PROVISION_ROOT:-}"
TEST_MODE="${SOVEREIGN_PROVISION_TEST:-0}"
DOCKER_KEY_FINGERPRINT=9DC858229FC7DD38854AE2D88D81803C0EBFCD88
NVIDIA_KEY_FINGERPRINT=C95B321B61E88C1809C4F759DDCAE044F796ECB0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --owner) OWNER="$2"; shift 2 ;;
    --offline) OFFLINE=1; shift ;;
    --reboot) REBOOT_ONLY=1; shift ;;
    *) echo "error: unknown option: $1" >&2; exit 2 ;;
  esac
done

die() { echo "error: Ubuntu provisioning: $*" >&2; exit 1; }
root_path() { printf '%s%s' "$PROVISION_ROOT" "$1"; }

if (( EUID != 0 )) && [[ "$TEST_MODE" != 1 ]]; then
  die "administrator privileges are required"
fi
if (( REBOOT_ONLY == 1 )); then
  command -v systemctl >/dev/null 2>&1 || die "systemctl is unavailable"
  exec systemctl reboot
fi

[[ "$OWNER" =~ ^[a-zA-Z_][a-zA-Z0-9_.-]*[$]?$ ]] || die "a valid appliance owner is required"
if [[ "$TEST_MODE" == 1 ]]; then
  OWNER_UID="${SOVEREIGN_PROVISION_OWNER_UID:-1000}"
  OWNER_GID="${SOVEREIGN_PROVISION_OWNER_GID:-1000}"
else
  OWNER_UID="$(id -u "$OWNER" 2>/dev/null)" || die "owner $OWNER does not exist"
  OWNER_GID="$(id -g "$OWNER" 2>/dev/null)" || die "owner $OWNER has no primary group"
  (( OWNER_UID >= 1000 && OWNER_UID < 60000 )) || die "owner $OWNER is not an ordinary login user"
fi

OS_RELEASE="${SOVEREIGN_OS_RELEASE:-$(root_path /etc/os-release)}"
[[ -r "$OS_RELEASE" ]] || die "cannot read $OS_RELEASE"
OS_ID="$(. "$OS_RELEASE" && printf '%s' "${ID:-}")"
OS_VERSION_ID="$(. "$OS_RELEASE" && printf '%s' "${VERSION_ID:-}")"
OS_CODENAME="$(. "$OS_RELEASE" && printf '%s' "${UBUNTU_CODENAME:-${VERSION_CODENAME:-}}")"
[[ "$OS_ID" == ubuntu && "$OS_VERSION_ID" == 24.04 ]] || die "Ubuntu 24.04 is required"
OS_CODENAME="${OS_CODENAME:-noble}"
[[ "$OS_CODENAME" == noble ]] || die "unexpected Ubuntu codename $OS_CODENAME"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=hardware.sh
source "$SCRIPT_DIR/hardware.sh"
sovereign_has_nvidia_display_device || die "no NVIDIA display or 3D controller was found on PCIe"

docker_ready() {
  command -v docker >/dev/null 2>&1 &&
    docker --context default info >/dev/null 2>&1 &&
    docker --context default compose version >/dev/null 2>&1
}

driver_loaded() {
  command -v nvidia-smi >/dev/null 2>&1 && nvidia-smi -L >/dev/null 2>&1
}

toolkit_ready() {
  command -v nvidia-ctk >/dev/null 2>&1 && docker_ready &&
    docker --context default info --format '{{json .Runtimes}}' 2>/dev/null | grep -qi nvidia
}

owner_in_docker_group() {
  if [[ "$TEST_MODE" == 1 ]]; then
    [[ "${SOVEREIGN_PROVISION_OWNER_IN_DOCKER:-0}" == 1 ]]
  else
    id -nG "$OWNER" 2>/dev/null | tr ' ' '\n' | grep -qx docker
  fi
}

NEED_DRIVER=0
NEED_ENGINE=0
NEED_TOOLKIT=0
NEED_MEMBERSHIP=0
driver_loaded || NEED_DRIVER=1
docker_ready || NEED_ENGINE=1
toolkit_ready || NEED_TOOLKIT=1
owner_in_docker_group || NEED_MEMBERSHIP=1

RESULT_DIR="$(root_path /var/lib/sovereign-stack)"
RESULT_FILE="$RESULT_DIR/provision-$OWNER_UID.env"
write_result() {
  local changed="$1" reboot_required="$2" managed_docker="$3" temporary boot_id
  if [[ -n "${SOVEREIGN_PROVISION_BOOT_ID:-}" ]]; then
    boot_id="$SOVEREIGN_PROVISION_BOOT_ID"
  elif [[ -r /proc/sys/kernel/random/boot_id ]]; then
    boot_id="$(</proc/sys/kernel/random/boot_id)"
  else
    boot_id=unknown
  fi
  [[ "$boot_id" =~ ^[A-Za-z0-9_.-]+$ ]] || die "the system boot identifier is invalid"
  mkdir -p "$RESULT_DIR"
  temporary="$RESULT_FILE.tmp.$$"
  {
    printf 'schema_version=1\n'
    printf 'owner_uid=%s\n' "$OWNER_UID"
    printf 'changed=%s\n' "$changed"
    printf 'reboot_required=%s\n' "$reboot_required"
    printf 'managed_docker=%s\n' "$managed_docker"
    printf 'boot_id=%s\n' "$boot_id"
    printf 'completed_at=%s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
  } > "$temporary"
  chmod 644 "$temporary"
  mv "$temporary" "$RESULT_FILE"
  printf 'result_file=%s\nchanged=%s\nreboot_required=%s\nmanaged_docker=%s\n' \
    "$RESULT_FILE" "$changed" "$reboot_required" "$managed_docker"
}

if (( NEED_DRIVER == 0 && NEED_ENGINE == 0 && NEED_TOOLKIT == 0 && NEED_MEMBERSHIP == 0 )); then
  write_result 0 0 0
  exit 0
fi
(( OFFLINE == 0 )) || die "the offline bundle does not contain Ubuntu driver and container packages"

for command in apt-get curl; do
  command -v "$command" >/dev/null 2>&1 || die "$command is required for authenticated package installation"
done

TEMP_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/sovereign-ubuntu-provision.XXXXXX")"
trap 'rm -rf "$TEMP_ROOT"' EXIT

echo "Updating Ubuntu package metadata..."
apt_get() {
  env DEBIAN_FRONTEND=noninteractive apt-get \
    -o "DPkg::Lock::Timeout=${SOVEREIGN_APT_LOCK_TIMEOUT:-900}" "$@"
}
apt_get update
BASE_PACKAGES=(ca-certificates curl gnupg)
(( NEED_DRIVER == 0 )) || BASE_PACKAGES+=(ubuntu-drivers-common)
apt_get install -y --no-install-recommends "${BASE_PACKAGES[@]}"
command -v gpg >/dev/null 2>&1 || die "the authenticated gnupg package did not provide gpg"

install_repository_key() {
  local url="$1" fingerprint="$2" destination="$3" downloaded actual destination_dir
  downloaded="$TEMP_ROOT/$(basename "$destination").download"
  destination_dir="$(dirname "$(root_path "$destination")")"
  mkdir -p "$destination_dir"
  curl -fsSL --proto '=https' --tlsv1.2 --retry 4 -o "$downloaded" "$url"
  actual="$(gpg --batch --show-keys --with-colons "$downloaded" | awk -F: '$1 == "fpr" {print toupper($10); exit}')"
  [[ "$actual" == "$fingerprint" ]] || die "repository key fingerprint mismatch for $url"
  gpg --batch --yes --dearmor --output "$(root_path "$destination")" "$downloaded"
  chmod 644 "$(root_path "$destination")"
}

if (( NEED_ENGINE == 1 )); then
  echo "Configuring Docker's authenticated Ubuntu repository..."
  install_repository_key https://download.docker.com/linux/ubuntu/gpg \
    "$DOCKER_KEY_FINGERPRINT" /etc/apt/keyrings/sovereign-stack-docker.gpg
  mkdir -p "$(root_path /etc/apt/sources.list.d)"
  printf 'deb [arch=amd64 signed-by=/etc/apt/keyrings/sovereign-stack-docker.gpg] https://download.docker.com/linux/ubuntu %s stable\n' \
    "$OS_CODENAME" > "$(root_path /etc/apt/sources.list.d/sovereign-stack-docker.list)"
fi

if (( NEED_TOOLKIT == 1 )); then
  echo "Configuring NVIDIA's authenticated container-toolkit repository..."
  install_repository_key https://nvidia.github.io/libnvidia-container/gpgkey \
    "$NVIDIA_KEY_FINGERPRINT" /usr/share/keyrings/sovereign-stack-nvidia-container-toolkit.gpg
  mkdir -p "$(root_path /etc/apt/sources.list.d)"
  printf 'deb [arch=amd64 signed-by=/usr/share/keyrings/sovereign-stack-nvidia-container-toolkit.gpg] https://nvidia.github.io/libnvidia-container/stable/deb/amd64 /\n' \
    > "$(root_path /etc/apt/sources.list.d/sovereign-stack-nvidia-container-toolkit.list)"
fi

if (( NEED_ENGINE == 1 || NEED_TOOLKIT == 1 )); then
  apt_get update
fi
if (( NEED_ENGINE == 1 )); then
  echo "Installing Docker Engine and Compose..."
  apt_get install -y --no-install-recommends \
    docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
fi
if (( NEED_DRIVER == 1 )); then
  echo "Installing Ubuntu's recommended signed NVIDIA compute driver..."
  env DEBIAN_FRONTEND=noninteractive ubuntu-drivers install --gpgpu
fi
if (( NEED_TOOLKIT == 1 )); then
  echo "Installing and configuring NVIDIA Container Toolkit..."
  apt_get install -y --no-install-recommends nvidia-container-toolkit
  nvidia-ctk runtime configure --runtime=docker
fi

systemctl enable --now docker
if (( NEED_TOOLKIT == 1 )); then
  systemctl restart docker
fi
if ! owner_in_docker_group; then
  usermod -aG docker "$OWNER"
  NEED_MEMBERSHIP=1
fi

REBOOT_REQUIRED=0
if (( NEED_DRIVER == 1 || NEED_MEMBERSHIP == 1 )); then
  REBOOT_REQUIRED=1
fi
if [[ -e "$(root_path /var/run/reboot-required)" ]]; then
  REBOOT_REQUIRED=1
fi
if (( REBOOT_REQUIRED == 1 )); then
  mkdir -p "$(root_path /var/run)"
  if [[ ! -e "$(root_path /var/run/reboot-required)" ]]; then
    printf '*** System restart required ***\n' > "$(root_path /var/run/reboot-required)"
  fi
  printf 'sovereign-stack-installer\n' >> "$(root_path /var/run/reboot-required.pkgs)"
  command -v loginctl >/dev/null 2>&1 || die "loginctl is required for reboot-safe resume"
  loginctl enable-linger "$OWNER"
fi

write_result 1 "$REBOOT_REQUIRED" "$NEED_ENGINE"
