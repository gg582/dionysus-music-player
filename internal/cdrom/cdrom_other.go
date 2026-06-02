//go:build !linux && !windows && !darwin

package cdrom

import "fmt"

type Device struct{}

func IsSupported() bool {
	return false
}

func DefaultDevice() string {
	return ""
}

func Open(device string) (*Device, error) {
	return nil, fmt.Errorf("audio CD playback is not implemented for this OS")
}

func (d *Device) Close() error {
	return nil
}

func (d *Device) ReadTOC() ([]Track, error) {
	return nil, fmt.Errorf("audio CD playback is not implemented for this OS")
}

func (d *Device) ReadAudioFrames(lba int, nframes int) ([]byte, error) {
	return nil, fmt.Errorf("audio CD playback is not implemented for this OS")
}

func (d *Device) Eject() error {
	return fmt.Errorf("audio CD playback is not implemented for this OS")
}
