package utils

import (
	"fmt"

	"github.com/gopxl/beep"
)

type float32Streamer struct {
	data     []float32
	channels int
	pos      int
	format   beep.Format
	closeFn  func() error
}

func NewFloat32Streamer(data []float32, channels int, sampleRate int, closeFn func() error) (beep.StreamSeekCloser, beep.Format, error) {
	if channels < 1 || channels > 2 {
		if closeFn != nil {
			_ = closeFn()
		}
		return nil, beep.Format{}, fmt.Errorf("unsupported channel count: %d", channels)
	}
	if sampleRate <= 0 {
		if closeFn != nil {
			_ = closeFn()
		}
		return nil, beep.Format{}, fmt.Errorf("invalid sample rate: %d", sampleRate)
	}
	if len(data) == 0 || len(data)%channels != 0 {
		if closeFn != nil {
			_ = closeFn()
		}
		return nil, beep.Format{}, fmt.Errorf("invalid decoded sample buffer")
	}

	format := beep.Format{
		SampleRate:  beep.SampleRate(sampleRate),
		NumChannels: channels,
		Precision:   4,
	}
	return &float32Streamer{
		data:     data,
		channels: channels,
		format:   format,
		closeFn:  closeFn,
	}, format, nil
}

func (s *float32Streamer) Stream(samples [][2]float64) (int, bool) {
	if s.pos >= s.Len() {
		return 0, false
	}

	n := min(len(samples), s.Len()-s.pos)
	for i := 0; i < n; i++ {
		idx := (s.pos + i) * s.channels
		left := float64(s.data[idx])
		right := left
		if s.channels == 2 {
			right = float64(s.data[idx+1])
		}
		samples[i][0] = left
		samples[i][1] = right
	}
	s.pos += n
	return n, n > 0
}

func (s *float32Streamer) Err() error {
	return nil
}

func (s *float32Streamer) Len() int {
	return len(s.data) / s.channels
}

func (s *float32Streamer) Position() int {
	return s.pos
}

func (s *float32Streamer) Seek(p int) error {
	if p < 0 || p > s.Len() {
		return fmt.Errorf("seek out of bounds")
	}
	s.pos = p
	return nil
}

func (s *float32Streamer) Close() error {
	if s.closeFn == nil {
		return nil
	}
	return s.closeFn()
}
