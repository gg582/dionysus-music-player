#!/bin/bash
set -euo pipefail
# ---------------------------------------------------------------------------
# build_appimage.sh
# Multi-architecture AppImage builder for Gozik
# Supports: amd64, arm64, riscv64
# Usage: ./build_appimage.sh <arch>
# ---------------------------------------------------------------------------

ARCH="${1:-amd64}"

case "$ARCH" in
  amd64|x86_64)
    GOARCH=amd64
    GNU_ARCH=x86_64
    DEB_ARCH=amd64
    CC=gcc
    RUNTIME_ARCH=x86_64
    FFMPEG_ARCH=amd64
    ;;
  arm64|aarch64)
    GOARCH=arm64
    GNU_ARCH=aarch64
    DEB_ARCH=arm64
    CC=aarch64-linux-gnu-gcc
    RUNTIME_ARCH=aarch64
    FFMPEG_ARCH=arm64
    ;;
  riscv64)
    GOARCH=riscv64
    GNU_ARCH=riscv64
    DEB_ARCH=riscv64
    CC=riscv64-linux-gnu-gcc
    RUNTIME_ARCH=riscv64
    FFMPEG_ARCH=riscv64
    ;;
  *)
    echo "Usage: $0 {amd64|arm64|riscv64}"
    exit 1
    ;;
esac

echo "=== Building AppImage for $ARCH ==="

PROJECT_ROOT="$(cd "$(dirname "$0")" && pwd)"
BUILD_DIR="${PROJECT_ROOT}/build-appimage-${ARCH}"
APPDIR="${BUILD_DIR}/AppDir"
DIST_DIR="${PROJECT_ROOT}/dist"
mkdir -p "$BUILD_DIR" "$DIST_DIR"
mkdir -p "$APPDIR/usr/bin"
mkdir -p "$APPDIR/usr/lib"
mkdir -p "$APPDIR/usr/share/applications"
mkdir -p "$APPDIR/usr/share/icons/hicolor/256x256/apps"

# ---------------------------------------------------------------------------
# 1. Cross-compile gozik binary
# ---------------------------------------------------------------------------
echo "[1/7] Cross-compiling gozik (GOARCH=$GOARCH, CC=$CC)..."
export GOARCH="$GOARCH"
export CGO_ENABLED=1
export CC="$CC"
cd "$PROJECT_ROOT"
go build -ldflags="-s -w" -o "${APPDIR}/usr/bin/gozik" ./cmd/gozik

# ---------------------------------------------------------------------------
# 2. Collect ffmpeg binary + libav libraries for CGO
# ---------------------------------------------------------------------------
echo "[2/7] Collecting ffmpeg binary and libav libraries..."

