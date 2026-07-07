#!/bin/bash
set -euo pipefail
# ---------------------------------------------------------------------------
# collect-windows-dlls.sh
# Recursively collect MinGW DLL dependencies for gozik.exe and bundle
# gdk-pixbuf image loaders so the Windows zip payload is self-contained.
#
# Usage: collect-windows-dlls.sh <mingw-prefix> <output-dir>
#   mingw-prefix: e.g. mingw64 or clangarm64
#   output-dir:   directory containing gozik.exe
# ---------------------------------------------------------------------------

MINGW_PREFIX="${1:-}"
OUTPUT_DIR="${2:-}"

if [[ -z "$MINGW_PREFIX" || -z "$OUTPUT_DIR" ]]; then
  echo "Usage: $0 <mingw-prefix> <output-dir>" >&2
  exit 1
fi

MINGW_BIN="/${MINGW_PREFIX}/bin"
MINGW_LIB="/${MINGW_PREFIX}/lib"

if [[ ! -d "$OUTPUT_DIR" ]]; then
  echo "Output directory does not exist: $OUTPUT_DIR" >&2
  exit 1
fi

cd "$OUTPUT_DIR"

# Collect DLLs recursively. Prefer ntldd if available; otherwise fall back to
# ldd and loop until no new dependencies appear.
collect_dlls() {
  local binary="$1"
  local needed

  if command -v ntldd >/dev/null 2>&1; then
    needed=$(ntldd -R "$binary" 2>/dev/null | awk '{print $3}' | grep -E "^/${MINGW_PREFIX}/bin/" || true)
  else
    needed=$(ldd "$binary" 2>/dev/null | awk '/=> \// {print $3}' | grep -E "^/${MINGW_PREFIX}/bin/" || true)
  fi

  local copied=0
  for dll in $needed; do
    if [[ -f "$dll" && ! -f "$(basename "$dll")" ]]; then
      cp -n "$dll" .
      copied=1
    fi
  done

  if [[ "$copied" -eq 1 ]]; then
    # Recurse over newly copied DLLs.
    for dll in *.dll; do
      collect_dlls "$dll"
    done
  fi
}

echo "Collecting DLLs for gozik.exe..."
collect_dlls "gozik.exe"

# Bundle gdk-pixbuf loaders if available.
GDK_PIXBUF_HOST="${MINGW_LIB}/gdk-pixbuf-2.0"
if [[ -d "$GDK_PIXBUF_HOST" ]]; then
  VERSION_DIR="$(find "$GDK_PIXBUF_HOST" -maxdepth 1 -type d -name '2.*' | head -n1)"
  if [[ -n "$VERSION_DIR" && -d "${VERSION_DIR}/loaders" ]]; then
    echo "Bundling gdk-pixbuf loaders..."
    LOADER_DIR="lib/gdk-pixbuf-2.0/$(basename "$VERSION_DIR")/loaders"
    mkdir -p "$LOADER_DIR"
    cp -L "${VERSION_DIR}/loaders/"*.dll "$LOADER_DIR/"

    # Collect loader DLL dependencies too.
    for loader in "$LOADER_DIR/"*.dll; do
      collect_dlls "$loader"
    done

    # Generate loaders.cache and rewrite absolute paths to be relative to the
    # cache file so the runtime env var works on the end user's machine.
    CACHE_FILE="lib/gdk-pixbuf-2.0/$(basename "$VERSION_DIR")/loaders.cache"
    if command -v gdk-pixbuf-query-loaders >/dev/null 2>&1; then
      gdk-pixbuf-query-loaders "${LOADER_DIR}/"*.dll > "$CACHE_FILE"
      sed -i "s|\"${LOADER_DIR}/|\"|g; s|\"${VERSION_DIR}/loaders/|\"|g" "$CACHE_FILE"
    fi
  fi
fi

echo "Windows payload dependencies collected in ${OUTPUT_DIR}."
