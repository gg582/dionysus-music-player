package audio

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gg582/gozik/internal/audio/ffmpeg"
	"github.com/gg582/gozik/internal/audio/midi"
	"github.com/gg582/gozik/internal/audio/pcm"
	"github.com/gopxl/beep"
)

// Mixer manages two concurrent audio channels and drives PCM data into the ring buffer.
type Mixer struct {
	mu sync.RWMutex

	primary *TrackSlot
	next    *TrackSlot

	crossfadeDur   int // frames
	crossfadePos   int
	crossfadeState atomic.Uint32 // 0=off, 1=active

	ring   *RingBuffer
	format beep.Format
	done   chan struct{}
	closed atomic.Bool
}

// TrackSlot wraps a decoded stream with reusable sample scratch space.
type TrackSlot struct {
	stream beep.StreamSeekCloser
}

// NewMixer creates a Mixer targeting the given ring buffer and sample format.
func NewMixer(ring *RingBuffer, format beep.Format) *Mixer {
	return &Mixer{
		ring:         ring,
		format:       format,
		done:         make(chan struct{}),
		crossfadeDur: format.SampleRate.N(5 * time.Second), // 5-second crossfade
	}
}

// Run is the core real-time mixing loop. It must be called exactly once in its own goroutine.
func (m *Mixer) Run() {
	const batch = 512
	priBuf := make([][2]float64, batch)
	nxtBuf := make([][2]float64, batch)
	pcmBuf := make([]byte, batch*engineChannels*enginePrecision)

	for {
		select {
		case <-m.done:
			return
		default:
		}

		m.mu.RLock()
		pri := m.primary
		nxt := m.next
		crossActive := m.crossfadeState.Load() == 1
		crossPos := m.crossfadePos
		crossDur := m.crossfadeDur
		m.mu.RUnlock()

		if pri == nil {
			time.Sleep(5 * time.Millisecond)
			continue
		}

		toRead := batch
		priLen := pri.stream.Len()
		priPos := pri.stream.Position()
		if priLen > 0 {
			rem := priLen - priPos
			if rem <= 0 {
				m.advance()
				continue
			}
			if rem < toRead {
				toRead = rem
			}
			if !crossActive && nxt != nil && rem <= crossDur {
				m.mu.Lock()
				m.crossfadeState.Store(1)
				m.crossfadePos = 0
				m.mu.Unlock()
				crossActive = true
				crossPos = 0
			}
		}

		pn, pok := pri.stream.Stream(priBuf[:toRead])
		if !pok && pn == 0 {
			m.advance()
			continue
		}

		var nn int
		if crossActive && nxt != nil {
			nn, _ = nxt.stream.Stream(nxtBuf[:pn])
		}

		// Mix and convert to int16 PCM.
		for i := 0; i < pn; i++ {
			var l, r float64
			if crossActive && i < nn && crossPos+i < crossDur {
				t := float64(crossPos+i) / float64(crossDur)
				pg := 1.0 - t
				ng := t
				l = priBuf[i][0]*pg + nxtBuf[i][0]*ng
				r = priBuf[i][1]*pg + nxtBuf[i][1]*ng
			} else {
				l = priBuf[i][0]
				r = priBuf[i][1]
			}

			// hard clip
			if l > 1.0 {
				l = 1.0
			} else if l < -1.0 {
				l = -1.0
			}
			if r > 1.0 {
				r = 1.0
			} else if r < -1.0 {
				r = -1.0
			}

			li := int16(l * 32767)
			ri := int16(r * 32767)
			pcmBuf[i*4+0] = byte(li)
			pcmBuf[i*4+1] = byte(li >> 8)
			pcmBuf[i*4+2] = byte(ri)
			pcmBuf[i*4+3] = byte(ri >> 8)
		}

		if crossActive {
			m.mu.Lock()
			m.crossfadePos += pn
			if m.crossfadePos >= m.crossfadeDur {
				m.crossfadeState.Store(0)
				m.crossfadePos = 0
				if m.primary != nil {
					_ = m.primary.stream.Close()
				}
				m.primary = m.next
				m.next = nil
			}
			m.mu.Unlock()
		}

		written := 0
		end := pn * 4
		for written < end {
			n, _ := m.ring.Write(pcmBuf[written:end])
			if n == 0 {
				time.Sleep(time.Millisecond)
				continue
			}
			written += n
		}
	}
}

func (m *Mixer) advance() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.primary != nil {
		_ = m.primary.stream.Close()
		m.primary = nil
	}
	if m.next != nil {
		m.primary = m.next
		m.next = nil
	}
	m.crossfadeState.Store(0)
	m.crossfadePos = 0
}

// LoadPrimary loads a new track into the primary slot, closing any existing primary.
func (m *Mixer) LoadPrimary(path string) error {
	slot, err := m.decodePath(path)
	if err != nil {
		return err
	}

	m.mu.Lock()
	if m.primary != nil {
		_ = m.primary.stream.Close()
	}
	m.primary = slot
	m.crossfadeState.Store(0)
	m.crossfadePos = 0
	m.mu.Unlock()

	return nil
}

// PrepareNext decodes a track into the standby slot for gapless/crossfade transition.
func (m *Mixer) PrepareNext(path string) error {
	slot, err := m.decodePath(path)
	if err != nil {
		return err
	}

	m.mu.Lock()
	if m.next != nil {
		_ = m.next.stream.Close()
	}
	m.next = slot
	m.mu.Unlock()

	return nil
}

// SeekPrimary seeks the current primary track.
func (m *Mixer) SeekPrimary(frame int) error {
	m.mu.RLock()
	pri := m.primary
	m.mu.RUnlock()
	if pri == nil {
		return fmt.Errorf("no primary track")
	}
	return pri.stream.Seek(frame)
}

// Stop clears all tracks and resets crossfade state.
func (m *Mixer) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.primary != nil {
		_ = m.primary.stream.Close()
		m.primary = nil
	}
	if m.next != nil {
		_ = m.next.stream.Close()
		m.next = nil
	}
	m.crossfadeState.Store(0)
	m.crossfadePos = 0
}

// Close signals the mixer loop to exit.
func (m *Mixer) Close() {
	if m.closed.CompareAndSwap(false, true) {
		close(m.done)
	}
}

func (m *Mixer) decodePath(path string) (*TrackSlot, error) {
	if isMIDI(path) {
		s, _, err := midi.Decode(path)
		if err != nil {
			return nil, err
		}
		return &TrackSlot{stream: s}, nil
	}

	if err := pcm.ValidateRawPCMSize(path); err != nil {
		return nil, err
	}

	s, _, err := ffmpeg.Decode(path)
	if err != nil {
		return nil, err
	}
	return &TrackSlot{stream: s}, nil
}

func isMIDI(path string) bool {
	p := strings.ToLower(path)
	return strings.HasSuffix(p, ".mid") || strings.HasSuffix(p, ".midi")
}
