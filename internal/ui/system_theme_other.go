//go:build !linux && !windows && !darwin

package ui

func systemPrefersDarkTheme() (bool, bool) {
	return false, false
}
