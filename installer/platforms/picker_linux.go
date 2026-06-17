//go:build linux
// +build linux

package platforms

import (
	"errors"
	"os/exec"
	"strings"
)

// PickFolder opens a native folder-selection dialog on Linux.
// It tries zenity first and falls back to kdialog.
func PickFolder(prompt string) (string, error) {
	if prompt == "" {
		prompt = "Select installation directory"
	}

	if path, err := tryZenity(prompt); err == nil {
		return path, nil
	}
	if path, err := tryKdialog(prompt); err == nil {
		return path, nil
	}
	return "", errors.New("no folder picker available (install zenity or kdialog)")
}

func tryZenity(prompt string) (string, error) {
	cmd := exec.Command("zenity", "--file-selection", "--directory", "--title", prompt)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func tryKdialog(prompt string) (string, error) {
	cmd := exec.Command("kdialog", "--getexistingdirectory", ".", "--title", prompt)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
