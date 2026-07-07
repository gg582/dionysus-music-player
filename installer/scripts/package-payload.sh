#!/bin/bash
set -euo pipefail
# ---------------------------------------------------------------------------
# package-payload.sh
# Convert platform build artifacts into tar.gz payloads consumed by the
# browser-based gozik installer.
#
# Usage: ./installer/scripts/package-payload.sh <input-dir> <output-dir>
#
# Expected inputs (produced by the release workflow):
#   gozik-amd64.AppImage
#   gozik-linux-arm64
#   gozik-linux-riscv64
#   gozik-windows-amd64.zip
#   gozik-windows-arm64.zip
#   gozik-macos-arm64.dmg
#
# Outputs:
#   gozik-payload-linux-amd64.tar.gz
#   gozik-payload-linux-arm64.tar.gz
#   gozik-payload-linux-riscv64.tar.gz
#   gozik-payload-windows-amd64.tar.gz
#   gozik-payload-windows-arm64.tar.gz
#   gozik-payload-darwin-arm64.tar.gz
# ---------------------------------------------------------------------------

PROJECT_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
INPUT_DIR="${1:-}"
OUTPUT_DIR="${2:-}"

if [[ -z "$INPUT_DIR" || -z "$OUTPUT_DIR" ]]; then
  echo "Usage: $0 <input-dir> <output-dir>" >&2
  exit 1
fi

INPUT_DIR="$(cd "$INPUT_DIR" && pwd)"
mkdir -p "$OUTPUT_DIR"
OUTPUT_DIR="$(cd "$OUTPUT_DIR" && pwd)"

cd "$PROJECT_ROOT"

workdir="$(mktemp -d)"
trap 'rm -rf "$workdir"' EXIT

package_linux_amd64() {
  local appimage="$INPUT_DIR/gozik-amd64.AppImage"
  if [[ ! -f "$appimage" ]]; then
    echo "Skipping linux/amd64 payload: gozik-amd64.AppImage not found"
    return 0
  fi
  local stage="$workdir/linux-amd64"
  mkdir -p "$stage"
  cp -L "$appimage" "$stage/gozik-amd64.AppImage"
  chmod +x "$stage/gozik-amd64.AppImage"
  # Include a loose icon so the installer can install it without having to
  # extract the AppImage (which may fail when FUSE is unavailable).
  if [[ -f "$PROJECT_ROOT/gozik.png" ]]; then
    cp "$PROJECT_ROOT/gozik.png" "$stage/gozik.png"
  fi
  tar -czf "$OUTPUT_DIR/gozik-payload-linux-amd64.tar.gz" -C "$stage" .
  echo "Created gozik-payload-linux-amd64.tar.gz"
}

package_linux_raw() {
  local arch="$1"
  local binary="$INPUT_DIR/gozik-linux-${arch}"
  if [[ ! -f "$binary" ]]; then
    echo "Skipping linux/${arch} payload: gozik-linux-${arch} not found"
    return 0
  fi
  local stage="$workdir/linux-${arch}"
  mkdir -p "$stage"
  cp "$binary" "$stage/gozik"
  chmod +x "$stage/gozik"

  if [[ -d "$PROJECT_ROOT/assets" ]]; then
    cp -R "$PROJECT_ROOT/assets" "$stage/assets"
  fi
  if [[ -f "$PROJECT_ROOT/gozik.png" ]]; then
    mkdir -p "$stage/assets/icons/hicolor/256x256/apps"
    cp "$PROJECT_ROOT/gozik.png" "$stage/assets/icons/hicolor/256x256/apps/gozik.png"
  fi

  tar -czf "$OUTPUT_DIR/gozik-payload-linux-${arch}.tar.gz" -C "$stage" .
  echo "Created gozik-payload-linux-${arch}.tar.gz"
}

package_windows() {
  local arch="$1"
  local zip="$INPUT_DIR/gozik-windows-${arch}.zip"
  if [[ ! -f "$zip" ]]; then
    echo "Skipping windows/${arch} payload: gozik-windows-${arch}.zip not found"
    return 0
  fi
  local stage="$workdir/windows-${arch}"
  mkdir -p "$stage"
  unzip -q "$zip" -d "$stage"
  # Flatten a single top-level directory if present.
  local top
  top="$(find "$stage" -mindepth 1 -maxdepth 1 -type d | head -n1 || true)"
  if [[ -n "$top" && -z "$(find "$stage" -mindepth 1 -maxdepth 1 -not -type d)" ]]; then
    mv "$top"/* "$stage/" 2>/dev/null || true
    rmdir "$top" 2>/dev/null || true
  fi
  tar -czf "$OUTPUT_DIR/gozik-payload-windows-${arch}.tar.gz" -C "$stage" .
  echo "Created gozik-payload-windows-${arch}.tar.gz"
}

package_darwin() {
  local dmg="$INPUT_DIR/gozik-macos-arm64.dmg"
  if [[ ! -f "$dmg" ]]; then
    echo "Skipping darwin/arm64 payload: gozik-macos-arm64.dmg not found"
    return 0
  fi
  local stage="$workdir/darwin-arm64"
  mkdir -p "$stage"

  local mountpoint="$workdir/mnt"
  mkdir -p "$mountpoint"

  # Attach the DMG read-only and locate the .app bundle.
  local dev
  dev="$(hdiutil attach -readonly -nobrowse -mountpoint "$mountpoint" "$dmg" | awk 'END {print $1}')"
  if [[ -z "$dev" ]]; then
    echo "Failed to mount DMG: $dmg" >&2
    return 1
  fi
  trap 'hdiutil detach "$dev" >/dev/null 2>&1 || true; rm -rf "$workdir"' EXIT

  local app
  app="$(find "$mountpoint" -maxdepth 1 -name '*.app' -type d | head -n1)"
  if [[ -z "$app" ]]; then
    echo "No .app bundle found inside DMG" >&2
    return 1
  fi

  cp -R "$app" "$stage/"
  hdiutil detach "$dev" >/dev/null 2>&1 || true
  # Restore the original trap.
  trap 'rm -rf "$workdir"' EXIT

  tar -czf "$OUTPUT_DIR/gozik-payload-darwin-arm64.tar.gz" -C "$stage" .
  echo "Created gozik-payload-darwin-arm64.tar.gz"
}

package_linux_amd64
package_linux_raw arm64
package_linux_raw riscv64
package_windows amd64
package_windows arm64
package_darwin

echo ""
echo "Payloads written to ${OUTPUT_DIR}/"
ls -lh "$OUTPUT_DIR"
