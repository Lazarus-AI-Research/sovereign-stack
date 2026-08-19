#!/usr/bin/env bash
# Build and inspect the unsigned native macOS package from reviewed inputs.
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
for command in pkgbuild pkgutil; do
  command -v "$command" >/dev/null 2>&1 || {
    echo "error: $command is required to build the macOS package" >&2
    exit 1
  }
done
mkdir -p "$OUTPUT_DIR"
case "$OUTPUT_DIR" in /*) ;; *) OUTPUT_DIR="$PWD/$OUTPUT_DIR" ;; esac

STAGE="$(mktemp -d "${TMPDIR:-/tmp}/sovereign-macos-pkg.XXXXXX")"
trap 'rm -rf "$STAGE"' EXIT
PAYLOAD="$STAGE/root"
EXPANDED="$STAGE/expanded"
mkdir -p "$PAYLOAD/usr/local/bin" "$PAYLOAD/usr/local/share/sovereign-stack"
cp "$ROOT/deploy/scripts/install.sh" "$PAYLOAD/usr/local/share/sovereign-stack/install.sh"
cp "$ROOT/packaging/macos/launch-install.command" \
  "$PAYLOAD/usr/local/share/sovereign-stack/launch-install.command"
printf '%s\n' "$VERSION" > "$PAYLOAD/usr/local/share/sovereign-stack/VERSION"
cp "$ROOT/packaging/sovereign-install" "$PAYLOAD/usr/local/bin/sovereign-install"
chmod 755 "$PAYLOAD/usr/local/bin/sovereign-install" \
  "$PAYLOAD/usr/local/share/sovereign-stack/install.sh" \
  "$PAYLOAD/usr/local/share/sovereign-stack/launch-install.command"

OUTPUT="$OUTPUT_DIR/SovereignStack-$VERSION-arm64-unsigned.pkg"
[[ ! -e "$OUTPUT" ]] || {
  echo "error: output package already exists: $OUTPUT" >&2
  exit 1
}
pkgbuild --root "$PAYLOAD" --scripts "$ROOT/packaging/macos" \
  --identifier ai.lazarus.sovereign-stack.installer --version "$VERSION" \
  "$OUTPUT" >&2

# Inspect the actual package, not just its staging tree.
pkgutil --expand-full "$OUTPUT" "$EXPANDED"
test -x "$EXPANDED/Payload/usr/local/bin/sovereign-install"
test -x "$EXPANDED/Payload/usr/local/share/sovereign-stack/install.sh"
test -x "$EXPANDED/Payload/usr/local/share/sovereign-stack/launch-install.command"
test -x "$EXPANDED/Scripts/postinstall"
bash -n "$EXPANDED/Scripts/postinstall" \
  "$EXPANDED/Payload/usr/local/share/sovereign-stack/install.sh" \
  "$EXPANDED/Payload/usr/local/share/sovereign-stack/launch-install.command"

echo "$OUTPUT"
