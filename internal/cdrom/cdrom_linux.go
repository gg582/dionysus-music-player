//go:build linux

package cdrom

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	CDROMREADTOCHDR   = 0x5305
	CDROMREADTOCENTRY = 0x5306
	CDROMREADAUDIO    = 0x530e
	CDROMEJECT        = 0x5309

	CDROM_LBA = 0x01
	CDROM_MSF = 0x02
)

type cdrom_tochdr struct {
	Trk0 uint8
	Trk1 uint8
}

type cdrom_tocentry struct {
	Track    uint8
	AdrCtrl  uint8
	Format   uint8
	Addr     cdrom_addr
	DataMode uint8
}

type cdrom_addr struct {
	Lba int32
	Msf [3]uint8
	Pad [4]uint8
}

type cdrom_read_audio struct {
	Addr       cdrom_addr
	AddrFormat int32
	NFrames    int32
	Buf        unsafe.Pointer
}

// Device wraps an open CD-ROM device.
type Device struct {
	f *os.File
}

func IsSupported() bool {
	return true
}

func DefaultDevice() string {
	return "/dev/sr0"
}

// Open opens the given CD-ROM device (e.g. "/dev/sr0").
func Open(device string) (*Device, error) {
	f, err := os.OpenFile(device, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	return &Device{f: f}, nil
}

// Close closes the device.
func (d *Device) Close() error {
	return d.f.Close()
}

// ReadTOC reads the table of contents and returns audio tracks.
func (d *Device) ReadTOC() ([]Track, error) {
	var hdr cdrom_tochdr
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, d.f.Fd(), CDROMREADTOCHDR, uintptr(unsafe.Pointer(&hdr)))
	if errno != 0 {
		return nil, fmt.Errorf("CDROMREADTOCHDR: %v", errno)
	}

	tracks := make([]Track, 0, hdr.Trk1-hdr.Trk0+1)

	for t := hdr.Trk0; t <= hdr.Trk1; t++ {
		entry := cdrom_tocentry{
			Track:  t,
			Format: CDROM_LBA,
		}
		_, _, errno = unix.Syscall(unix.SYS_IOCTL, d.f.Fd(), CDROMREADTOCENTRY, uintptr(unsafe.Pointer(&entry)))
		if errno != 0 {
			return nil, fmt.Errorf("CDROMREADTOCENTRY track %d: %v", t, errno)
		}

		startLBA := int(entry.Addr.Lba)
		isAudio := ((entry.AdrCtrl >> 4) & 0x04) == 0

		if t > hdr.Trk0 {
			prev := &tracks[len(tracks)-1]
			prev.EndLBA = startLBA
			prev.Length = prev.EndLBA - prev.StartLBA
		}

		tracks = append(tracks, Track{
			Number:   int(t),
			StartLBA: startLBA,
			IsAudio:  isAudio,
		})
	}

	// Lead-out to determine end of last track.
	entry := cdrom_tocentry{
		Track:  0xAA,
		Format: CDROM_LBA,
	}
	_, _, errno = unix.Syscall(unix.SYS_IOCTL, d.f.Fd(), CDROMREADTOCENTRY, uintptr(unsafe.Pointer(&entry)))
	if errno != 0 {
		return nil, fmt.Errorf("CDROMREADTOCENTRY lead-out: %v", errno)
	}
	if len(tracks) > 0 {
		last := &tracks[len(tracks)-1]
		last.EndLBA = int(entry.Addr.Lba)
		last.Length = last.EndLBA - last.StartLBA
	}

	return tracks, nil
}

// ReadAudioFrames reads nframes audio sectors starting at lba.
func (d *Device) ReadAudioFrames(lba int, nframes int) ([]byte, error) {
	if nframes <= 0 {
		return nil, nil
	}
	buf := make([]byte, nframes*CD_FRAME_SIZE)
	ra := cdrom_read_audio{
		AddrFormat: CDROM_LBA,
		NFrames:    int32(nframes),
		Buf:        unsafe.Pointer(&buf[0]),
	}
	ra.Addr.Lba = int32(lba)

	_, _, errno := unix.Syscall(unix.SYS_IOCTL, d.f.Fd(), CDROMREADAUDIO, uintptr(unsafe.Pointer(&ra)))
	if errno != 0 {
		return nil, fmt.Errorf("CDROMREADAUDIO lba=%d nframes=%d: %v", lba, nframes, errno)
	}
	return buf, nil
}

// Eject opens the CD tray.
func (d *Device) Eject() error {
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, d.f.Fd(), CDROMEJECT, 0)
	if errno != 0 {
		return fmt.Errorf("CDROMEJECT: %v", errno)
	}
	return nil
}
