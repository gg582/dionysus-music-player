package config

import (
	"os"
	"path/filepath"
	"strings"
)

const themeFileName = "theme"

// ThemeConfigDir returns the directory where theme settings are stored.
func ThemeConfigDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = "."
	}
	return filepath.Join(dir, "gozik")
}

// LoadTheme reads the persisted theme mode (light, dark, system).
// Returns "system" when no preference has been saved.
func LoadTheme() string {
	path := filepath.Join(ThemeConfigDir(), themeFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return "system"
	}
	mode := strings.TrimSpace(string(data))
	switch mode {
	case "light", "dark", "system":
		return mode
	default:
		return "system"
	}
}

// SaveTheme persists the theme mode.
func SaveTheme(mode string) error {
	switch mode {
	case "light", "dark", "system":
	default:
		mode = "system"
	}
	dir := ThemeConfigDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	path := filepath.Join(dir, themeFileName)
	return os.WriteFile(path, []byte(mode), 0644)
}
