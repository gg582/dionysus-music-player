package audio

import (
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ebitengine/oto/v3"
	"github.com/gopxl/beep"
)

const (
	engineSampleRate = 48000
	engineChannels   = 2
	enginePrecision  = 2 // int16
)

// Engine orchestrates the low-level software mixer, ring buffer, and host audio output.
// It is safe for concurrent use.
type Engine struct {
	ctx    *oto.Context
	player *oto.Player

	mixer  *Mixer
	ring   *RingBuffer
	reader *ringReader

	state atomic.Uint32 // 0=idle, 1=playing, 2=paused

	mu     sync.Mutex
	closed bool
	done   chan struct{}
}

// NewEngine initializes the host audio context and starts the mixer goroutine.
func NewEngine() (*Engine, error) {
	op := &oto.NewContextOptions{
		SampleRate:   engineSampleRate,
		ChannelCount: engineChannels,
		Format:       oto.FormatSignedInt16LE,
	}
	ctx, ready, err := oto.NewContext(op)
	if err != nil {
		return nil, fmt.Errorf("oto context: %w", err)
	}
	<-ready

	ring := NewRingBuffer(256 * 1024) // ~1.3 s buffer @ 48 kHz stereo int16
	reader := &ringReader{ring: ring, done: make(chan struct{})}

	format := beep.Format{
		SampleRate:  beep.SampleRate(engineSampleRate),
		NumChannels: engineChannels,
		Precision:   enginePrecision,
	}
	mixer := NewMixer(ring, format)

	player := ctx.NewPlayer(reader)

	e := &Engine{
		ctx:    ctx,
		player: player,
		mixer:  mixer,
		ring:   ring,
		reader: reader,
		done:   make(chan struct{}),
	}

	go mixer.Run()
	player.Play()
	e.state.Store(1)

	return e, nil
}

// Load replaces the primary track.
func (e *Engine) Load(path string) error {
	return e.mixer.LoadPrimary(path)
}

// PrepareNext decodes the next track into the standby slot for gapless playback.
func (e *Engine) PrepareNext(path string) error {
	return e.mixer.PrepareNext(path)
}

// Pause suspends audio output.
func (e *Engine) Pause() {
	e.player.Pause()
	e.state.Store(2)
}

// Resume continues audio output.
func (e *Engine) Resume() {
	e.player.Play()
	e.state.Store(1)
}

// Stop halts playback and clears all mixer slots.
func (e *Engine) Stop() {
	e.mixer.Stop()
	e.state.Store(0)
}

// Seek seeks the primary track by frame index.
func (e *Engine) Seek(frame int) error {
	return e.mixer.SeekPrimary(frame)
}

// Close tears down the engine.
func (e *Engine) Close() error {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil
	}
	e.closed = true
	e.mu.Unlock()

	close(e.done)
	e.reader.Close()
	e.mixer.Close()
	_ = e.player.Close()
	_ = e.ctx.Suspend()
	return nil
}

// RingBuffer is a fixed-size circular byte buffer for interleaved PCM int16 data.
type RingBuffer struct {
	buf []byte
	mu  sync.Mutex
	r   int // read position
	w   int // write position
	n   int // bytes available
}

// NewRingBuffer creates a ring buffer with the given capacity.
func NewRingBuffer(capacity int) *RingBuffer {
	return &RingBuffer{buf: make([]byte, capacity)}
}

// Write copies as much of p as possible into the ring.
// It returns the number of bytes written.
func (rb *RingBuffer) Write(p []byte) (int, error) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	free := len(rb.buf) - rb.n
	if free == 0 {
		return 0, nil
	}
	if len(p) > free {
		p = p[:free]
	}

	written := copy(rb.buf[rb.w:], p)
	if written < len(p) {
		written += copy(rb.buf, p[written:])
	}
	rb.w = (rb.w + written) % len(rb.buf)
	rb.n += written
	return written, nil
}

// Read copies up to len(p) bytes from the ring into p.
func (rb *RingBuffer) Read(p []byte) (int, error) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if rb.n == 0 {
		return 0, nil
	}
	if len(p) > rb.n {
		p = p[:rb.n]
	}

	n := copy(p, rb.buf[rb.r:])
	if n < len(p) {
		n += copy(p[n:], rb.buf)
	}
	rb.r = (rb.r + n) % len(rb.buf)
	rb.n -= n
	return n, nil
}

// ringReader adapts RingBuffer to io.Reader for oto.Player.
type ringReader struct {
	ring *RingBuffer
	done chan struct{}
	once sync.Once
}

func (r *ringReader) Read(p []byte) (int, error) {
	for {
		select {
		case <-r.done:
			return 0, io.EOF
		default:
		}
		n, _ := r.ring.Read(p)
		if n > 0 {
			return n, nil
		}
		time.Sleep(time.Millisecond)
	}
}

func (r *ringReader) Close() {
	r.once.Do(func() {
		close(r.done)
	})
}
