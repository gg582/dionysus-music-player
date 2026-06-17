//go:build darwin
// +build darwin

package platforms

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Install performs a per-user install on macOS by downloading the app bundle payload.
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

	appSrc := filepath.Join(ctx.InstallDir, "Gozik.app")
	if _, err := os.Stat(appSrc); os.IsNotExist(err) {
		return fmt.Errorf("payload does not contain Gozik.app: %s", appSrc)
	}

	appDir := filepath.Join(os.Getenv("HOME"), "Applications")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		return err
	}
	appDst := filepath.Join(appDir, "Gozik.app")

	if _, err := os.Stat(appDst); err == nil {
		ctx.OnLog("Removing existing Gozik.app")
		if err := os.RemoveAll(appDst); err != nil {
			return err
		}
	}

	cmd := exec.Command("cp", "-R", appSrc, appDst)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("copy app bundle failed: %v\n%s", err, out)
	}

	if err := os.RemoveAll(appSrc); err != nil {
		ctx.Warn("could not remove temporary app bundle: " + err.Error())
	}

	ctx.OnProgress("install", 100)
	return nil
}
