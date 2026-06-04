package audio

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"strings"

	"github.com/gopxl/beep"
)

const ffmpegFallbackSampleRate = beep.SampleRate(48000)

func decodeFFmpeg(path string) (beep.StreamSeekCloser, beep.Format, error) {
	return decodeFFmpegSegment(path, 0, 0)
}

func decodeFFmpegSegment(path string, startSec, endSec int) (beep.StreamSeekCloser, beep.Format, error) {
	tmp, err := os.CreateTemp("", "gozik-ffmpeg-*.pcm")
	if err != nil {
		return nil, beep.Format{}, err
	}
	tmpPath := tmp.Name()
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return nil, beep.Format{}, err
	}

	args := []string{
		"-y",
		"-hide_banner",
		"-loglevel", "error",
	}
	if startSec > 0 {
		args = append(args, "-ss", fmt.Sprintf("%d", startSec))
	}
	if endSec > startSec {
		args = append(args, "-t", fmt.Sprintf("%d", endSec-startSec))
	}
	args = append(args,
		"-i", path,
		"-vn",
		"-f", "f32le",
		"-acodec", "pcm_f32le",
		"-ac", "2",
		"-ar", fmt.Sprint(ffmpegFallbackSampleRate),
		tmpPath,
	)

	cmd := exec.Command("ffmpeg", args...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		os.Remove(tmpPath)
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, beep.Format{}, fmt.Errorf("ffmpeg decode failed: %s", msg)
	}

	f, err := os.Open(tmpPath)
	if err != nil {
		os.Remove(tmpPath)
		return nil, beep.Format{}, err
	}

	info, err := f.Stat()
	if err != nil {
		f.Close()
		os.Remove(tmpPath)
		return nil, beep.Format{}, err
	}
	if info.Size() == 0 {
		f.Close()
		os.Remove(tmpPath)
		return nil, beep.Format{}, fmt.Errorf("ffmpeg decode produced no audio")
	}

	format := beep.Format{
		SampleRate:  ffmpegFallbackSampleRate,
		NumChannels: 2,
		Precision:   4,
	}
	return &pcm32FileStreamer{
		file:   f,
		path:   tmpPath,
		frames: int(info.Size() / 8),
		format: format,
	}, format, nil
}

type pcm32FileStreamer struct {
	file   *os.File
	path   string
	pos    int
	frames int
	format beep.Format
}

func (s *pcm32FileStreamer) Stream(samples [][2]float64) (n int, ok bool) {
	if s.pos >= s.frames {
		return 0, false
	}

	buf := make([]byte, len(samples)*8)
	read, err := s.file.ReadAt(buf, int64(s.pos*8))
	if err != nil && err != io.EOF {
		return 0, false
	}
	read -= read % 8
	frames := read / 8

	for i := 0; i < frames; i++ {
		left := math.Float32frombits(binary.LittleEndian.Uint32(buf[i*8:]))
		right := math.Float32frombits(binary.LittleEndian.Uint32(buf[i*8+4:]))
		samples[i][0] = float64(left)
		samples[i][1] = float64(right)
	}

	s.pos += frames
	return frames, frames > 0
}

func (s *pcm32FileStreamer) Err() error {
	return nil
}

func (s *pcm32FileStreamer) Len() int {
	return s.frames
}

func (s *pcm32FileStreamer) Position() int {
	return s.pos
}

func (s *pcm32FileStreamer) Seek(p int) error {
	if p < 0 || p > s.Len() {
		return fmt.Errorf("seek out of bounds")
	}
	s.pos = p
	return nil
}

func (s *pcm32FileStreamer) Close() error {
	err := s.file.Close()
	if removeErr := os.Remove(s.path); err == nil {
		err = removeErr
	}
	return err
}

type pcmFileStreamer struct {
	file   *os.File
	path   string
	pos    int
	frames int
	format beep.Format
}

func (s *pcmFileStreamer) Stream(samples [][2]float64) (n int, ok bool) {
	if s.pos >= s.frames {
		return 0, false
	}

	buf := make([]byte, len(samples)*4)
	read, err := s.file.ReadAt(buf, int64(s.pos*4))
	if err != nil && err != io.EOF {
		return 0, false
	}
	read -= read % 4
	frames := read / 4

	for i := 0; i < frames; i++ {
		left := int16(binary.LittleEndian.Uint16(buf[i*4:]))
		right := int16(binary.LittleEndian.Uint16(buf[i*4+2:]))
		samples[i][0] = float64(left) / (1 << 15)
		samples[i][1] = float64(right) / (1 << 15)
	}

	s.pos += frames
	return frames, frames > 0
}

func (s *pcmFileStreamer) Err() error {
	return nil
}

func (s *pcmFileStreamer) Len() int {
	return s.frames
}

func (s *pcmFileStreamer) Position() int {
	return s.pos
}

func (s *pcmFileStreamer) Seek(p int) error {
	if p < 0 || p > s.Len() {
		return fmt.Errorf("seek out of bounds")
	}
	s.pos = p
	return nil
}

func (s *pcmFileStreamer) Close() error {
	err := s.file.Close()
	if removeErr := os.Remove(s.path); err == nil {
		err = removeErr
	}
	return err
}
