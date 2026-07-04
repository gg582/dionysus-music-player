package ffmpeg

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os/exec"
	"sync"

	"github.com/gg582/gozik/internal/audio/pcm"
	"github.com/gg582/gozik/internal/models"
)

// Prescan spawns a background analysis of path using FFmpeg subprocess.
// It returns chapter metadata (nil) and a 200-point RMS waveform.
func Prescan(path string) (*models.Waveform, []models.Chapter, error) {
	if err := pcm.ValidateRawPCMSize(path); err != nil {
		return nil, nil, err
	}

	// Use ffmpeg process to decode to 8kHz mono s16le PCM
	cmd := exec.Command("ffmpeg",
		"-y",
		"-loglevel", "error",
		"-i", path,
		"-f", "s16le",
		"-ac", "1",
		"-ar", "8000",
		"-",
	)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}

	var samples []int16
	buf := make([]byte, 8192)
	for {
		n, err := stdout.Read(buf)
		if n > 0 {
			for i := 0; i < n; i += 2 {
				if i+1 < n {
					val := int16(binary.LittleEndian.Uint16(buf[i : i+2]))
					samples = append(samples, val)
				}
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			_ = cmd.Process.Kill()
			return nil, nil, err
		}
	}

	_ = cmd.Wait()

	totalSamples := len(samples)
	if totalSamples < 200 {
		return nil, nil, fmt.Errorf("too few samples: %d", totalSamples)
	}

	windowSize := totalSamples / 200
	var w models.Waveform

	for i := 0; i < 200; i++ {
		start := i * windowSize
		end := start + windowSize
		if i == 199 {
			end = totalSamples
		}

		var sumSquares float64
		count := 0
		for j := start; j < end; j++ {
			v := float64(samples[j]) / 32768.0
			sumSquares += v * v
			count++
		}

		if count > 0 {
			rms := math.Sqrt(sumSquares / float64(count))
			amp := rms * 1024.0 // heuristic gain
			if amp > 255.0 {
				amp = 255.0
			}
			w.Data[i] = uint8(amp)
		}
	}

	return &w, nil, nil
}

// ScanResult is delivered back to the GUI/main thread via a channel.
type ScanResult struct {
	Path     string
	Waveform *models.Waveform
	Chapters []models.Chapter
	Err      error
}

// ScanPool limits concurrent FFmpeg scans via a semaphore and deduplicates
// in-flight paths with a mutex-protected ownership map.
type ScanPool struct {
	limit  chan struct{}       // capacity = max concurrent workers
	mu     sync.Mutex          // guards active + done
	active map[string]struct{} // owned paths
	out    chan ScanResult
	done   chan struct{}
}

// NewScanPool creates a pool allowing at most maxWorkers concurrent scans.
func NewScanPool(maxWorkers, outBuf int) *ScanPool {
	return &ScanPool{
		limit:  make(chan struct{}, maxWorkers),
		active: make(map[string]struct{}),
		out:    make(chan ScanResult, outBuf),
		done:   make(chan struct{}),
	}
}

// Submit attempts to take ownership of path and dispatch a worker.
// It returns false if the path is already owned or the pool is closed.
func (p *ScanPool) Submit(path string) bool {
	p.mu.Lock()
	select {
	case <-p.done:
		p.mu.Unlock()
		return false
	default:
	}
	if _, owned := p.active[path]; owned {
		p.mu.Unlock()
		return false
	}
	p.active[path] = struct{}{}
	p.mu.Unlock()

	select {
	case p.limit <- struct{}{}: // acquire slot
	case <-p.done:
		p.mu.Lock()
		delete(p.active, path)
		p.mu.Unlock()
		return false
	}

	go p.work(path)
	return true
}

// work owns exactly one goroutine lifecycle: scan, send, release.
func (p *ScanPool) work(path string) {
	defer func() {
		<-p.limit // release slot
		p.mu.Lock()
		delete(p.active, path)
		p.mu.Unlock()
	}()

	wf, ch, err := Prescan(path)

	select {
	case <-p.done:
		return
	case p.out <- ScanResult{Path: path, Waveform: wf, Chapters: ch, Err: err}:
	}
}

// Results returns the output channel.
func (p *ScanPool) Results() <-chan ScanResult { return p.out }

// Close shuts down the pool. After Close, Submit returns false.
func (p *ScanPool) Close() {
	p.mu.Lock()
	select {
	case <-p.done:
		p.mu.Unlock()
		return
	default:
	}
	close(p.done)
	p.mu.Unlock()
}
