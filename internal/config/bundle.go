package config

import (
	"os"
	"path/filepath"
	"runtime"
)

// SetupBundledEnvironment points gdk-pixbuf and other GTK helpers at the
// libraries shipped next to the binary when running from a Windows zip or
// macOS app bundle. On Linux the AppImage AppRun already sets these, so this
// is a no-op when the variables are already present.
func SetupBundledEnvironment() {
	if runtime.GOOS == "linux" {
		return
	}

	execPath, err := os.Executable()
	if err != nil {
		return
	}
	execDir := filepath.Dir(execPath)

	var baseDir string
	switch runtime.GOOS {
	case "darwin":
		// Gozik.app/Contents/MacOS/gozik -> Gozik.app/Contents/Resources
		if filepath.Base(execDir) == "MacOS" && filepath.Base(filepath.Dir(execDir)) == "Contents" {
			baseDir = filepath.Join(execDir, "..", "Resources")
		}
	case "windows":
		baseDir = execDir
	}

	if baseDir == "" {
		return
	}

	if os.Getenv("GDK_PIXBUF_MODULE_FILE") == "" {
		cache := filepath.Join(baseDir, "lib", "gdk-pixbuf-2.0", "2.10.0", "loaders.cache")
		if _, err := os.Stat(cache); err == nil {
			_ = os.Setenv("GDK_PIXBUF_MODULE_FILE", cache)
		}
	}

	if os.Getenv("GDK_PIXBUF_MODULEDIR") == "" {
		moddir := filepath.Join(baseDir, "lib", "gdk-pixbuf-2.0", "2.10.0", "loaders")
		if _, err := os.Stat(moddir); err == nil {
			_ = os.Setenv("GDK_PIXBUF_MODULEDIR", moddir)
		}
	}
}
