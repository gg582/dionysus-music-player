package audio

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os/exec"
	"strings"

	"github.com/gopxl/beep"
)

// decodeFFmpegStream decodes a network URL via FFmpeg stdout streaming.
// Seeking is not supported (Len() == 0, Seek() returns an error).
func decodeFFmpegStream(url string) (beep.StreamSeekCloser, beep.Format, error) {
	cmd := exec.Command(
		"ffmpeg",
		"-hide_banner",
		"-loglevel", "error",
		"-i", url,
		"-vn",
		"-f", "f32le",
		"-acodec", "pcm_f32le",
		"-ac", "2",
		"-ar", fmt.Sprint(ffmpegFallbackSampleRate),
		"pipe:1",
	)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, beep.Format{}, fmt.Errorf("ffmpeg stdout pipe: %w", err)
	}
	cmd.Stderr = nil // silence stderr; or could capture if needed

	if err := cmd.Start(); err != nil {
		return nil, beep.Format{}, fmt.Errorf("ffmpeg start: %w", err)
	}

	format := beep.Format{
		SampleRate:  ffmpegFallbackSampleRate,
		NumChannels: 2,
		Precision:   4,
	}
	return &ffmpegStream{
		stdout: stdout,
		cmd:    cmd,
		format: format,
	}, format, nil
}

type ffmpegStream struct {
	stdout io.ReadCloser
	cmd    *exec.Cmd
	pos    int
	format beep.Format
	err    error
}

func (s *ffmpegStream) Stream(samples [][2]float64) (int, bool) {
	if s.err != nil {
		return 0, false
	}

	buf := make([]byte, len(samples)*8)
	n, err := s.stdout.Read(buf)
	if err != nil {
		if err == io.EOF {
			frames := n / 8
			for i := 0; i < frames; i++ {
				left := math.Float32frombits(binary.LittleEndian.Uint32(buf[i*8:]))
				right := math.Float32frombits(binary.LittleEndian.Uint32(buf[i*8+4:]))
				samples[i][0] = float64(left)
				samples[i][1] = float64(right)
			}
			s.pos += frames
			return frames, frames > 0
		}
		s.err = err
		return 0, false
	}

	frames := n / 8
	for i := 0; i < frames; i++ {
		left := math.Float32frombits(binary.LittleEndian.Uint32(buf[i*8:]))
		right := math.Float32frombits(binary.LittleEndian.Uint32(buf[i*8+4:]))
		samples[i][0] = float64(left)
		samples[i][1] = float64(right)
	}
	s.pos += frames
	return frames, frames > 0
}

func (s *ffmpegStream) Err() error {
	return s.err
}

func (s *ffmpegStream) Len() int {
	return 0
}

func (s *ffmpegStream) Position() int {
	return s.pos
}

func (s *ffmpegStream) Seek(p int) error {
	return fmt.Errorf("seek not supported on live streams")
}

func (s *ffmpegStream) Close() error {
	_ = s.stdout.Close()
	_ = s.cmd.Process.Kill()
	_ = s.cmd.Wait()
	return nil
}

func IsStreamURL(path string) bool {
	return strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://")
}