if [ "$ARCH" = "riscv64" ]; then
  # riscv64: obtain ffmpeg and libav libraries from Debian ports via apt.
  RISCV_PKGDIR="${BUILD_DIR}/riscv-pkgs"
  mkdir -p "$RISCV_PKGDIR"

  if ! dpkg --print-foreign-architectures 2>/dev/null | grep -q riscv64; then
    echo "      riscv64 apt multiarch not detected. Setting up Debian ports..."
    dpkg --add-architecture riscv64 || true
    if [ ! -f /etc/apt/sources.list.d/riscv64-ports.list ]; then
      echo "deb [arch=riscv64] http://deb.debian.org/debian-ports sid main" \
        > /etc/apt/sources.list.d/riscv64-ports.list
      apt-get update || true
    fi
  fi

  apt-get download -o Dir::Cache::Archives="$RISCV_PKGDIR" \
    ffmpeg:riscv64 \
    libavcodec-dev:riscv64 \
    libavformat-dev:riscv64 \
    libavutil-dev:riscv64 \
    libswresample-dev:riscv64 \
    2>/dev/null || true

  RISCV_EXTRACT="${BUILD_DIR}/riscv-extract"
  mkdir -p "$RISCV_EXTRACT"
  for deb in "$RISCV_PKGDIR"/*.deb; do
    [ -f "$deb" ] || continue
    dpkg-deb -x "$deb" "$RISCV_EXTRACT"
  done

  if [ -f "${RISCV_EXTRACT}/usr/bin/ffmpeg" ]; then
    cp -L "${RISCV_EXTRACT}/usr/bin/ffmpeg" "${APPDIR}/usr/bin/ffmpeg"
    chmod +x "${APPDIR}/usr/bin/ffmpeg"
  else
    echo "ERROR: Failed to obtain riscv64 ffmpeg from Debian ports."
    echo "       Ensure 'dpkg --add-architecture riscv64' and Debian ports repo are configured."
    exit 1
  fi

  # Copy libav shared libs for CGO linkage
  RISCV_LIBDIR="${RISCV_EXTRACT}/usr/lib/riscv64-linux-gnu"
  if [ -d "$RISCV_LIBDIR" ]; then
    for so in "$RISCV_LIBDIR"/*.so*; do
      [ -f "$so" ] || continue
      cp -L "$so" "${APPDIR}/usr/lib/"
    done
  fi
else
  # Prefer distro ffmpeg when available. It avoids relying on external
  # static-build mirrors during release CI, and its shared libs are bundled
  # below through readelf dependency scanning.
  if command -v ffmpeg >/dev/null 2>&1; then
    cp -L "$(command -v ffmpeg)" "${APPDIR}/usr/bin/ffmpeg"
  else
    FFMPEG_TAR="${BUILD_DIR}/ffmpeg-${FFMPEG_ARCH}-static.tar.xz"
    if [ ! -f "$FFMPEG_TAR" ]; then
      curl -fL -o "$FFMPEG_TAR" \
        "https://johnvansickle.com/ffmpeg/releases/ffmpeg-release-${FFMPEG_ARCH}-static.tar.xz"
    fi
    tar -xJf "$FFMPEG_TAR" -C "$BUILD_DIR" --strip-components=1
    cp -L "${BUILD_DIR}/ffmpeg" "${APPDIR}/usr/bin/ffmpeg"
  fi
  chmod +x "${APPDIR}/usr/bin/ffmpeg"
fi

# ---------------------------------------------------------------------------
# 3. Collect shared libraries into AppDir/usr/lib
# ---------------------------------------------------------------------------
echo "[3/7] Collecting shared libraries..."

copy_needed_libs() {
  local binary="$1"
  local needed
  needed=$(${GNU_ARCH}-linux-gnu-readelf -d "$binary" 2>/dev/null \
    | grep 'NEEDED' | sed 's/.*\[\(.*\)\].*/\1/' || true)

  for lib in $needed; do
    # Skip standard C runtime and basic system/UI libraries that belong to the host system
    case "$lib" in
      libc.so*|libpthread.so*|libdl.so*|librt.so*|libm.so*|ld-linux*.so*|ld64.so*|linux-vdso.so*)
        continue
        ;;
      libglib-2.0.so*|libgobject-2.0.so*|libgio-2.0.so*|libgmodule-2.0.so*|libgthread-2.0.so*)
        continue
        ;;
      libgtk-3.so*|libgdk-3.so*|libatk-1.0.so*|libatk-bridge-2.0.so*|libcups.so*)
        continue
        ;;
      libcairo.so*|libfontconfig.so*|libfreetype.so*|libpango*.so*|libharfbuzz.so*)
        continue
        ;;
      libX11.so*|libXext.so*|libXrender.so*|libXi.so*|libXfixes.so*|libXcursor.so*|libXdamage.so*|libXcomposite.so*|libXrandr.so*|libXinerama.so*)
        continue
        ;;
      libwayland-client.so*|libwayland-cursor.so*|libwayland-egl.so*|libffi.so*|libz.so*|libpng*.so*|libjpeg*.so*)
        continue
        ;;
    esac

    local found=0
    for prefix in "/usr/lib/${GNU_ARCH}-linux-gnu" "/usr/${GNU_ARCH}-linux-gnu/lib" "/usr/local/lib/${GNU_ARCH}-linux-gnu"; do
      if [ -f "${prefix}/${lib}" ]; then
        cp -L "${prefix}/${lib}" "${APPDIR}/usr/lib/"
        found=1
        break
      fi
    done

    # Fallback to riscv extract directory
    if [ "$found" = "0" ] && [ -d "${BUILD_DIR}/riscv-extract/usr/lib/${GNU_ARCH}-linux-gnu" ]; then
      if [ -f "${BUILD_DIR}/riscv-extract/usr/lib/${GNU_ARCH}-linux-gnu/${lib}" ]; then
        cp -L "${BUILD_DIR}/riscv-extract/usr/lib/${GNU_ARCH}-linux-gnu/${lib}" "${APPDIR}/usr/lib/"
        found=1
      fi
    fi

    if [ "$found" = "0" ]; then
      echo "      WARNING: $lib not found for $ARCH (required by $binary)"
    fi
  done
}

