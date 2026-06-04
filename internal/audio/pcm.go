package audio

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/gopxl/beep"
)

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
