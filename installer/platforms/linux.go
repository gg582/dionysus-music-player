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
	execPath, isAppImage, err := findExecutable(ctx)
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
	if !strings.Contains(os.Getenv("PATH"), binDir) {
		ctx.Warn("~/.local/bin is not in your PATH; you may need to log out and back in, or add it to your shell profile")
	}

	if err := installIcons(ctx, isAppImage); err != nil {
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
// gozik binary as a fallback. The second return value is true when an
// AppImage is used.
func findExecutable(ctx *InstallContext) (string, bool, error) {
	entries, err := os.ReadDir(ctx.InstallDir)
	if err != nil {
		return "", false, err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".AppImage") {
			appImagePath := filepath.Join(ctx.InstallDir, name)
			if err := os.Chmod(appImagePath, 0755); err != nil {
				return "", false, fmt.Errorf("could not make AppImage executable: %w", err)
			}
			ctx.OnLog("Using bundled AppImage: " + appImagePath)
			return appImagePath, true, nil
		}
	}

	binaryPath := filepath.Join(ctx.InstallDir, "gozik")
	if runtime.GOOS == "windows" {
		binaryPath += ".exe"
	}
	if _, err := os.Stat(binaryPath); err != nil {
		return "", false, fmt.Errorf("no gozik executable found in payload: %w", err)
	}
	return binaryPath, false, nil
}

func installIcons(ctx *InstallContext, isAppImage bool) error {
	iconsDst := filepath.Join(os.Getenv("HOME"), ".local", "share", "icons", "hicolor")
	if err := os.MkdirAll(iconsDst, 0755); err != nil {
		return err
	}

	iconsSrc := filepath.Join(ctx.InstallDir, "assets", "icons", "hicolor")
	info, err := os.Stat(iconsSrc)
	if err == nil && info.IsDir() {
		return copyDir(iconsSrc, iconsDst)
	}

	// AppImage payloads may not ship a loose icon tree. Try to extract one
	// from the AppImage itself, or fall back to a bundled gozik.png.
	if isAppImage {
		appImagePath := ""
		entries, _ := os.ReadDir(ctx.InstallDir)
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".AppImage") {
				appImagePath = filepath.Join(ctx.InstallDir, e.Name())
				break
			}
		}
		if appImagePath != "" {
			extractDir := filepath.Join(ctx.InstallDir, ".appimage-extract")
			if err := extractAppImageIcon(appImagePath, extractDir, iconsDst); err == nil {
				ctx.OnLog("Installed icons extracted from AppImage")
				_ = os.RemoveAll(extractDir)
				return nil
			}
			_ = os.RemoveAll(extractDir)
		}
	}

	fallback := filepath.Join(ctx.InstallDir, "gozik.png")
	if _, err := os.Stat(fallback); err == nil {
		sizeDir := filepath.Join(iconsDst, "256x256", "apps")
		if err := os.MkdirAll(sizeDir, 0755); err != nil {
			return err
		}
		return copyFile(fallback, filepath.Join(sizeDir, "gozik.png"), 0644)
	}

	return fmt.Errorf("no icon source found in payload")
}

// extractAppImageIcon runs an AppImage with --appimage-extract to pull out
// the icon tree. This is best-effort: it may fail if FUSE is unavailable,
// in which case the caller falls back to gozik.png.
func extractAppImageIcon(appImage, extractDir, iconsDst string) error {
	cmd := exec.Command(appImage, "--appimage-extract", "usr/share/icons/hicolor/*/*/*")
	cmd.Dir = extractDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	extractedIcons := filepath.Join(extractDir, "squashfs-root", "usr", "share", "icons", "hicolor")
	if _, err := os.Stat(extractedIcons); err != nil {
		return err
	}
	return copyDir(extractedIcons, iconsDst)
}

func installDesktopEntry(ctx *InstallContext, binaryPath string) error {
	appsDir := filepath.Join(os.Getenv("HOME"), ".local", "share", "applications")
	if err := os.MkdirAll(appsDir, 0755); err != nil {
		return err
	}

	entry := fmt.Sprintf(`[Desktop Entry]
Name=Gozik
Comment=Simple GTK music player
Exec=%s %%F
Icon=gozik
Type=Application
Categories=AudioVideo;Audio;Player;
Terminal=false
`, escapeDesktopExec(binaryPath))

	path := filepath.Join(appsDir, "gozik.desktop")
	if err := os.WriteFile(path, []byte(entry), 0644); err != nil {
		return err
	}

	ctx.OnLog("Wrote desktop entry " + path)
	return nil
}

// escapeDesktopExec applies the Desktop Entry Exec value escaping rules:
// space, tab, newline, double quote, backslash, and $` must be escaped with
// a preceding backslash.
func escapeDesktopExec(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case ' ', '\t', '\n', '"', '\\', '$', '`':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
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
