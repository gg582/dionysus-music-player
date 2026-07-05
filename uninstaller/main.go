package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func main() {
	headless := flag.Bool("headless", false, "Run uninstallation silently without prompts")
	flag.Parse()

	fmt.Println("Gozik Uninstaller starting...")

	if !*headless {
		fmt.Print("Are you sure you want to uninstall Gozik? (y/N): ")
		var answer string
		fmt.Scanln(&answer)
		answer = strings.ToLower(strings.TrimSpace(answer))
		if answer != "y" && answer != "yes" {
			fmt.Println("Uninstallation cancelled.")
			return
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting home directory: %v\n", err)
		os.Exit(1)
	}

	var installDir string
	switch runtime.GOOS {
	case "windows":
		local := os.Getenv("LOCALAPPDATA")
		if local == "" {
			local = filepath.Join(home, "AppData", "Local")
		}
		installDir = filepath.Join(local, "gozik")
	case "darwin":
		installDir = filepath.Join(home, "Library", "Application Support", "gozik")
	default:
		installDir = filepath.Join(home, ".local", "share", "gozik")
	}

	fmt.Printf("Removing installation directory: %s\n", installDir)
	if err := os.RemoveAll(installDir); err != nil {
		fmt.Fprintf(os.Stderr, "Warning removing installation directory: %v\n", err)
	}

	// Platform specific cleanup
	switch runtime.GOOS {
	case "windows":
		// Remove shortcuts and environment path and registry
		desktop := filepath.Join(home, "Desktop")
		startMenu := filepath.Join(home, "AppData", "Roaming", "Microsoft", "Windows", "Start Menu", "Programs")
		_ = os.Remove(filepath.Join(desktop, "Gozik.lnk"))
		_ = os.Remove(filepath.Join(startMenu, "Gozik.lnk"))

		// We can try calling reg delete command
		cmd := exec.Command("reg", "delete", `HKCU\Software\Microsoft\Windows\CurrentVersion\Uninstall\Gozik`, "/f")
		_ = cmd.Run()

	case "darwin":
		appDst := filepath.Join(home, "Applications", "Gozik.app")
		fmt.Printf("Removing App bundle: %s\n", appDst)
		if err := os.RemoveAll(appDst); err != nil {
			fmt.Fprintf(os.Stderr, "Warning removing App bundle: %v\n", err)
		}

	default: // Linux / FreeBSDs
		binLink := filepath.Join(home, ".local", "bin", "gozik")
		fmt.Printf("Removing symlink: %s\n", binLink)
		_ = os.Remove(binLink)

		desktopFile := filepath.Join(home, ".local", "share", "applications", "gozik.desktop")
		fmt.Printf("Removing desktop entry: %s\n", desktopFile)
		_ = os.Remove(desktopFile)

		iconsDir := filepath.Join(home, ".local", "share", "icons", "hicolor")
		fmt.Println("Removing icons...")
		// Rather than removing the whole directory, delete individual gozik icon files if they exist under sizes
		sizes := []string{"16x16", "22x22", "24x24", "32x32", "48x48", "64x64", "128x128", "256x256", "512x512"}
		for _, sz := range sizes {
			_ = os.Remove(filepath.Join(iconsDir, sz, "apps", "gozik.png"))
		}

		// Update desktop database and icon cache
		_ = exec.Command("update-desktop-database", "-q", filepath.Join(home, ".local", "share", "applications")).Run()
		_ = exec.Command("gtk-update-icon-cache", "-f", "-t", iconsDir).Run()
	}

	fmt.Println("Gozik has been successfully uninstalled.")
}
