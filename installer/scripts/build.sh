#!/bin/bash
set -euo pipefail
# Cross-compile gozik installer binaries for all supported platforms.
# Usage: ./installer/scripts/build.sh [output-dir]

PROJECT_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
INSTALLER_DIR="${PROJECT_ROOT}/installer"
OUTPUT_DIR="${1:-${INSTALLER_DIR}/dist}"

mkdir -p "$OUTPUT_DIR"

cd "$INSTALLER_DIR"

# Targets supported by the payload assets published on GitHub Releases.
TARGETS=(
  "windows amd64 .exe"
  "windows arm64 .exe"
  "darwin arm64 "
  "linux amd64 "
  "linux arm64 "
  "linux riscv64 "
)

for spec in "${TARGETS[@]}"; do
  read -r goos goarch suffix <<< "$spec"
  name="$goos"
  if [ "$goos" = "darwin" ]; then
    name="macos"
  fi
  output="gozik-installer-${name}-${goarch}${suffix}"
  echo "Building $output ..."
  env GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=0 \
    go build -ldflags="-s -w" -o "${OUTPUT_DIR}/${output}" .
done

echo ""
echo "Installers written to ${OUTPUT_DIR}/"
ls -lh "${OUTPUT_DIR}"
