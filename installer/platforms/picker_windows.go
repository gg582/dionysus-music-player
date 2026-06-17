//go:build windows
// +build windows

package platforms

import (
	"errors"
	"os/exec"
	"strings"
)

// PickFolder opens a native folder-selection dialog on Windows using PowerShell.
func PickFolder(prompt string) (string, error) {
	if prompt == "" {
		prompt = "Select installation directory"
	}
	ps := `
Add-Type -AssemblyName System.Windows.Forms
$dlg = New-Object System.Windows.Forms.FolderBrowserDialog
$dlg.Description = ` + quote(prompt) + `
$dlg.ShowNewFolderButton = $true
if ($dlg.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) {
    Write-Output $dlg.SelectedPath
}
`
	cmd := exec.Command("powershell.exe", "-ExecutionPolicy", "Bypass", "-NoProfile", "-Command", ps)
	out, err := cmd.Output()
	if err != nil {
		return "", errors.New("folder picker failed or was cancelled")
	}
	path := strings.TrimSpace(string(out))
	if path == "" {
		return "", errors.New("no folder selected")
	}
	return path, nil
}

func quote(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}
