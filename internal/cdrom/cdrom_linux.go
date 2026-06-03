//go:build linux

package cdrom

import (
	"encoding/binary"
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
	_        [1]byte
	Addr     [4]byte
	DataMode uint8
	_        [3]byte
}

type cdrom_read_audio struct {
	Addr       [4]byte
	AddrFormat uint8
	_          [3]byte
	NFrames    int32
	Buf        unsafe.Pointer
	_          [4]byte
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
	hdr := new(cdrom_tochdr)
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, d.f.Fd(), CDROMREADTOCHDR, uintptr(unsafe.Pointer(hdr)))
	if errno != 0 {
		return nil, fmt.Errorf("CDROMREADTOCHDR: %v", errno)
	}

	tracks := make([]Track, 0, hdr.Trk1-hdr.Trk0+1)

	for t := hdr.Trk0; t <= hdr.Trk1; t++ {
		entry := new(cdrom_tocentry)
		entry.Track = t
		entry.Format = CDROM_LBA

		_, _, errno = unix.Syscall(unix.SYS_IOCTL, d.f.Fd(), CDROMREADTOCENTRY, uintptr(unsafe.Pointer(entry)))
		if errno != 0 {
			return nil, fmt.Errorf("CDROMREADTOCENTRY track %d: %v", t, errno)
		}

		startLBA := int(int32(binary.LittleEndian.Uint32(entry.Addr[:])))
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
	entry := new(cdrom_tocentry)
	entry.Track = 0xAA
	entry.Format = CDROM_LBA

	_, _, errno = unix.Syscall(unix.SYS_IOCTL, d.f.Fd(), CDROMREADTOCENTRY, uintptr(unsafe.Pointer(entry)))
	if errno != 0 {
		return nil, fmt.Errorf("CDROMREADTOCENTRY lead-out: %v", errno)
	}
	if len(tracks) > 0 {
		last := &tracks[len(tracks)-1]
		last.EndLBA = int(int32(binary.LittleEndian.Uint32(entry.Addr[:])))
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
	ra := new(cdrom_read_audio)
	ra.AddrFormat = CDROM_LBA
	ra.NFrames = int32(nframes)
	ra.Buf = unsafe.Pointer(&buf[0])
	binary.LittleEndian.PutUint32(ra.Addr[:], uint32(lba))

	_, _, errno := unix.Syscall(unix.SYS_IOCTL, d.f.Fd(), CDROMREADAUDIO, uintptr(unsafe.Pointer(ra)))
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
