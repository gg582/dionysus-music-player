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
echo "[1/8] Cross-compiling gozik (GOARCH=$GOARCH, CC=$CC)..."
export GOARCH="$GOARCH"
export CGO_ENABLED=1
export CC="$CC"
cd "$PROJECT_ROOT"
go build -ldflags="-s -w" -o "${APPDIR}/usr/bin/gozik" ./cmd/gozik

# ---------------------------------------------------------------------------
# 2. Collect ffmpeg binary + libav libraries for CGO
# ---------------------------------------------------------------------------
echo "[2/8] Collecting ffmpeg binary and libav libraries..."

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
echo "[3/8] Collecting shared libraries (fat bundle)..."

# Only the C runtime, pthread, and dynamic linker are intentionally left on
# the host. Everything else -- GTK, GDK, GLib, X11, Wayland, fontconfig,
# cairo, pango, harfbuzz, libjpeg, libpng, libz, etc. -- is bundled. This
# makes the AppImage larger but avoids SONAME mismatches between distros
# (e.g. libjpeg.so.62 vs libjpeg.so.8) and missing GUI libraries on minimal
# hosts.
copy_needed_libs() {
  local binary="$1"
  local needed
  needed=$(${GNU_ARCH}-linux-gnu-readelf -d "$binary" 2>/dev/null \
    | grep 'NEEDED' | sed 's/.*\[\(.*\)\].*/\1/' || true)

  for lib in $needed; do
    # Keep only the bare-minimum host-provided runtime out of the bundle.
    case "$lib" in
      libc.so*|libpthread.so*|libdl.so*|librt.so*|libm.so*|ld-linux*.so*|ld64.so*|linux-vdso.so*)
        continue
        ;;
    esac

    # Avoid copying the same library repeatedly.
    if [ -f "${APPDIR}/usr/lib/${lib}" ]; then
      continue
    fi

    local found=0
    # Search the standard multiarch directories and a few common subdirs
    # (e.g. pulseaudio) where distros place helper libraries.
    for prefix in "/usr/lib/${GNU_ARCH}-linux-gnu" "/usr/${GNU_ARCH}-linux-gnu/lib" "/usr/local/lib/${GNU_ARCH}-linux-gnu"; do
      for subdir in "" "pulseaudio"; do
        local search_dir="$prefix"
        if [ -n "$subdir" ]; then
          search_dir="${prefix}/${subdir}"
        fi
        if [ -f "${search_dir}/${lib}" ]; then
          cp -L "${search_dir}/${lib}" "${APPDIR}/usr/lib/"
          found=1
          break 2
        fi
      done
    done

    # Fallback to riscv extract directory (and its subdirectories)
    if [ "$found" = "0" ]; then
      local riscv_root="${BUILD_DIR}/riscv-extract/usr/lib/${GNU_ARCH}-linux-gnu"
      if [ -d "$riscv_root" ]; then
        local riscv_match
        riscv_match="$(find "$riscv_root" -name "$lib" -type f 2>/dev/null | head -n1)"
        if [ -n "$riscv_match" ]; then
          cp -L "$riscv_match" "${APPDIR}/usr/lib/"
          found=1
        fi
      fi
    fi

    if [ "$found" = "0" ]; then
      echo "      WARNING: $lib not found for $ARCH (required by $binary)"
    fi
  done
}

copy_needed_libs "${APPDIR}/usr/bin/gozik"
copy_needed_libs "${APPDIR}/usr/bin/ffmpeg"

# Resolve indirect dependencies. Iterate until no new libraries appear.
prev_count=-1
while true; do
  new_count=0
  for lib in "${APPDIR}/usr/lib/"*.so*; do
    [ -f "$lib" ] || continue
    copy_needed_libs "$lib"
  done
  new_count=$(find "${APPDIR}/usr/lib" -type f | wc -l)
  if [ "$prev_count" -eq "$new_count" ]; then
    break
  fi
  prev_count=$new_count
done

# ---------------------------------------------------------------------------
# 4. Bundle gdk-pixbuf image loaders
# ---------------------------------------------------------------------------
echo "[4/8] Bundling gdk-pixbuf image loaders..."

HOST_GDK_PIXBUF_ROOT="/usr/lib/${GNU_ARCH}-linux-gnu/gdk-pixbuf-2.0"
GDK_PIXBUF_VERSION_DIR=""
if [ -d "$HOST_GDK_PIXBUF_ROOT" ]; then
  GDK_PIXBUF_VERSION_DIR="$(find "$HOST_GDK_PIXBUF_ROOT" -maxdepth 1 -type d -name '2.*' | head -n1)"
fi

