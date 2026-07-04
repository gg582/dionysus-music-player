package config

import (
	"os"
	"path/filepath"
	"strings"
)

// Prefix can be overridden at link time with -ldflags.
var Prefix = ""

// AssetPath searches for a resource relative to the gozik data directory.
// It checks, in order:
//  1. Compiled-in Prefix + share/gozik/<rel>
//  2. /usr/share/gozik/<rel>
//  3. /usr/local/share/gozik/<rel>
//  4. XDG_DATA_DIRS entries + gozik/<rel>
//  5. GOZIK_ASSETS environment variable
//  6. ./assets/<rel> (development fallback)
func AssetPath(rel string) string {
	candidates := []string{}

	if env := os.Getenv("GOZIK_ASSETS"); env != "" {
		candidates = append(candidates, filepath.Join(env, rel))
	}

	if execPath, err := os.Executable(); err == nil {
		execDir := filepath.Dir(execPath)
		candidates = append(candidates, filepath.Join(execDir, "assets", rel))
	}

	// Development fallback relative to working directory (prioritized for local runs).
	candidates = append(candidates, filepath.Join(".", "assets", rel))

	if Prefix != "" {
		candidates = append(candidates, filepath.Join(Prefix, "share", "gozik", rel))
	}
	candidates = append(candidates,
		filepath.Join("/usr", "share", "gozik", rel),
		filepath.Join("/usr", "local", "share", "gozik", rel),
	)

	if xdg := os.Getenv("XDG_DATA_DIRS"); xdg != "" {
		for _, dir := range strings.Split(xdg, ":") {
			if dir != "" {
				candidates = append(candidates, filepath.Join(dir, "gozik", rel))
			}
		}
	}

	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	// If nothing found, return the first candidate so callers get a sensible
	// error from the consumer (e.g. gtk.Builder will report missing file).
	if len(candidates) > 0 {
		return candidates[0]
	}
	return rel
}
