package ffmpeg

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"math"
	"os/exec"
	"strings"
	"sync"

	"github.com/gopxl/beep"
)

// DecodeStream decodes a network URL via FFmpeg stdout streaming.
// Seeking is supported by restarting the process with -ss parameter.
func DecodeStream(url string, headers map[string]string) (beep.StreamSeekCloser, beep.Format, error) {
	format := beep.Format{
		SampleRate:  ffmpegFallbackSampleRate,
		NumChannels: 2,
		Precision:   4,
	}

	fs := &ffmpegStream{
		url:     url,
		headers: headers,
		format:  format,
	}

	if err := fs.Seek(0); err != nil {
		return nil, beep.Format{}, err
	}

	return fs, format, nil
}

type ffmpegStream struct {
	mu      sync.Mutex
	url     string
	headers map[string]string
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
	s.mu.Lock()
	if s.err != nil {
		s.mu.Unlock()
		return 0, false
	}
	stdout := s.stdout
	s.mu.Unlock()

	if stdout == nil {
		return 0, false
	}

	buf := make([]byte, len(samples)*8)
	n, err := stdout.Read(buf)

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stdout != stdout {
		return 0, true
	}

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
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	return s.waitErr
}

func (s *ffmpegStream) Len() int {
	return 0
}

func (s *ffmpegStream) Position() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pos
}

func (s *ffmpegStream) Seek(p int) error {
	s.mu.Lock()
	oldStdout := s.stdout
	oldCmd := s.cmd
	oldDone := s.done
	s.mu.Unlock()

	if oldCmd != nil {
		if oldStdout != nil {
			_ = oldStdout.Close()
		}
		if oldCmd.Process != nil {
			_ = oldCmd.Process.Kill()
		}
		if oldDone != nil {
			<-oldDone
		}
	}

	offsetSeconds := float64(p) / float64(s.format.SampleRate)

	args := []string{
		"-hide_banner",
		"-loglevel", "error",
	}
	for k, v := range s.headers {
		args = append(args, "-headers", fmt.Sprintf("%s: %s\r\n", k, v))
	}
	if offsetSeconds > 0 {
		args = append(args, "-ss", fmt.Sprintf("%.3f", offsetSeconds))
	}
	args = append(args,
		"-reconnect", "1",
		"-reconnect_streamed", "1",
		"-i", s.url,
		"-vn",
		"-f", "f32le",
		"-acodec", "pcm_f32le",
		"-ac", "2",
		"-ar", fmt.Sprint(s.format.SampleRate),
		"pipe:1",
	)
	cmd := exec.Command("ffmpeg", args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("ffmpeg stdout pipe: %w", err)
	}

	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("ffmpeg start: %w", err)
	}

	done := make(chan struct{})

	s.mu.Lock()
	s.stdout = stdout
	s.cmd = cmd
	s.stderr = stderr
	s.done = done
	s.pos = p
	s.waitErr = nil
	s.err = nil
	s.mu.Unlock()

	go func(c *exec.Cmd, d chan struct{}, se *bytes.Buffer) {
		defer close(d)
		err := c.Wait()

		s.mu.Lock()
		defer s.mu.Unlock()

		if s.cmd == c {
			if err != nil {
				s.waitErr = err
				if msg := strings.TrimSpace(se.String()); msg != "" {
					log.Printf("ffmpeg stream error: %v: %s", err, msg)
				} else {
					log.Printf("ffmpeg stream error: %v", err)
				}
			}
		}
	}(cmd, done, stderr)

	return nil
}

func (s *ffmpegStream) Close() error {
	s.mu.Lock()
	stdout := s.stdout
	cmd := s.cmd
	done := s.done
	s.mu.Unlock()

	if stdout != nil {
		_ = stdout.Close()
	}
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	if done != nil {
		<-done
	}
	return nil
}

func IsStreamURL(path string) bool {
	return strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://")
}
