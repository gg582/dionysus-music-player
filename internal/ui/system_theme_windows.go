//go:build windows

package ui

import "golang.org/x/sys/windows/registry"

func systemPrefersDarkTheme() (bool, bool) {
	key, err := registry.OpenKey(
		registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`,
		registry.QUERY_VALUE,
	)
	if err != nil {
		return false, false
	}
	defer key.Close()

	value, _, err := key.GetIntegerValue("AppsUseLightTheme")
	if err != nil {
		return false, false
	}
	return value == 0, true
}
