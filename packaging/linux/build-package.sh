#!/usr/bin/env bash
# Build the native Ubuntu package from reviewed repository inputs.
set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
VERSION="$(<"$ROOT/VERSION")"
OUTPUT_DIR="$PWD"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --version) VERSION="${2#v}"; shift 2 ;;
    --output-dir) OUTPUT_DIR="$2"; shift 2 ;;
    *) echo "error: unknown option: $1" >&2; exit 2 ;;
  esac
done
[[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+([-+][0-9A-Za-z.-]+)?$ ]] || {
  echo "error: invalid package version $VERSION" >&2
  exit 2
}
command -v dpkg-deb >/dev/null 2>&1 || {
  echo "error: dpkg-deb is required to build the Ubuntu package" >&2
  exit 1
}
mkdir -p "$OUTPUT_DIR"
case "$OUTPUT_DIR" in /*) ;; *) OUTPUT_DIR="$PWD/$OUTPUT_DIR" ;; esac

STAGE="$(mktemp -d "${TMPDIR:-/tmp}/sovereign-deb.XXXXXX")"
trap 'rm -rf "$STAGE"' EXIT
mkdir -p "$STAGE/DEBIAN" "$STAGE/usr/local/bin" "$STAGE/usr/local/libexec" \
  "$STAGE/usr/local/share/sovereign-stack" "$STAGE/usr/lib/systemd/system"
cp "$ROOT/deploy/scripts/install.sh" "$STAGE/usr/local/share/sovereign-stack/install.sh"
cp "$ROOT/deploy/scripts/hardware.sh" "$ROOT/deploy/scripts/detect-hardware.sh" \
  "$ROOT/deploy/scripts/provision-ubuntu.sh" "$STAGE/usr/local/share/sovereign-stack/"
printf '%s\n' "$VERSION" > "$STAGE/usr/local/share/sovereign-stack/VERSION"
cp "$ROOT/packaging/sovereign-install" "$STAGE/usr/local/bin/sovereign-install"
cp "$ROOT/packaging/linux/package-install" "$STAGE/usr/local/libexec/sovereign-stack-package-install"
cp "$ROOT/packaging/linux/sovereign-stack-package-install.service" "$STAGE/usr/lib/systemd/system/"
cp "$ROOT/packaging/linux/postinst" "$STAGE/DEBIAN/postinst"
cp "$ROOT/packaging/linux/prerm" "$STAGE/DEBIAN/prerm"
chmod 755 "$STAGE/DEBIAN/postinst" "$STAGE/DEBIAN/prerm" \
  "$STAGE/usr/local/bin/sovereign-install" \
  "$STAGE/usr/local/libexec/sovereign-stack-package-install" \
  "$STAGE/usr/local/share/sovereign-stack/"*.sh
cat > "$STAGE/DEBIAN/control" <<EOF
Package: sovereign-stack-installer
Version: $VERSION
Section: utils
Priority: optional
Architecture: amd64
Maintainer: Lazarus AI Research <support@lazarusai.com>
Depends: bash, ca-certificates, curl, openssl, passwd, systemd, tar, util-linux
Description: One-click SovereignStack private AI appliance installer
EOF

OUTPUT="$OUTPUT_DIR/SovereignStack-$VERSION-amd64.deb"
dpkg-deb --build --root-owner-group "$STAGE" "$OUTPUT"
[[ "$(dpkg-deb --field "$OUTPUT" Package)" == sovereign-stack-installer ]] || {
  echo "error: built package metadata is invalid" >&2
  exit 1
}
echo "$OUTPUT"
