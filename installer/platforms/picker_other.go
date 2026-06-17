//go:build !linux && !darwin && !windows
// +build !linux,!darwin,!windows

package platforms

import "errors"

// PickFolder is not implemented on unsupported platforms.
func PickFolder(prompt string) (string, error) {
	return "", errors.New("folder picker is not supported on this platform")
}
