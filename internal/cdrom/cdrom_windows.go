//go:build windows

package cdrom

import (
	"fmt"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	ioctlCDROMReadTOC  = 0x00024000
	ioctlCDROMRawRead  = 0x0002403e
	ioctlStorageEject  = 0x002d4808
	windowsDriveCDROM  = 5
	windowsTrackCDDA   = 2
	windowsTOCMaxTrack = 100
)

type windowsTrackData struct {
	Reserved    byte
	ControlAdr  byte
	TrackNumber byte
	Reserved1   byte
	Address     [4]byte
}

type windowsTOC struct {
	Length     [2]byte
	FirstTrack byte
	LastTrack  byte
	TrackData  [windowsTOCMaxTrack]windowsTrackData
}

type windowsRawReadInfo struct {
	DiskOffset  int64
	SectorCount uint32
	TrackMode   int32
}

type Device struct {
	handle windows.Handle
}

func IsSupported() bool {
	return true
}

func DefaultDevice() string {
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		return ""
	}
	for i := 0; i < 26; i++ {
		if mask&(1<<uint(i)) == 0 {
			continue
		}
		root := fmt.Sprintf("%c:\\", 'A'+i)
		if windows.GetDriveType(windows.StringToUTF16Ptr(root)) == windowsDriveCDROM {
			return root[:2]
		}
	}
	return ""
}

func Open(device string) (*Device, error) {
	if device == "" {
		device = DefaultDevice()
	}
	if device == "" {
		return nil, fmt.Errorf("no CD-ROM drive found")
	}

	path := windowsDevicePath(device)
	handle, err := windows.CreateFile(
		windows.StringToUTF16Ptr(path),
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_EXISTING,
		0,
		0,
	)
	if err != nil {
		return nil, err
	}
	return &Device{handle: handle}, nil
}

func windowsDevicePath(device string) string {
	device = strings.TrimSpace(device)
	device = strings.TrimRight(device, `\`)
	if strings.HasPrefix(device, `\\.\`) {
		return device
	}
	if len(device) == 1 {
		device += ":"
	}
	return `\\.\` + device
}

func (d *Device) Close() error {
	return windows.CloseHandle(d.handle)
}

func (d *Device) ReadTOC() ([]Track, error) {
	var toc windowsTOC
	var returned uint32
	err := windows.DeviceIoControl(
		d.handle,
		ioctlCDROMReadTOC,
		nil,
		0,
		(*byte)(unsafe.Pointer(&toc)),
		uint32(unsafe.Sizeof(toc)),
		&returned,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("IOCTL_CDROM_READ_TOC: %w", err)
	}

	trackCount := int(toc.LastTrack-toc.FirstTrack) + 1
	if trackCount <= 0 || trackCount >= windowsTOCMaxTrack {
		return nil, fmt.Errorf("invalid CD TOC track range %d-%d", toc.FirstTrack, toc.LastTrack)
	}

	tracks := make([]Track, 0, trackCount)
	for i := 0; i < trackCount; i++ {
		entry := toc.TrackData[i]
		startLBA := windowsMSFToLBA(entry.Address)
		isAudio := ((entry.ControlAdr >> 4) & 0x04) == 0

		if i > 0 {
			prev := &tracks[len(tracks)-1]
			prev.EndLBA = startLBA
			prev.Length = prev.EndLBA - prev.StartLBA
		}

		tracks = append(tracks, Track{
			Number:   int(entry.TrackNumber),
			StartLBA: startLBA,
			IsAudio:  isAudio,
		})
	}

	leadOut := windowsMSFToLBA(toc.TrackData[trackCount].Address)
	if len(tracks) > 0 {
		last := &tracks[len(tracks)-1]
		last.EndLBA = leadOut
		last.Length = last.EndLBA - last.StartLBA
	}

	return tracks, nil
}

func windowsMSFToLBA(msf [4]byte) int {
	lba := (int(msf[1])*CD_SECS+int(msf[2]))*CD_FRAMES + int(msf[3]) - 150
	if lba < 0 {
		return 0
	}
	return lba
}

func (d *Device) ReadAudioFrames(lba int, nframes int) ([]byte, error) {
	if nframes <= 0 {
		return nil, nil
	}
	buf := make([]byte, nframes*CD_FRAME_SIZE)
	info := windowsRawReadInfo{
		DiskOffset:  int64(lba) * 2048,
		SectorCount: uint32(nframes),
		TrackMode:   windowsTrackCDDA,
	}
	var returned uint32
	err := windows.DeviceIoControl(
		d.handle,
		ioctlCDROMRawRead,
		(*byte)(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
		&buf[0],
		uint32(len(buf)),
		&returned,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("IOCTL_CDROM_RAW_READ lba=%d nframes=%d: %w", lba, nframes, err)
	}
	return buf[:returned], nil
}

func (d *Device) Eject() error {
	var returned uint32
	err := windows.DeviceIoControl(d.handle, ioctlStorageEject, nil, 0, nil, 0, &returned, nil)
	if err != nil {
		return fmt.Errorf("IOCTL_STORAGE_EJECT_MEDIA: %w", err)
	}
	return nil
}
