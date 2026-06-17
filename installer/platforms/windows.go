//go:build windows
// +build windows

package platforms

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const uninstallKey = `Software\Microsoft\Windows\CurrentVersion\Uninstall\Gozik`

// Install performs a per-user install on Windows.
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

	if ctx.AddToPATH {
		if err := addToUserPath(ctx.InstallDir); err != nil {
			ctx.Warn("could not update PATH: " + err.Error())
		}
	}

	if ctx.CreateShortcut {
		if err := createStartMenuShortcut(ctx); err != nil {
			ctx.Warn("could not create Start Menu shortcut: " + err.Error())
		}
		if err := createDesktopShortcut(ctx); err != nil {
			ctx.Warn("could not create desktop shortcut: " + err.Error())
		}
	}

	if err := writeUninstallEntry(ctx); err != nil {
		ctx.Warn("could not write uninstall entry: " + err.Error())
	}

	ctx.OnProgress("install", 100)
	return nil
}

func addToUserPath(dir string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()

	path, _, err := k.GetStringValue("Path")
	if err != nil && err != registry.ErrNotExist {
		return err
	}

	for _, part := range filepath.SplitList(path) {
		if strings.EqualFold(part, dir) {
			return nil
		}
	}

	newPath := path
	if newPath != "" && !strings.HasSuffix(newPath, ";") {
		newPath += ";"
	}
	newPath += dir

	if err := k.SetStringValue("Path", newPath); err != nil {
		return err
	}
	return broadcastEnvironmentChange()
}

func broadcastEnvironmentChange() error {
	// Updating the registry is enough; Windows broadcasts the change when the
	// user logs off and back on, or when a new process starts.
	return nil
}

func createStartMenuShortcut(ctx *InstallContext) error {
	programs, err := windows.KnownFolderPath(windows.FOLDERID_RoamingAppData, 0)
	if err != nil {
		return err
	}
	startMenu := filepath.Join(programs, "Microsoft", "Windows", "Start Menu", "Programs")
	if err := os.MkdirAll(startMenu, 0755); err != nil {
		return err
	}
	icon := filepath.Join(ctx.InstallDir, "assets", "icons", "hicolor", "256x256", "apps", "gozik.png")
	return createShortcut(filepath.Join(startMenu, "Gozik.lnk"), filepath.Join(ctx.InstallDir, "gozik.exe"), "", icon)
}

func createDesktopShortcut(ctx *InstallContext) error {
	desktop, err := windows.KnownFolderPath(windows.FOLDERID_Desktop, 0)
	if err != nil {
		return err
	}
	icon := filepath.Join(ctx.InstallDir, "assets", "icons", "hicolor", "256x256", "apps", "gozik.png")
	return createShortcut(filepath.Join(desktop, "Gozik.lnk"), filepath.Join(ctx.InstallDir, "gozik.exe"), "", icon)
}

func createShortcut(lnkPath, target, args, icon string) error {
	ps := fmt.Sprintf(`
$WshShell = New-Object -ComObject WScript.Shell
$Shortcut = $WshShell.CreateShortcut(%q)
$Shortcut.TargetPath = %q
$Shortcut.Arguments = %q
if (%q -ne "") { $Shortcut.IconLocation = %q }
$Shortcut.Save()
`, lnkPath, target, args, icon, icon)
	cmd := exec.Command("powershell.exe", "-ExecutionPolicy", "Bypass", "-NoProfile", "-Command", ps)
	return cmd.Run()
}

func writeUninstallEntry(ctx *InstallContext) error {
	uninstallScript := filepath.Join(ctx.InstallDir, "uninstall.ps1")
	content := fmt.Sprintf(`$installDir = %q
$programs = [Environment]::GetFolderPath('ApplicationData')
$startMenu = Join-Path $programs 'Microsoft\Windows\Start Menu\Programs'
$desktop = [Environment]::GetFolderPath('Desktop')

Remove-Item -Path (Join-Path $startMenu 'Gozik.lnk') -ErrorAction SilentlyContinue
Remove-Item -Path (Join-Path $desktop 'Gozik.lnk') -ErrorAction SilentlyContinue

$key = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\Gozik'
if (Test-Path $key) { Remove-Item -Path $key -Recurse }

$path = [Environment]::GetEnvironmentVariable('Path', 'User')
$parts = $path -split ';' | Where-Object { $_ -ne $installDir }
[Environment]::SetEnvironmentVariable('Path', ($parts -join ';'), 'User')

Remove-Item -Path $installDir -Recurse -Force
`, ctx.InstallDir)
	if err := os.WriteFile(uninstallScript, []byte(content), 0644); err != nil {
		return err
	}

	k, _, err := registry.CreateKey(registry.CURRENT_USER, uninstallKey, registry.WRITE)
	if err != nil {
		return err
	}
	defer k.Close()

	k.SetStringValue("DisplayName", "Gozik")
	k.SetStringValue("DisplayIcon", filepath.Join(ctx.InstallDir, "gozik.exe"))
	k.SetStringValue("UninstallString", fmt.Sprintf(`powershell.exe -ExecutionPolicy Bypass -File "%s"`, uninstallScript))
	k.SetStringValue("InstallLocation", ctx.InstallDir)
	k.SetStringValue("Publisher", "gosuda")
	return nil
}
