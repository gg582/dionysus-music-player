//go:build darwin

package cdrom

import (
	"encoding/binary"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type darwinTrack struct {
	track      Track
	path       string
	dataOffset int64
	dataSize   int64
	littlePCM  bool
}

type Device struct {
	mount  string
	tracks []darwinTrack
}

func IsSupported() bool {
	return true
}

func DefaultDevice() string {
	entries, err := os.ReadDir("/Volumes")
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		mount := filepath.Join("/Volumes", entry.Name())
		if files, _ := darwinAudioTrackFiles(mount); len(files) > 0 {
			return mount
		}
	}
	return ""
}

func Open(device string) (*Device, error) {
	if device == "" {
		device = DefaultDevice()
	}
	if device == "" {
		return nil, fmt.Errorf("no mounted audio CD found")
	}

	files, err := darwinAudioTrackFiles(device)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no AIFF audio CD tracks found in %s", device)
	}
	sort.Slice(files, func(i, j int) bool {
		return darwinTrackNumber(files[i], i+1) < darwinTrackNumber(files[j], j+1)
	})

	tracks := make([]darwinTrack, 0, len(files))
	startLBA := 0
	for idx, path := range files {
		info, err := darwinReadAIFFInfo(path)
		if err != nil {
			return nil, err
		}
		length := int(info.sampleFrames * 4 / CD_FRAME_SIZE)
		if length <= 0 {
			continue
		}
		number := darwinTrackNumber(path, idx+1)
		track := Track{
			Number:   number,
			StartLBA: startLBA,
			EndLBA:   startLBA + length,
			Length:   length,
			IsAudio:  true,
		}
		tracks = append(tracks, darwinTrack{
			track:      track,
			path:       path,
			dataOffset: info.dataOffset,
			dataSize:   info.dataSize,
			littlePCM:  info.littlePCM,
		})
		startLBA = track.EndLBA
	}
	if len(tracks) == 0 {
		return nil, fmt.Errorf("no playable AIFF audio CD tracks found in %s", device)
	}
	return &Device{mount: device, tracks: tracks}, nil
}

func darwinAudioTrackFiles(mount string) ([]string, error) {
	files := make([]string, 0)
	err := filepath.WalkDir(mount, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if path != mount && entry.IsDir() {
			return filepath.SkipDir
		}
		if entry.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext == ".aiff" || ext == ".aif" || ext == ".aifc" {
			files = append(files, path)
		}
		return nil
	})
	sort.Strings(files)
	return files, err
}

func darwinTrackNumber(path string, fallback int) int {
	name := filepath.Base(path)
	digits := strings.Builder{}
	for _, r := range name {
		if r < '0' || r > '9' {
			if digits.Len() > 0 {
				break
			}
			continue
		}
		digits.WriteRune(r)
	}
	if digits.Len() == 0 {
		return fallback
	}
	n, err := strconv.Atoi(digits.String())
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

type darwinAIFFInfo struct {
	sampleFrames uint32
	dataOffset   int64
	dataSize     int64
	littlePCM    bool
}

func darwinReadAIFFInfo(path string) (darwinAIFFInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return darwinAIFFInfo{}, err
	}
	defer f.Close()

	var header [12]byte
	if _, err := io.ReadFull(f, header[:]); err != nil {
		return darwinAIFFInfo{}, err
	}
	if string(header[0:4]) != "FORM" || (string(header[8:12]) != "AIFF" && string(header[8:12]) != "AIFC") {
		return darwinAIFFInfo{}, fmt.Errorf("%s is not an AIFF audio track", path)
	}

	var info darwinAIFFInfo
	for {
		var chunk [8]byte
		if _, err := io.ReadFull(f, chunk[:]); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			return darwinAIFFInfo{}, err
		}
		id := string(chunk[0:4])
		size := int64(binary.BigEndian.Uint32(chunk[4:8]))
		chunkStart, _ := f.Seek(0, io.SeekCurrent)

		switch id {
		case "COMM":
			buf := make([]byte, size)
			if _, err := io.ReadFull(f, buf); err != nil {
				return darwinAIFFInfo{}, err
			}
			if len(buf) < 18 {
				return darwinAIFFInfo{}, fmt.Errorf("invalid COMM chunk in %s", path)
			}
			channels := binary.BigEndian.Uint16(buf[0:2])
			sampleFrames := binary.BigEndian.Uint32(buf[2:6])
			bits := binary.BigEndian.Uint16(buf[6:8])
			if channels != 2 || bits != 16 {
				return darwinAIFFInfo{}, fmt.Errorf("%s is %d-channel %d-bit audio, expected stereo 16-bit", path, channels, bits)
			}
			info.sampleFrames = sampleFrames
			if len(buf) >= 22 && string(buf[18:22]) == "sowt" {
				info.littlePCM = true
			}
		case "SSND":
			var ssnd [8]byte
			if _, err := io.ReadFull(f, ssnd[:]); err != nil {
				return darwinAIFFInfo{}, err
			}
			offset := int64(binary.BigEndian.Uint32(ssnd[0:4]))
			info.dataOffset = chunkStart + 8 + offset
			info.dataSize = size - 8 - offset
		}

		next := chunkStart + size
		if size%2 != 0 {
			next++
		}
		if _, err := f.Seek(next, io.SeekStart); err != nil {
			return darwinAIFFInfo{}, err
		}
	}

	if info.sampleFrames == 0 || info.dataOffset == 0 || info.dataSize <= 0 {
		return darwinAIFFInfo{}, fmt.Errorf("missing AIFF audio data in %s", path)
	}
	return info, nil
}

func (d *Device) Close() error {
	return nil
}

func (d *Device) ReadTOC() ([]Track, error) {
	tracks := make([]Track, 0, len(d.tracks))
	for _, t := range d.tracks {
		tracks = append(tracks, t.track)
	}
	return tracks, nil
}

func (d *Device) ReadAudioFrames(lba int, nframes int) ([]byte, error) {
	if nframes <= 0 {
		return nil, nil
	}
	track := d.trackAt(lba)
	if track == nil {
		return nil, fmt.Errorf("no audio track contains LBA %d", lba)
	}

	available := track.track.EndLBA - lba
	if nframes > available {
		nframes = available
	}
	size := nframes * CD_FRAME_SIZE
	offset := track.dataOffset + int64(lba-track.track.StartLBA)*CD_FRAME_SIZE
	if offset+int64(size) > track.dataOffset+track.dataSize {
		size = int(track.dataOffset + track.dataSize - offset)
	}
	if size <= 0 {
		return nil, nil
	}

	f, err := os.Open(track.path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, err
	}
	buf := make([]byte, size)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF {
		return nil, err
	}
	buf = buf[:n]
	if !track.littlePCM {
		for i := 0; i+1 < len(buf); i += 2 {
			buf[i], buf[i+1] = buf[i+1], buf[i]
		}
	}
	return buf, nil
}

func (d *Device) trackAt(lba int) *darwinTrack {
	for i := range d.tracks {
		t := &d.tracks[i]
		if lba >= t.track.StartLBA && lba < t.track.EndLBA {
			return t
		}
	}
	return nil
}

func (d *Device) Eject() error {
	if d.mount == "" {
		return nil
	}
	return exec.Command("diskutil", "eject", d.mount).Run()
}
