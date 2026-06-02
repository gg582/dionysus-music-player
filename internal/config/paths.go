package config

import (
	"os"
	"path/filepath"
	"strings"
)

// Prefix can be overridden at link time with -ldflags.
var Prefix = ""

// AssetPath searches for a resource relative to the dionysus data directory.
// It checks, in order:
//  1. Compiled-in Prefix + share/dionysus/<rel>
//  2. /usr/share/dionysus/<rel>
//  3. /usr/local/share/dionysus/<rel>
//  4. XDG_DATA_DIRS entries + dionysus/<rel>
//  5. DIONYSUS_ASSETS environment variable
//  6. ./assets/<rel> (development fallback)
func AssetPath(rel string) string {
	candidates := []string{}

	if Prefix != "" {
		candidates = append(candidates, filepath.Join(Prefix, "share", "dionysus", rel))
	}
	candidates = append(candidates,
		filepath.Join("/usr", "share", "dionysus", rel),
		filepath.Join("/usr", "local", "share", "dionysus", rel),
	)

	if xdg := os.Getenv("XDG_DATA_DIRS"); xdg != "" {
		for _, dir := range strings.Split(xdg, ":") {
			if dir != "" {
				candidates = append(candidates, filepath.Join(dir, "dionysus", rel))
			}
		}
	}

	if env := os.Getenv("DIONYSUS_ASSETS"); env != "" {
		candidates = append(candidates, filepath.Join(env, rel))
	}

	// Development fallback relative to working directory.
	candidates = append(candidates, filepath.Join(".", "assets", rel))

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
