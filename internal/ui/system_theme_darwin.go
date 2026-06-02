//go:build darwin

package ui

import (
	"os/exec"
	"strings"
)

func systemPrefersDarkTheme() (bool, bool) {
	out, err := exec.Command("defaults", "read", "-g", "AppleInterfaceStyle").Output()
	if err != nil {
		return false, true
	}
	return strings.EqualFold(strings.TrimSpace(string(out)), "Dark"), true
}
