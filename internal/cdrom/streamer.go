package cdrom

import (
	"encoding/binary"
)

// TrackStreamer implements beep.StreamSeeker for a CD audio track.
type TrackStreamer struct {
	dev          *Device
	startLBA     int
	endLBA       int
	readLBA      int // next LBA to fetch from device
	buf          []byte
	bufPos       int
	totalSamples int
	err          error
}

// NewTrackStreamer creates a streamer for the given track.
func NewTrackStreamer(dev *Device, track Track) *TrackStreamer {
	return &TrackStreamer{
		dev:          dev,
		startLBA:     track.StartLBA,
		endLBA:       track.EndLBA,
		readLBA:      track.StartLBA,
		totalSamples: (track.EndLBA - track.StartLBA) * CD_SAMPLES,
	}
}

func (s *TrackStreamer) fillBuf() error {
	if s.readLBA >= s.endLBA {
		s.buf = nil
		return nil
	}
	frames := 25
	if s.readLBA+frames > s.endLBA {
		frames = s.endLBA - s.readLBA
	}
	buf, err := s.dev.ReadAudioFrames(s.readLBA, frames)
	if err != nil {
		return err
	}
	s.buf = buf
	s.bufPos = 0
	s.readLBA += frames
	return nil
}

// Stream implements beep.Streamer.
func (s *TrackStreamer) Stream(samples [][2]float64) (int, bool) {
	if s.err != nil {
		return 0, false
	}
	n := 0
	for n < len(samples) {
		if s.buf == nil || s.bufPos >= len(s.buf) {
			if err := s.fillBuf(); err != nil {
				s.err = err
				return n, n > 0
			}
			if s.buf == nil {
				return n, n > 0
			}
		}
		for n < len(samples) && s.bufPos+3 < len(s.buf) {
			left := float64(int16(binary.LittleEndian.Uint16(s.buf[s.bufPos:]))) / 32768.0
			right := float64(int16(binary.LittleEndian.Uint16(s.buf[s.bufPos+2:]))) / 32768.0
			samples[n][0] = left
			samples[n][1] = right
			s.bufPos += 4
			n++
		}
	}
	return n, true
}

// Err implements beep.Streamer.
func (s *TrackStreamer) Err() error {
	return s.err
}

// Close implements beep.StreamSeekCloser (no-op; device is owned by Player).
func (s *TrackStreamer) Close() error {
	return nil
}

// Len implements beep.StreamSeeker.
func (s *TrackStreamer) Len() int {
	return s.totalSamples
}

// Position implements beep.StreamSeeker.
func (s *TrackStreamer) Position() int {
	totalBytes := (s.readLBA - s.startLBA) * CD_FRAME_SIZE
	totalBytes -= len(s.buf)
	totalBytes += s.bufPos
	return totalBytes / 4
}

// Seek implements beep.StreamSeeker.
func (s *TrackStreamer) Seek(p int) error {
	if p < 0 {
		p = 0
	}
	if p > s.totalSamples {
		p = s.totalSamples
	}
	totalBytes := p * 4
	frame := totalBytes / CD_FRAME_SIZE
	offset := totalBytes % CD_FRAME_SIZE

	s.readLBA = s.startLBA + frame
	if s.readLBA >= s.endLBA {
		s.readLBA = s.endLBA
		s.buf = nil
		s.bufPos = 0
		return nil
	}
	s.buf = nil
	s.bufPos = 0
	if offset > 0 {
		if err := s.fillBuf(); err != nil {
			return err
		}
		if s.buf != nil && offset < len(s.buf) {
			s.bufPos = offset
		}
	}
	return nil
}