copy_needed_libs "${APPDIR}/usr/bin/gozik"
copy_needed_libs "${APPDIR}/usr/bin/ffmpeg"

# Resolve indirect dependencies (repeat a few times to stabilise)
for _ in {1..4}; do
  local_changed=0
  for lib in "${APPDIR}/usr/lib/"*.so*; do
    [ -f "$lib" ] || continue
    if copy_needed_libs "$lib"; then
      local_changed=1
    fi
  done
  # copy_needed_libs always returns 0; break when no new files appear
  new_count=$(find "${APPDIR}/usr/lib" -type f | wc -l)
  if [ "${prev_count:-0}" = "$new_count" ]; then
    break
  fi
  prev_count=$new_count
done

# ---------------------------------------------------------------------------
# 4. Desktop entry, icon and UI assets
# ---------------------------------------------------------------------------
echo "[4/7] Setting up desktop entry, icon and UI assets..."
cp "${PROJECT_ROOT}/assets/gozik.desktop" "${APPDIR}/usr/share/applications/"
cp "${PROJECT_ROOT}/gozik.png" "${APPDIR}/usr/share/icons/hicolor/256x256/apps/"
ln -sf usr/share/applications/gozik.desktop "${APPDIR}/gozik.desktop" || true
ln -sf usr/share/icons/hicolor/256x256/apps/gozik.png "${APPDIR}/gozik.png" || true

# Ensure Exec points to the bundled binary
sed -i 's|^Exec=.*|Exec=gozik|' "${APPDIR}/usr/share/applications/gozik.desktop"

# Bundle UI assets so the binary can find them inside the AppImage
mkdir -p "${APPDIR}/usr/share/gozik/ui"
cp -r "${PROJECT_ROOT}/assets/ui/"* "${APPDIR}/usr/share/gozik/ui/"

# ---------------------------------------------------------------------------
# 5. AppRun launcher
# ---------------------------------------------------------------------------
echo "[5/7] Writing AppRun launcher..."
cat > "${APPDIR}/AppRun" <<'EOF'
#!/bin/bash
HERE="$(dirname "$(readlink -f "${0}")")"
export LD_LIBRARY_PATH="${HERE}/usr/lib:${LD_LIBRARY_PATH:-}"
export PATH="${HERE}/usr/bin:${PATH:-}"
export GOZIK_ASSETS="${HERE}/usr/share/gozik"
exec "${HERE}/usr/bin/gozik" "$@"
EOF
chmod +x "${APPDIR}/AppRun"

# ---------------------------------------------------------------------------
# 6. Create SquashFS image
# ---------------------------------------------------------------------------
echo "[6/7] Creating SquashFS image..."
SQUASHFS="${BUILD_DIR}/gozik.squashfs"
rm -f "$SQUASHFS"
mksquashfs "$APPDIR" "$SQUASHFS" -root-owned -noappend -comp xz

# ---------------------------------------------------------------------------
# 7. Attach AppImage runtime
# ---------------------------------------------------------------------------
echo "[7/7] Attaching AppImage runtime..."
RUNTIME_FILE="${BUILD_DIR}/runtime-${RUNTIME_ARCH}"
if [ ! -f "$RUNTIME_FILE" ]; then
  curl -sL -o "$RUNTIME_FILE" \
    "https://github.com/AppImage/AppImageKit/releases/download/continuous/runtime-${RUNTIME_ARCH}"
fi

APPIMAGE_NAME="gozik-${ARCH}.AppImage"
cat "$RUNTIME_FILE" "$SQUASHFS" > "${DIST_DIR}/${APPIMAGE_NAME}"
chmod +x "${DIST_DIR}/${APPIMAGE_NAME}"

echo ""
echo "=== SUCCESS ==="
echo "Output: ${DIST_DIR}/${APPIMAGE_NAME}"
