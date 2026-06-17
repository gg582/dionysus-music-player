//go:build darwin
// +build darwin

package platforms

import (
	"errors"
	"os/exec"
	"strings"
)

// PickFolder opens a native folder-selection dialog on macOS using osascript.
func PickFolder(prompt string) (string, error) {
	if prompt == "" {
		prompt = "Select installation directory"
	}
	script := `tell application "System Events" to POSIX path of (choose folder with prompt ` + quote(prompt) + `)`
	cmd := exec.Command("osascript", "-e", script)
	out, err := cmd.Output()
	if err != nil {
		return "", errors.New("folder picker failed or was cancelled")
	}
	return strings.TrimSpace(string(out)), nil
}

func quote(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}
