package audio

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/gopxl/beep"
)

const (
	rawPCMSampleRate = 44100
	rawPCMChannels   = 2
)

var (
	filenameDurationRE = regexp.MustCompile(`(?i)\[(?:(\d+)m)?(\d+)s\]`)
	cueFileRE          = regexp.MustCompile(`(?i)^\s*FILE\s+(?:"([^"]+)"|(\S+))`)
	cueIndex01RE       = regexp.MustCompile(`(?i)^\s*INDEX\s+01\s+(\d+):(\d+):(\d+)`)
)

// ParseDuration extracts a raw PCM duration hint from the filename or a
// same-basename CUE sheet. It returns seconds and whether a hint was found.
func ParseDuration(filePath string) (float64, bool) {
	base := filepath.Base(filePath)
	if m := filenameDurationRE.FindStringSubmatch(base); m != nil {
		minutes := 0
		if m[1] != "" {
			v, err := strconv.Atoi(m[1])
			if err != nil {
				return 0, false
			}
			minutes = v
		}
		seconds, err := strconv.Atoi(m[2])
		if err != nil {
			return 0, false
		}
		total := float64(minutes*60 + seconds)
		return total, total > 0
	}

	return parseCueDuration(filePath)
}

// DetectPcmFormat detects signed little-endian raw PCM depth for 44100 Hz
// stereo files. If no duration hint exists, it validates 16-bit alignment and
// defaults to s16le.
func DetectPcmFormat(filePath string) (string, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return "", err
	}
	fileSize := info.Size()

	duration, ok := ParseDuration(filePath)
	if !ok {
		if fileSize%4 != 0 {
			return "", fmt.Errorf("raw pcm file size %d is not a multiple of 4 bytes", fileSize)
		}
		return "s16le", nil
	}
	if duration <= 0 {
		return "", fmt.Errorf("invalid raw pcm duration %.3f seconds", duration)
	}

	bytesPerSample := float64(fileSize) / (duration * rawPCMSampleRate * rawPCMChannels)
	switch int(math.Round(bytesPerSample)) {
	case 2:
		if fileSize%4 != 0 {
			return "", fmt.Errorf("s16le raw pcm file size %d is not aligned to 4-byte stereo frames", fileSize)
		}
		return "s16le", nil
	case 3:
		if fileSize%6 != 0 {
			return "", fmt.Errorf("s24le raw pcm file size %d is not aligned to 6-byte stereo frames", fileSize)
		}
		return "s24le", nil
	case 4:
		if fileSize%8 != 0 {
			return "", fmt.Errorf("s32le raw pcm file size %d is not aligned to 8-byte stereo frames", fileSize)
		}
		return "s32le", nil
	default:
		return "", fmt.Errorf("unsupported raw pcm bytes per sample %.3f from size %d and duration %.3f", bytesPerSample, fileSize, duration)
	}
}

func rawPCMFrameSize(format string) (int, error) {
	switch format {
	case "s16le":
		return 4, nil
	case "s24le":
		return 6, nil
	case "s32le":
		return 8, nil
	default:
		return 0, fmt.Errorf("unsupported raw pcm format %q", format)
	}
}

func parseCueDuration(filePath string) (float64, bool) {
	cuePath := strings.TrimSuffix(filePath, filepath.Ext(filePath)) + ".cue"
	data, err := os.ReadFile(cuePath)
	if err != nil {
		return 0, false
	}

	target := filepath.Base(filePath)
	inMatchingFile := true
	sawFile := false
	lastIndex := -1.0

	for _, line := range strings.Split(string(data), "\n") {
		if m := cueFileRE.FindStringSubmatch(line); m != nil {
			name := m[1]
			if name == "" {
				name = m[2]
			}
			sawFile = true
			inMatchingFile = strings.EqualFold(filepath.Base(strings.TrimSpace(name)), target)
			continue
		}
		if !inMatchingFile {
			continue
		}
		if m := cueIndex01RE.FindStringSubmatch(line); m != nil {
			minutes, _ := strconv.Atoi(m[1])
			seconds, _ := strconv.Atoi(m[2])
			frames, _ := strconv.Atoi(m[3])
			lastIndex = float64(minutes*60+seconds) + float64(frames)/75.0
		}
	}

	if sawFile && lastIndex < 0 {
		return 0, false
	}
	return lastIndex, lastIndex > 0
}

type pcmStreamer struct {
	data   []int16
	pos    int
	format beep.Format
	rc     io.Closer
}

func decodePCM(r io.Reader) (beep.StreamSeekCloser, beep.Format, error) {
	return decodePCMWithFormat(r, 44100)
}

func decodePCMWithFormat(r io.Reader, sampleRate beep.SampleRate) (beep.StreamSeekCloser, beep.Format, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, beep.Format{}, err
	}

	if len(data)%4 != 0 {
		return nil, beep.Format{}, fmt.Errorf("raw pcm data length %d is not a multiple of 4 bytes", len(data))
	}

	samples := make([]int16, len(data)/2)
	for i := 0; i < len(samples); i++ {
		samples[i] = int16(binary.LittleEndian.Uint16(data[i*2:]))
	}

	format := beep.Format{
		SampleRate:  sampleRate,
		NumChannels: 2,
		Precision:   2,
	}

	streamer := &pcmStreamer{
		data:   samples,
		format: format,
	}
	if c, ok := r.(io.Closer); ok {
		streamer.rc = c
	}

	return streamer, format, nil
}

func (s *pcmStreamer) Stream(samples [][2]float64) (n int, ok bool) {
	totalSamples := len(s.data)
	if s.pos >= totalSamples {
		return 0, false
	}
	for i := range samples {
		if s.pos >= totalSamples {
			return i, i > 0
		}
		left := float64(s.data[s.pos]) / (1 << 15)
		s.pos++
		right := float64(s.data[s.pos]) / (1 << 15)
		s.pos++
		samples[i][0] = left
		samples[i][1] = right
		n++
	}
	return len(samples), true
}

func (s *pcmStreamer) Err() error {
	return nil
}

func (s *pcmStreamer) Len() int {
	return len(s.data) / 2
}

func (s *pcmStreamer) Position() int {
	return s.pos / 2
}

func (s *pcmStreamer) Seek(p int) error {
	if p < 0 || p > s.Len() {
		return fmt.Errorf("seek out of bounds")
	}
	s.pos = p * 2
	return nil
}

func (s *pcmStreamer) Close() error {
	if s.rc != nil {
		return s.rc.Close()
	}
	return nil
}
