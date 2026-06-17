//go:build linux || freebsd || openbsd || netbsd
// +build linux freebsd openbsd netbsd

package platforms

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Install performs a per-user install on Linux and BSD systems.
func Install(ctx *InstallContext) error {
	t := CurrentTarget()
	archive, err := RetryDownload(t, ctx, 3)
	if err != nil {
		return err
	}
	defer os.Remove(archive)

	if err := os.MkdirAll(ctx.InstallDir, 0755); err != nil {
		return err
	}

	if err := ExtractTarGz(archive, ctx.InstallDir, ctx); err != nil {
		return err
	}

	// Prefer a bundled AppImage if the payload included one; otherwise use
	// the plain gozik binary.
	execPath, err := findExecutable(ctx)
	if err != nil {
		return err
	}

	binDir := filepath.Join(os.Getenv("HOME"), ".local", "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return err
	}

	linkPath := filepath.Join(binDir, "gozik")
	if err := os.Remove(linkPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Symlink(execPath, linkPath); err != nil {
		ctx.Warn("could not create ~/.local/bin/gozik symlink: " + err.Error())
	}

	if err := installIcons(ctx); err != nil {
		ctx.Warn("icon install failed: " + err.Error())
	}

	if err := installDesktopEntry(ctx, execPath); err != nil {
		ctx.Warn("desktop entry install failed: " + err.Error())
	}

	if err := updateIconCache(ctx); err != nil {
		ctx.Warn("icon cache update failed: " + err.Error())
	}

	ctx.OnProgress("install", 100)
	return nil
}

// findExecutable locates the file that should be launched after install.
// It returns the path to an AppImage bundled in the payload, or the plain
// gozik binary as a fallback.
func findExecutable(ctx *InstallContext) (string, error) {
	entries, err := os.ReadDir(ctx.InstallDir)
	if err != nil {
		return "", err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".AppImage") {
			appImagePath := filepath.Join(ctx.InstallDir, name)
			if err := os.Chmod(appImagePath, 0755); err != nil {
				return "", fmt.Errorf("could not make AppImage executable: %w", err)
			}
			ctx.OnLog("Using bundled AppImage: " + appImagePath)
			return appImagePath, nil
		}
	}

	binaryPath := filepath.Join(ctx.InstallDir, "gozik")
	if runtime.GOOS == "windows" {
		binaryPath += ".exe"
	}
	if _, err := os.Stat(binaryPath); err != nil {
		return "", fmt.Errorf("no gozik executable found in payload: %w", err)
	}
	return binaryPath, nil
}

func installIcons(ctx *InstallContext) error {
	iconsSrc := filepath.Join(ctx.InstallDir, "assets", "icons", "hicolor")
	iconsDst := filepath.Join(os.Getenv("HOME"), ".local", "share", "icons", "hicolor")

	info, err := os.Stat(iconsSrc)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("icon source not found: %s", iconsSrc)
	}

	return copyDir(iconsSrc, iconsDst)
}

func installDesktopEntry(ctx *InstallContext, binaryPath string) error {
	appsDir := filepath.Join(os.Getenv("HOME"), ".local", "share", "applications")
	if err := os.MkdirAll(appsDir, 0755); err != nil {
		return err
	}

	entry := fmt.Sprintf(`[Desktop Entry]
Name=Gozik
Comment=Simple GTK music player
Exec=%q %%F
Icon=gozik
Type=Application
Categories=AudioVideo;Audio;Player;
Terminal=false
`, binaryPath)

	path := filepath.Join(appsDir, "gozik.desktop")
	if err := os.WriteFile(path, []byte(entry), 0644); err != nil {
		return err
	}

	ctx.OnLog("Wrote desktop entry " + path)
	return nil
}

func updateIconCache(ctx *InstallContext) error {
	iconsDir := filepath.Join(os.Getenv("HOME"), ".local", "share", "icons", "hicolor")
	if _, err := exec.LookPath("gtk-update-icon-cache"); err == nil {
		cmd := exec.Command("gtk-update-icon-cache", "-f", "-t", iconsDir)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}
	if _, err := exec.LookPath("update-desktop-database"); err == nil {
		appsDir := filepath.Join(os.Getenv("HOME"), ".local", "share", "applications")
		cmd := exec.Command("update-desktop-database", "-q", appsDir)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		_ = cmd.Run()
	}
	return nil
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		return copyFile(path, target, info.Mode())
	})
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = out.ReadFrom(in)
	return err
}
