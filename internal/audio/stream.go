package audio

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"math"
	"os/exec"
	"strings"

	"github.com/gopxl/beep"
)

// decodeFFmpegStream decodes a network URL via FFmpeg stdout streaming.
// Seeking is not supported (Len() == 0, Seek() returns an error).
func decodeFFmpegStream(url string, headers map[string]string) (beep.StreamSeekCloser, beep.Format, error) {
	args := []string{
		"-hide_banner",
		"-loglevel", "error",
	}
	for k, v := range headers {
		args = append(args, "-headers", fmt.Sprintf("%s: %s\r\n", k, v))
	}
	// Reconnect when YouTube resets the TLS connection mid-stream.
	// Without these flags FFmpeg exits (often with code 0) after an
	// inconsistent amount of decoded audio, causing truncated playback.
	args = append(args,
		"-reconnect", "1",
		"-reconnect_streamed", "1",
		"-i", url,
		"-vn",
		"-f", "f32le",
		"-acodec", "pcm_f32le",
		"-ac", "2",
		"-ar", fmt.Sprint(ffmpegFallbackSampleRate),
		"pipe:1",
	)
	cmd := exec.Command("ffmpeg", args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, beep.Format{}, fmt.Errorf("ffmpeg stdout pipe: %w", err)
	}

	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		return nil, beep.Format{}, fmt.Errorf("ffmpeg start: %w", err)
	}

	format := beep.Format{
		SampleRate:  ffmpegFallbackSampleRate,
		NumChannels: 2,
		Precision:   4,
	}

	fs := &ffmpegStream{
		stdout: stdout,
		cmd:    cmd,
		format: format,
		stderr: stderr,
		done:   make(chan struct{}),
	}

	// Watch FFmpeg so we can surface connection errors that would otherwise
	// be invisible (stderr was previously discarded and FFmpeg sometimes
	// exits with code 0 after a mid-stream connection reset).
	go func() {
		defer close(fs.done)
		if err := cmd.Wait(); err != nil {
			fs.waitErr = err
			if msg := strings.TrimSpace(stderr.String()); msg != "" {
				log.Printf("ffmpeg stream error: %v: %s", err, msg)
			} else {
				log.Printf("ffmpeg stream error: %v", err)
			}
		}
	}()

	return fs, format, nil
}

type ffmpegStream struct {
	stdout  io.ReadCloser
	cmd     *exec.Cmd
	pos     int
	format  beep.Format
	err     error
	waitErr error
	stderr  *bytes.Buffer
	done    chan struct{}
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
	if s.err != nil {
		return s.err
	}
	return s.waitErr
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
	<-s.done
	return nil
}

func IsStreamURL(path string) bool {
	return strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://")
}