if [ -n "$GDK_PIXBUF_VERSION_DIR" ] && [ -d "${GDK_PIXBUF_VERSION_DIR}/loaders" ]; then
  APPDIR_GDK_PIXBUF="${APPDIR}/usr/lib/gdk-pixbuf-2.0/$(basename "$GDK_PIXBUF_VERSION_DIR")"
  mkdir -p "${APPDIR_GDK_PIXBUF}/loaders"
  cp -L "${GDK_PIXBUF_VERSION_DIR}/loaders/"*.so "${APPDIR_GDK_PIXBUF}/loaders/"

  # Bring in the loader dependencies as well.
  for loader in "${APPDIR_GDK_PIXBUF}/loaders/"*.so; do
    [ -f "$loader" ] || continue
    copy_needed_libs "$loader"
  done

  # Regenerate loaders.cache. The query tools emit absolute paths when given
  # absolute arguments; rewrite them to be relative to the cache file so the
  # AppImage mounts work regardless of the host layout.
  APPDIR_LOADERS_CACHE="${APPDIR_GDK_PIXBUF}/loaders.cache"
  if command -v "${GNU_ARCH}-linux-gnu-gdk-pixbuf-query-loaders" >/dev/null 2>&1; then
    "${GNU_ARCH}-linux-gnu-gdk-pixbuf-query-loaders" \
      "${APPDIR_GDK_PIXBUF}/loaders/"*.so > "$APPDIR_LOADERS_CACHE"
  elif command -v gdk-pixbuf-query-loaders >/dev/null 2>&1; then
    gdk-pixbuf-query-loaders \
      "${APPDIR_GDK_PIXBUF}/loaders/"*.so > "$APPDIR_LOADERS_CACHE"
  elif [ -f "${GDK_PIXBUF_VERSION_DIR}/loaders.cache" ]; then
    cp "${GDK_PIXBUF_VERSION_DIR}/loaders.cache" "$APPDIR_LOADERS_CACHE"
  fi

  if [ -f "$APPDIR_LOADERS_CACHE" ]; then
    sed -i "s|\"${APPDIR_GDK_PIXBUF}/|\"|g; s|\"${GDK_PIXBUF_VERSION_DIR}/|\"|g" "$APPDIR_LOADERS_CACHE"
  fi
fi

# ---------------------------------------------------------------------------
# 5. Desktop entry, icon and UI assets
# ---------------------------------------------------------------------------
echo "[5/8] Setting up desktop entry, icon and UI assets..."
cp "${PROJECT_ROOT}/assets/gozik.desktop" "${APPDIR}/usr/share/applications/"
cp "${PROJECT_ROOT}/gozik.png" "${APPDIR}/usr/share/icons/hicolor/256x256/apps/"
ln -sf usr/share/applications/gozik.desktop "${APPDIR}/gozik.desktop" || true
ln -sf usr/share/icons/hicolor/256x256/apps/gozik.png "${APPDIR}/.DirIcon" || true
ln -sf usr/share/icons/hicolor/256x256/apps/gozik.png "${APPDIR}/gozik.png" || true

# Ensure Exec points to the bundled binary
sed -i 's|^Exec=.*|Exec=gozik|' "${APPDIR}/usr/share/applications/gozik.desktop"

# Bundle UI assets so the binary can find them inside the AppImage
mkdir -p "${APPDIR}/usr/share/gozik/ui"
cp -r "${PROJECT_ROOT}/assets/ui/"* "${APPDIR}/usr/share/gozik/ui/"

# ---------------------------------------------------------------------------
# 6. AppRun launcher
# ---------------------------------------------------------------------------
echo "[6/8] Writing AppRun launcher..."
cat > "${APPDIR}/AppRun" <<EOF
#!/bin/bash
HERE="\$(dirname "\$(readlink -f "\${0}")")"
export LD_LIBRARY_PATH="\${HERE}/usr/lib:\${LD_LIBRARY_PATH:-}"
export PATH="\${HERE}/usr/bin:\${PATH:-}"
export GOZIK_ASSETS="\${HERE}/usr/share/gozik"

# Point gdk-pixbuf at bundled image loaders so PNG/JPEG/SVG icons work even
# when the host ships incompatible loader modules or libjpeg SONAMEs.
if [ -d "\${HERE}/usr/lib/gdk-pixbuf-2.0" ]; then
  export GDK_PIXBUF_MODULEDIR="\$(find "\${HERE}/usr/lib/gdk-pixbuf-2.0" -maxdepth 1 -type d -name '2.*' | head -n1)/loaders"
  export GDK_PIXBUF_MODULE_FILE="\$(dirname "\${GDK_PIXBUF_MODULEDIR}")/loaders.cache"
fi

exec "\${HERE}/usr/bin/gozik" "\$@"
EOF
chmod +x "${APPDIR}/AppRun"

# ---------------------------------------------------------------------------
# 7. Create SquashFS image
# ---------------------------------------------------------------------------
echo "[7/8] Creating SquashFS image..."
SQUASHFS="${BUILD_DIR}/gozik.squashfs"
rm -f "$SQUASHFS"
mksquashfs "$APPDIR" "$SQUASHFS" -root-owned -noappend -comp xz

# ---------------------------------------------------------------------------
# 8. Attach AppImage runtime
# ---------------------------------------------------------------------------
echo "[8/8] Attaching AppImage runtime..."
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
